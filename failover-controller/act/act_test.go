package act

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sassendes/bedrock/failover-controller/testenv"

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
