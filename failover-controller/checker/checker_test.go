package checker

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/sassendes/bedrock/failover-controller/act"
	"github.com/sassendes/bedrock/failover-controller/metrics"
	"github.com/sassendes/bedrock/failover-controller/testenv"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func serverCert(t *testing.T, dns string) (tls.Certificate, *x509.CertPool) {
	caKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	caTmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "cluster-1"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	caDER, _ := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	ca, _ := x509.ParseCertificate(caDER)
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: dns}, DNSNames: []string{dns}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, KeyUsage: x509.KeyUsageDigitalSignature}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(ca)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, pool
}

func run(t *testing.T, dns string, status int) float64 {
	cert, pool := serverCert(t, dns)
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(status)
	}))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	srv.StartTLS()
	defer srv.Close()
	host, port, _ := net.SplitHostPort(srv.Listener.Addr().String())

	w := testenv.New()
	ctx := context.Background()
	p, _ := w.Kube.CoreV1().Pods("cluster-1").Get(ctx, "cluster-1-1", metav1.GetOptions{})
	p.Status.PodIP = host
	if _, err := w.Kube.CoreV1().Pods("cluster-1").Update(ctx, p, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.Port, _ = strconv.Atoi(port)
	ac := act.DefaultConfig()
	m := metrics.New()
	New(w.Kube, w.Dyn, ac.Discovery, act.New(w.Dyn, w.Kube, ac), cfg, pool, m, slog.New(slog.NewTextHandler(io.Discard, nil))).Round(ctx)
	if _, ok := m.Get("bedrock_checker_last_round_timestamp_seconds", nil); !ok {
		t.Fatal("no dead man's switch heartbeat")
	}
	v, ok := m.Get("bedrock_checker_primary_healthy", map[string]string{"target": "cluster-1/cluster-1"})
	if !ok {
		t.Fatal("no health series")
	}
	return v
}

func TestHealthyPrimary(t *testing.T) {
	if v := run(t, "cluster-1-rw.cluster-1.svc", http.StatusOK); v != 1 {
		t.Fatalf("healthy %v", v)
	}
}

func TestUnhealthyPrimary(t *testing.T) {
	if v := run(t, "cluster-1-rw.cluster-1.svc", http.StatusInternalServerError); v != 0 {
		t.Fatalf("healthy %v", v)
	}
}

func TestWrongCertificateIsUnhealthy(t *testing.T) {
	if v := run(t, "someone-else.svc", http.StatusOK); v != 0 {
		t.Fatalf("a certificate for another name was trusted: %v", v)
	}
}
