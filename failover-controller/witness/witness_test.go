package witness

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/sassendes/bedrock/failover-controller/act"
	"github.com/sassendes/bedrock/failover-controller/discovery"
	"github.com/sassendes/bedrock/failover-controller/judge"
	"github.com/sassendes/bedrock/failover-controller/observe"
	"github.com/sassendes/bedrock/failover-controller/testenv"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type world struct {
	t       *testing.T
	w       *testenv.World
	g       *Gatherer
	streams map[string]observe.ReceiverState
	health  int
	family  discovery.Family
}

func serverTLS(t *testing.T, dns string) (*tls.Config, *x509.CertPool) {
	caKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	caTmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "ca"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	caDER, _ := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	ca, _ := x509.ParseCertificate(caDER)
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(2), DNSNames: []string{dns}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, KeyUsage: x509.KeyUsageDigitalSignature}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(ca)
	return &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}, pool
}

func newWorld(t *testing.T) *world {
	ctx := context.Background()
	x := &world{t: t, w: testenv.New(), streams: map[string]observe.ReceiverState{}, health: http.StatusOK}
	srvTLS, roots := serverTLS(t, "cluster-1-rw.cluster-1.svc")
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/readyz" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(x.health)
	}))
	srv.TLS = srvTLS
	srv.StartTLS()
	t.Cleanup(srv.Close)
	host, port, _ := net.SplitHostPort(srv.Listener.Addr().String())

	p, _ := x.w.Kube.CoreV1().Pods("cluster-1").Get(ctx, "cluster-1-1", metav1.GetOptions{})
	p.Status.PodIP = host
	p.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: "postgres", Ready: true, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}}
	if _, err := x.w.Kube.CoreV1().Pods("cluster-1").Update(ctx, p, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	x.setReported(true)

	cfg := DefaultConfig()
	cfg.ManagerPort, _ = strconv.Atoi(port)
	cfg.Roots = func() *x509.CertPool { return roots }
	cfg.Check.Timeout = time.Second
	x.g = New(x.w.Kube, act.New(x.w.Dyn, x.w.Kube, act.DefaultConfig()), cfg)
	x.g.receiver = func(_ context.Context, c observe.CheckConfig) observe.ReceiverState {
		if s, ok := x.streams[c.Host]; ok {
			return s
		}
		return observe.ReceiverState{Status: "streaming", AgeMs: 100}
	}
	d, err := discovery.Discover(ctx, x.w.Dyn, x.w.Kube, act.DefaultConfig().Discovery)
	if err != nil || len(d.Families) != 1 {
		t.Fatalf("discovery %v %+v", err, d)
	}
	x.family = d.Families[0]
	return x
}

func (x *world) setReported(isPrimary bool) {
	obj := x.w.Cluster("cluster-1")
	_ = unstructured.SetNestedField(obj.Object, isPrimary, "status", "instancesReportedState", "cluster-1-1", "isPrimary")
	if _, err := x.w.Dyn.Resource(testenv.Clusters).Namespace("cluster-1").Update(context.Background(), obj, metav1.UpdateOptions{}); err != nil {
		x.t.Fatal(err)
	}
}

func (x *world) setContainer(s corev1.ContainerStatus) {
	ctx := context.Background()
	p, _ := x.w.Kube.CoreV1().Pods("cluster-1").Get(ctx, "cluster-1-1", metav1.GetOptions{})
	s.Name = "postgres"
	p.Status.ContainerStatuses = []corev1.ContainerStatus{s}
	if _, err := x.w.Kube.CoreV1().Pods("cluster-1").Update(ctx, p, metav1.UpdateOptions{}); err != nil {
		x.t.Fatal(err)
	}
}

func (x *world) allStreamsGone() {
	for _, ip := range []string{"10.0.2.1", "10.0.1.1"} {
		x.streams[ip] = observe.ReceiverState{}
	}
}

func dead(m map[judge.Witness]judge.Answer) int {
	n := 0
	for _, a := range m {
		if a == judge.Dead {
			n++
		}
	}
	return n
}

func TestHealthyPrimaryHasNoDeadWitness(t *testing.T) {
	x := newWorld(t)
	got := x.g.Gather(context.Background(), x.family)
	for w, a := range got {
		if a != judge.Alive {
			t.Fatalf("%s says %s about a healthy primary", w, a)
		}
	}
}

func TestCrashedPrimaryGetsFourDeadWitnesses(t *testing.T) {
	x := newWorld(t)
	x.allStreamsGone()
	x.health = http.StatusServiceUnavailable
	x.setContainer(corev1.ContainerStatus{State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}}})
	x.setReported(false)
	if got := x.g.Gather(context.Background(), x.family); dead(got) != 4 {
		t.Fatalf("got %v", got)
	}
}

func TestHungPrimaryIsDeadToTheKubelet(t *testing.T) {
	x := newWorld(t)
	x.setContainer(corev1.ContainerStatus{Ready: false, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}})
	if got := x.g.Gather(context.Background(), x.family)[judge.Kubelet]; got != judge.Dead {
		t.Fatalf("a running but not ready postgres was %s", got)
	}
}

func TestOneStreamingReplicaMeansAlive(t *testing.T) {
	x := newWorld(t)
	x.streams["10.0.2.1"] = observe.ReceiverState{}
	if got := x.g.Gather(context.Background(), x.family)[judge.Replicas]; got != judge.Alive {
		t.Fatalf("got %s", got)
	}
}

func TestUnreachableWitnessesAreSilentNotDead(t *testing.T) {
	x := newWorld(t)
	x.streams["10.0.2.1"] = observe.ReceiverState{Err: "connect: timeout"}
	x.streams["10.0.1.1"] = observe.ReceiverState{}
	x.w.SetNodeReady("n1", false, time.Now())
	got := x.g.Gather(context.Background(), x.family)
	if got[judge.Replicas] != judge.Silent || got[judge.Kubelet] != judge.Silent {
		t.Fatalf("got %v", got)
	}
}

func TestStaleStreamIsDead(t *testing.T) {
	x := newWorld(t)
	x.streams["10.0.2.1"] = observe.ReceiverState{Status: "streaming", AgeMs: 60_000}
	x.streams["10.0.1.1"] = observe.ReceiverState{Status: "waiting", AgeMs: 100}
	if got := x.g.Gather(context.Background(), x.family)[judge.Replicas]; got != judge.Dead {
		t.Fatalf("got %s", got)
	}
}
