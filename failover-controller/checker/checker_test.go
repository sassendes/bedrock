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
	"github.com/sassendes/bedrock/failover-controller/observe"
	"github.com/sassendes/bedrock/failover-controller/store"
	"github.com/sassendes/bedrock/failover-controller/testenv"

	coordinationv1 "k8s.io/api/coordination/v1"
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

type setup struct {
	status   int
	dns      string
	round    bool
	lease    bool
	votes    bool
	fencing  bool
	stallFor time.Duration
	oneSlot  bool
	leaseFor int32
}

type result struct {
	healthy, halt, active float64
	pages                 int
}

func healthyPath() setup {
	return setup{status: http.StatusOK, dns: "cluster-1-rw.cluster-1.svc", round: true, lease: true, votes: true, leaseFor: 15}
}

func run(t *testing.T, s setup) result {
	cert, pool := serverCert(t, s.dns)
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/readyz" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(s.status)
	}))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	srv.StartTLS()
	defer srv.Close()
	host, port, _ := net.SplitHostPort(srv.Listener.Addr().String())

	w := testenv.New()
	testenv.Optimistic(w.Kube, "configmaps")
	ctx := context.Background()
	p, _ := w.Kube.CoreV1().Pods("cluster-1").Get(ctx, "cluster-1-1", metav1.GetOptions{})
	p.Status.PodIP = host
	if _, err := w.Kube.CoreV1().Pods("cluster-1").Update(ctx, p, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	st := store.New(w.Kube, store.DefaultConfig())
	if s.round {
		if err := st.PutRound(ctx, store.Round{Number: 7, Challenge: "c7", IssuedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	if s.lease {
		holder, dur := "decider-0", s.leaseFor
		renew := metav1.NewMicroTime(now)
		if _, err := w.Kube.CoordinationV1().Leases(testenv.Namespace).Create(ctx, &coordinationv1.Lease{
			ObjectMeta: metav1.ObjectMeta{Name: "bedrock-decider", Namespace: testenv.Namespace},
			Spec:       coordinationv1.LeaseSpec{HolderIdentity: &holder, LeaseDurationSeconds: &dur, RenewTime: &renew},
		}, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	if s.votes {
		for _, n := range []string{"n2", "n3", "n4", "n5"} {
			for _, slot := range []string{"a", "b"} {
				if s.oneSlot && slot == "b" {
					continue
				}
				v := map[string]observe.Vote{"cluster-1/cluster-1": {Target: "cluster-1/cluster-1", Challenge: "c7", Ballot: observe.BallotUp}}
				if err := st.PublishVotes(ctx, n, slot, "", v); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	if s.fencing {
		c := &store.Claim{Target: "cluster-1/cluster-1", Epoch: 1, State: store.ClaimOpen, Step: store.StepFencing, Times: map[string]string{}}
		if err := st.Open(ctx, c, nil); err != nil {
			t.Fatal(err)
		}
	}
	cfg := DefaultConfig()
	cfg.Port, _ = strconv.Atoi(port)
	ac := act.DefaultConfig()
	m := metrics.New()
	c := New(w.Kube, w.Dyn, ac.Discovery, act.New(w.Dyn, w.Kube, ac), st, cfg, pool, m, slog.New(slog.NewTextHandler(io.Discard, nil)))
	c.now = func() time.Time { return now }
	c.Round(ctx)
	if s.stallFor > 0 {
		now = now.Add(s.stallFor)
		c.Round(ctx)
	}
	if _, ok := m.Get("bedrock_checker_last_round_timestamp_seconds", nil); !ok {
		t.Fatal("no dead man's switch heartbeat")
	}
	l := map[string]string{"target": "cluster-1/cluster-1"}
	var r result
	r.healthy, _ = m.Get("bedrock_checker_primary_healthy", l)
	r.halt, _ = m.Get("bedrock_checker_halt", l)
	r.active, _ = m.Get("bedrock_checker_active", l)
	r.pages = m.Count("bedrock_checker_page")
	halts, err := st.GetHalts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if halts["cluster-1.cluster-1"] != (r.halt == 1) {
		t.Fatalf("object %v disagrees with the metric %v", halts, r.halt)
	}
	return r
}

func TestSleepsWhileTheNormalPathWorks(t *testing.T) {
	r := run(t, healthyPath())
	if r.active != 0 || r.halt != 0 || r.pages != 0 || r.healthy != 1 {
		t.Fatalf("got %+v", r)
	}
}

func TestStalledVerifierAndUpDatabaseStopsEverythingWithoutAPage(t *testing.T) {
	s := healthyPath()
	s.stallFor = time.Minute
	r := run(t, s)
	if r.active != 1 || r.halt != 1 || r.pages != 0 {
		t.Fatalf("got %+v", r)
	}
}

func TestNoRoundAtAllCountsAsStalled(t *testing.T) {
	s := healthyPath()
	s.round = false
	if r := run(t, s); r.active != 1 || r.halt != 1 {
		t.Fatalf("got %+v", r)
	}
}

func TestNoLeaderAndDownDatabasePages(t *testing.T) {
	s := healthyPath()
	s.lease = false
	s.status = http.StatusServiceUnavailable
	r := run(t, s)
	if r.active != 1 || r.halt != 0 || r.pages != 1 {
		t.Fatalf("got %+v", r)
	}
}

func TestLostQuorumAndUpDatabaseHalts(t *testing.T) {
	s := healthyPath()
	s.votes = false
	if r := run(t, s); r.active != 1 || r.halt != 1 || r.pages != 0 {
		t.Fatalf("got %+v", r)
	}
}

func TestNeverHaltsAFailoverPastTheFence(t *testing.T) {
	s := healthyPath()
	s.stallFor = time.Minute
	s.fencing = true
	if r := run(t, s); r.halt != 0 {
		t.Fatalf("halted a failover that already started fencing: %+v", r)
	}
}

func TestWrongCertificateIsDown(t *testing.T) {
	s := healthyPath()
	s.dns = "someone-else.svc"
	s.round = false
	r := run(t, s)
	if r.healthy != 0 || r.halt != 0 || r.pages != 1 {
		t.Fatalf("a certificate for another name was trusted: %+v", r)
	}
}

func TestOnlyTheVerifierStallingIsEnough(t *testing.T) {
	s := healthyPath()
	s.stallFor = time.Minute
	s.leaseFor = 3600
	if r := run(t, s); r.active != 1 || r.halt != 1 {
		t.Fatalf("a stalled verifier alone did not wake the checker: %+v", r)
	}
}

func TestNodesWithOneObserverDoNotMakeQuorum(t *testing.T) {
	s := healthyPath()
	s.oneSlot = true
	if r := run(t, s); r.active != 1 || r.halt != 1 {
		t.Fatalf("half the observers made quorum: %+v", r)
	}
}
