package act

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sassendes/bedrock/failover-controller/testenv"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestRepointNeedsAPreparedEntry(t *testing.T) {
	w := testenv.New()
	a := New(w.Dyn, w.Kube, DefaultConfig())
	ok, msg, err := a.Repoint(context.Background(), "cluster-2/cluster-2", "cluster-3/cluster-3", 1)
	if err != nil {
		t.Fatal(err)
	}
	if ok || !strings.Contains(msg, "by hand") {
		t.Fatalf("repointed without a prepared entry: %s", msg)
	}
	src, _, _ := unstructured.NestedString(w.Cluster("cluster-2").Object, "spec", "replica", "source")
	if src != "cluster-1" {
		t.Fatalf("source changed to %q", src)
	}
}

func TestRepointIsIdempotent(t *testing.T) {
	w := testenv.New()
	a := New(w.Dyn, w.Kube, DefaultConfig())
	for i := 0; i < 2; i++ {
		ok, _, err := a.Repoint(context.Background(), "cluster-3/cluster-3", "cluster-2/cluster-2", 1)
		if err != nil || !ok {
			t.Fatalf("round %d: %v %v", i, ok, err)
		}
	}
	src, _, _ := unstructured.NestedString(w.Cluster("cluster-3").Object, "spec", "replica", "source")
	if src != "cluster-2" {
		t.Fatalf("source %q", src)
	}
}

func TestStableHostFallsBackToThePrimaryService(t *testing.T) {
	w := testenv.New()
	if err := w.Dyn.Resource(testenv.Poolers).Namespace("cluster-2").Delete(context.Background(), "cluster-2-pooler-rw", metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	h, err := New(w.Dyn, w.Kube, DefaultConfig()).StableHost(context.Background(), "cluster-2/cluster-2")
	if err != nil {
		t.Fatal(err)
	}
	if h != "cluster-2-rw.cluster-2.svc.cluster.local" {
		t.Fatalf("host %s", h)
	}
}

func TestFlipRefusesWhenNothingIsLabelled(t *testing.T) {
	w := testenv.New()
	if _, err := New(w.Dyn, w.Kube, DefaultConfig()).Flip(context.Background(), "cluster-2/cluster-2", "cluster-3/cluster-3", 1); err == nil {
		t.Fatal("flip with no labelled service succeeded")
	}
}

func TestFlipIsSafeToRepeat(t *testing.T) {
	w := testenv.New()
	ctx := context.Background()
	a := New(w.Dyn, w.Kube, DefaultConfig())
	first, err := a.Flip(ctx, "cluster-1/cluster-1", "cluster-2/cluster-2", 7)
	if err != nil {
		t.Fatal(err)
	}
	again, err := a.Flip(ctx, "cluster-1/cluster-1", "cluster-2/cluster-2", 7)
	if err != nil {
		t.Fatalf("a retry of the same run failed: %v", err)
	}
	if len(again) != len(first) || again[0] != first[0] {
		t.Fatalf("retry reported %v, first run %v", again, first)
	}
	if _, err := a.Flip(ctx, "cluster-1/cluster-1", "cluster-2/cluster-2", 8); err == nil {
		t.Fatal("another epoch counted a service it never flipped")
	}
}

func TestTerminatingPodIsNotAFence(t *testing.T) {
	w := testenv.New()
	ctx := context.Background()
	p, err := w.Kube.CoreV1().Pods("cluster-1").Get(ctx, "cluster-1-1", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	now := metav1.Now()
	p.DeletionTimestamp = &now
	if _, err := w.Kube.CoreV1().Pods("cluster-1").Update(ctx, p, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := w.Kube.CoreV1().Pods("cluster-1").Delete(ctx, "cluster-1-2", metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	ok, reason, err := New(w.Dyn, w.Kube, DefaultConfig()).VerifyFence(ctx, "cluster-1/cluster-1", []string{"n1", "n2"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if ok || !strings.Contains(reason, "cluster-1-1") {
		t.Fatalf("terminating pod passed the fence check: %v %s", ok, reason)
	}
}

func TestPromotedNeedsHealthy(t *testing.T) {
	w := testenv.New()
	a := New(w.Dyn, w.Kube, DefaultConfig())
	ctx := context.Background()
	if ok, _, _ := a.Promoted(ctx, "cluster-2/cluster-2"); ok {
		t.Fatal("a replica counted as promoted")
	}
	if err := a.Promote(ctx, "cluster-2/cluster-2", 1); err != nil {
		t.Fatal(err)
	}
	w.SetStatus("cluster-2", "Promoting to primary cluster", 1)
	if ok, _, _ := a.Promoted(ctx, "cluster-2/cluster-2"); ok {
		t.Fatal("promoting counted as promoted")
	}
	w.SetStatus("cluster-2", "Cluster in healthy state", 1)
	if ok, reason, _ := a.Promoted(ctx, "cluster-2/cluster-2"); !ok {
		t.Fatal(reason)
	}
}

func TestFlipOnlyTouchesTheConfiguredNamespaces(t *testing.T) {
	w := testenv.New()
	ctx := context.Background()
	stray := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Namespace: "someone-else", Name: "postgres-rw", Labels: map[string]string{"bedrock.io/points-at": "cluster-1"}},
		Spec:       corev1.ServiceSpec{Type: corev1.ServiceTypeExternalName, ExternalName: "elsewhere.example"},
	}
	if _, err := w.Kube.CoreV1().Services("someone-else").Create(ctx, stray, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	flipped, err := New(w.Dyn, w.Kube, DefaultConfig()).Flip(ctx, "cluster-1/cluster-1", "cluster-2/cluster-2", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(flipped) != 1 || !strings.HasPrefix(flipped[0], "application/postgres-rw") {
		t.Fatalf("flipped %v", flipped)
	}
	got, err := w.Kube.CoreV1().Services("someone-else").Get(ctx, "postgres-rw", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Spec.ExternalName != "elsewhere.example" || got.Labels["bedrock.io/points-at"] != "cluster-1" {
		t.Fatalf("a service outside the flip namespaces was rewritten: %+v", got)
	}
}

func TestFenceScalesOnlyItsOwnPoolersToZero(t *testing.T) {
	w := testenv.New()
	ctx := context.Background()
	a := New(w.Dyn, w.Kube, DefaultConfig())
	for i := 0; i < 2; i++ {
		if err := a.Fence(ctx, "cluster-1/cluster-1", 4); err != nil {
			t.Fatal(err)
		}
	}
	p, err := w.Dyn.Resource(testenv.Poolers).Namespace("cluster-1").Get(ctx, "cluster-1-pooler-rw", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if n, _, _ := unstructured.NestedInt64(p.Object, "spec", "instances"); n != 0 {
		t.Fatalf("pooler left at %d instances", n)
	}
	if p.GetAnnotations()["bedrock.io/failover-epoch"] != "4" {
		t.Fatalf("pooler write carries no epoch: %v", p.GetAnnotations())
	}
	other, err := w.Dyn.Resource(testenv.Poolers).Namespace("cluster-2").Get(ctx, "cluster-2-pooler-rw", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, found, _ := unstructured.NestedInt64(other.Object, "spec", "instances"); found {
		t.Fatal("the fence touched another cluster's pooler")
	}
}

func TestAPoolerStillUpIsNotAFence(t *testing.T) {
	w := testenv.New()
	ctx := context.Background()
	for _, p := range []string{"cluster-1-1", "cluster-1-2"} {
		if err := w.Kube.CoreV1().Pods("cluster-1").Delete(ctx, p, metav1.DeleteOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	ok, reason, err := New(w.Dyn, w.Kube, DefaultConfig()).VerifyFence(ctx, "cluster-1/cluster-1", []string{"n1", "n2"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if ok || !strings.Contains(reason, "cluster-1-pooler-rw") {
		t.Fatalf("a pooler with instances passed the fence check: %v %s", ok, reason)
	}
}
