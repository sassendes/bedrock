package transport

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sassendes/bedrock/failover-controller/observe"
)

func writePEM(t *testing.T, path, typ string, der []byte) {
	t.Helper()
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
}

func makeCerts(t *testing.T, dir, name string) (cert, key, ca string) {
	t.Helper()
	caKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	caTmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test-ca"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	caCert, _ := x509.ParseCertificate(caDER)
	leafKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	leafTmpl := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: name}, DNSNames: []string{name}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}, KeyUsage: x509.KeyUsageDigitalSignature}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTmpl, caCert, &leafKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, _ := x509.MarshalPKCS8PrivateKey(leafKey)
	cert, key, ca = filepath.Join(dir, "tls.crt"), filepath.Join(dir, "tls.key"), filepath.Join(dir, "ca.crt")
	writePEM(t, cert, "CERTIFICATE", leafDER)
	writePEM(t, key, "PRIVATE KEY", keyDER)
	writePEM(t, ca, "CERTIFICATE", caDER)
	return
}

func upProof(target, nonce string) observe.Proof {
	return observe.Proof{Result: observe.ResultUp, Target: target, Nonce: nonce, EchoNonce: nonce, LSN: "0/1", SystemID: "42", Timeline: 1}
}

func TestExchangeOverMutualTLS(t *testing.T) {
	cert, key, ca := makeCerts(t, t.TempDir(), "bedrock-observer")
	serverTLS, clientTLS, err := LoadTLS(cert, key, ca, "bedrock-observer")
	if err != nil {
		t.Fatal(err)
	}
	names := []string{"a", "b", "c"}
	keys := observe.Keyring{}
	priv := map[string]ed25519.PrivateKey{}
	for _, n := range names {
		pub, p, _ := ed25519.GenerateKey(rand.Reader)
		keys[n], priv[n] = pub, p
	}
	addrs := map[string]string{}
	nodes := map[string]*observe.Node{}
	trans := map[string]*HTTP{}
	for _, n := range names {
		tr := NewHTTP(clientTLS, 2*time.Second)
		var peers []string
		for _, m := range names {
			if m != n {
				peers = append(peers, m)
			}
		}
		prober := observe.ProberFunc(func(_ context.Context, target, nonce string) observe.Proof { return upProof(target, nonce) })
		node := observe.NewNode(n, priv[n], keys, peers, tr, prober, 2)
		srv := httptest.NewUnstartedServer(NewHandler(node, 16, 1<<20, 5*time.Second))
		srv.TLS = serverTLS
		srv.StartTLS()
		t.Cleanup(srv.Close)
		addrs[n] = strings.TrimPrefix(srv.URL, "https://")
		nodes[n], trans[n] = node, tr
	}
	for _, tr := range trans {
		tr.SetAddrs(addrs)
	}
	answers, err := nodes["a"].Ask(context.Background(), "cluster-1/cluster-1", 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(answers) != 2 {
		t.Fatalf("got %d answers over mtls, want 2", len(answers))
	}
	if j := observe.Judge("42", answers); j.Verdict != observe.VerdictOwnPathBroken {
		t.Fatalf("verdict %s", j.Verdict)
	}
}

func TestClientWithoutCertificateIsRefused(t *testing.T) {
	cert, key, ca := makeCerts(t, t.TempDir(), "bedrock-observer")
	serverTLS, clientTLS, err := LoadTLS(cert, key, ca, "bedrock-observer")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(NewHandler(observe.NewNode("x", nil, nil, nil, nil, nil, 1), 1, 1024, time.Second))
	srv.TLS = serverTLS
	srv.StartTLS()
	defer srv.Close()
	bare := clientTLS.Clone()
	bare.Certificates = nil
	tr := NewHTTP(bare, time.Second)
	tr.SetAddrs(map[string]string{"x": strings.TrimPrefix(srv.URL, "https://")})
	if err := tr.Send(context.Background(), "me", "x", observe.Envelope{Request: &observe.Request{ID: "1"}}); err == nil {
		t.Fatal("a client without a certificate got through")
	}
}

func TestHandlerRejectsJunk(t *testing.T) {
	h := NewHandler(observe.NewNode("x", nil, nil, nil, nil, nil, 1), 1, 64, time.Second)
	for _, body := range []string{"{}", "not json", `{"request":{},"answer":{}}`, strings.Repeat("x", 200)} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, Path, strings.NewReader(body)))
		if rec.Code == http.StatusAccepted {
			t.Fatalf("accepted %q", body)
		}
	}
}
