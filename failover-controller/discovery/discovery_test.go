package discovery

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynfake "k8s.io/client-go/dynamic/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"
)

func primary(ns, name string, instances int64) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "postgresql.cnpg.io/v1",
		"kind":       "Cluster",
		"metadata":   map[string]any{"namespace": ns, "name": name, "uid": ns + "-" + name},
		"spec":       map[string]any{"instances": instances},
		"status":     map[string]any{"readyInstances": instances, "currentPrimary": name + "-1", "phase": "Cluster in healthy state"},
	}}
}

func replica(ns, name, host, delay string) *unstructured.Unstructured {
	rep := map[string]any{"enabled": true, "source": "upstream"}
	if delay != "" {
		rep["minApplyDelay"] = delay
	}
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "postgresql.cnpg.io/v1",
		"kind":       "Cluster",
		"metadata":   map[string]any{"namespace": ns, "name": name, "uid": ns + "-" + name},
		"spec": map[string]any{
			"instances": int64(3),
			"replica":   rep,
			"externalClusters": []any{
				map[string]any{"name": "upstream", "connectionParameters": map[string]any{"host": host}},
			},
		},
	}}
}

func node(name, zone string) *corev1.Node {
	return &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{"topology.kubernetes.io/zone": zone}}}
}

func instancePod(ns, cluster, name, nodeName string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name, Labels: map[string]string{"cnpg.io/cluster": cluster, "cnpg.io/podRole": "instance"}},
		Spec:       corev1.PodSpec{NodeName: nodeName},
	}
}

func jobPod(ns, cluster, name, nodeName string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name, Labels: map[string]string{"cnpg.io/cluster": cluster, "cnpg.io/jobRole": "join"}},
		Spec:       corev1.PodSpec{NodeName: nodeName},
	}
}

func service(ns, name, ownerKind, ownerAPIVersion, ownerName string) *corev1.Service {
	return &corev1.Service{ObjectMeta: metav1.ObjectMeta{
		Namespace: ns,
		Name:      name,
		OwnerReferences: []metav1.OwnerReference{
			{APIVersion: ownerAPIVersion, Kind: ownerKind, Name: ownerName, UID: "x"},
		},
	}}
}

func rw(ns, cluster string) *corev1.Service {
	return service(ns, cluster+"-rw", "Cluster", "postgresql.cnpg.io/v1", cluster)
}

func testConfig() Config {
	cfg := DefaultConfig()
	cfg.DRZones = map[string]bool{"offsite": true}
	return cfg
}

func run(t *testing.T, cfg Config, clusters []runtime.Object, kube []runtime.Object) Discovery {
	t.Helper()
	dyn := dynfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
		map[schema.GroupVersionResource]string{cfg.ClusterGVR: "ClusterList"}, clusters...)
	got, err := Discover(context.Background(), dyn, kubefake.NewSimpleClientset(kube...), cfg)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	return got
}

func TestFourClusterFamilyByFactsNotNames(t *testing.T) {
	clusters := []runtime.Object{
		primary("poop", "poop", 3),
		replica("banana", "banana", "poop-rw.poop.svc", ""),
		replica("x", "x", "poop-rw.poop.svc.cluster.local", "5m"),
		replica("far", "far", "poop-rw.poop", ""),
	}
	kube := []runtime.Object{
		rw("poop", "poop"),
		node("n1", "main"), node("n2", "main"), node("n13", "offsite"),
		instancePod("poop", "poop", "poop-1", "n1"),
		instancePod("banana", "banana", "banana-1", "n2"),
		instancePod("x", "x", "x-1", "n1"),
		instancePod("far", "far", "far-1", "n13"),
		jobPod("far", "far", "far-join", "n1"),
	}
	got := run(t, testConfig(), clusters, kube)

	if len(got.Problems) != 0 {
		t.Fatalf("unexpected problems: %+v", got.Problems)
	}
	if len(got.Families) != 1 {
		t.Fatalf("want 1 family, got %d", len(got.Families))
	}
	f := got.Families[0]
	if f.Primary.Key() != "poop/poop" || f.Primary.Instances != 3 || f.Primary.CurrentPrimary != "poop-1" {
		t.Errorf("primary = %+v", f.Primary)
	}
	if len(f.Live) != 1 || f.Live[0].Key() != "banana/banana" {
		t.Errorf("live = %+v", f.Live)
	}
	if len(f.Delayed) != 1 || f.Delayed[0].Key() != "x/x" || f.Delayed[0].ApplyDelay != "5m" {
		t.Errorf("delayed = %+v", f.Delayed)
	}
	if len(f.DR) != 1 || f.DR[0].Key() != "far/far" {
		t.Errorf("dr = %+v, job pods must not count toward zones", f.DR)
	}
}

func TestDeadPodsDoNotCountTowardZones(t *testing.T) {
	evicted := instancePod("far", "far", "far-old", "n1")
	evicted.Status.Phase = corev1.PodFailed
	done := instancePod("far", "far", "far-done", "n1")
	done.Status.Phase = corev1.PodSucceeded
	leaving := instancePod("far", "far", "far-leaving", "n1")
	now := metav1.Now()
	leaving.DeletionTimestamp = &now

	clusters := []runtime.Object{primary("a", "a", 3), replica("far", "far", "a-rw.a.svc", "")}
	kube := []runtime.Object{
		rw("a", "a"),
		node("n1", "main"), node("n13", "offsite"),
		instancePod("far", "far", "far-1", "n13"),
		evicted, done, leaving,
	}
	got := run(t, testConfig(), clusters, kube)
	if len(got.Problems) != 0 || len(got.Families[0].DR) != 1 {
		t.Fatalf("dead pods on a main node must not split a dr cluster, got %+v", got)
	}
}

func TestServiceNameDoesNotMatterOnlyItsOwner(t *testing.T) {
	clusters := []runtime.Object{primary("a", "a", 3), replica("b", "b", "whatever.a.svc", "")}
	kube := []runtime.Object{service("a", "whatever", "Cluster", "postgresql.cnpg.io/v1", "a")}
	got := run(t, testConfig(), clusters, kube)
	if len(got.Problems) != 0 || len(got.Families[0].Live) != 1 {
		t.Fatalf("got %+v", got)
	}
}

func TestEverythingComesFromConfig(t *testing.T) {
	cfg := testConfig()
	cfg.ClusterGVR = schema.GroupVersionResource{Group: "db.example.io", Version: "v9", Resource: "pgs"}
	cfg.ClusterKind = "Pg"
	cfg.ClusterLabel = "example/pg"
	cfg.InstanceSelector = "example/role=db"
	cfg.ZoneLabel = "example/site"
	cfg.DRZones = map[string]bool{"cave": true}

	far := replica("far", "far", "src.a", "")
	far.SetAPIVersion("db.example.io/v9")
	far.SetKind("Pg")
	main := primary("a", "a", 3)
	main.SetAPIVersion("db.example.io/v9")
	main.SetKind("Pg")

	kube := []runtime.Object{
		service("a", "src", "Pg", "db.example.io/v9", "a"),
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "n9", Labels: map[string]string{"example/site": "cave"}}},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Namespace: "far", Name: "far-1", Labels: map[string]string{"example/pg": "far", "example/role": "db"}},
			Spec:       corev1.PodSpec{NodeName: "n9"},
		},
	}
	got := run(t, cfg, []runtime.Object{main, far}, kube)
	if len(got.Problems) != 0 || len(got.Families) != 1 || len(got.Families[0].DR) != 1 {
		t.Fatalf("got %+v", got)
	}
}

func TestDelayFromPostgresParameter(t *testing.T) {
	r := replica("b", "b", "a-rw.a.svc", "")
	_ = unstructured.SetNestedField(r.Object, "10min", "spec", "postgresql", "parameters", "recovery_min_apply_delay")
	got := run(t, testConfig(), []runtime.Object{primary("a", "a", 3), r}, []runtime.Object{rw("a", "a")})
	if len(got.Families[0].Delayed) != 1 {
		t.Fatalf("want delayed via recovery_min_apply_delay, got %+v", got.Families[0])
	}
}

func TestZeroDelayIsLive(t *testing.T) {
	got := run(t, testConfig(), []runtime.Object{primary("a", "a", 3), replica("b", "b", "a-rw.a.svc", "0s")}, []runtime.Object{rw("a", "a")})
	if len(got.Families[0].Live) != 1 {
		t.Fatalf("0s delay must count as live, got %+v", got.Families[0])
	}
}

func TestShortHostUsesReplicaNamespace(t *testing.T) {
	got := run(t, testConfig(), []runtime.Object{primary("db", "a", 3), replica("db", "b", "a-rw", "")}, []runtime.Object{rw("db", "a")})
	if len(got.Problems) != 0 || len(got.Families[0].Live) != 1 {
		t.Fatalf("got %+v", got)
	}
}

func TestTwoIndependentFamilies(t *testing.T) {
	got := run(t, testConfig(), []runtime.Object{
		primary("a", "a", 3), replica("a2", "a2", "a-rw.a.svc", ""),
		primary("b", "b", 3), replica("b2", "b2", "b-rw.b.svc", ""),
	}, []runtime.Object{rw("a", "a"), rw("b", "b")})
	if len(got.Families) != 2 || len(got.Families[0].Live) != 1 || len(got.Families[1].Live) != 1 {
		t.Fatalf("got %+v", got.Families)
	}
}

func TestProblemsAreRefusedNotGuessed(t *testing.T) {
	distributed := replica("d", "d", "a-rw.a.svc", "")
	_ = unstructured.SetNestedField(distributed.Object, "a", "spec", "replica", "primary")
	noExternal := replica("e", "e", "a-rw.a.svc", "")
	unstructured.RemoveNestedField(noExternal.Object, "spec", "externalClusters")
	noEnabled := replica("f", "f", "a-rw.a.svc", "")
	unstructured.RemoveNestedField(noEnabled.Object, "spec", "replica", "enabled")

	clusters := []runtime.Object{
		primary("a", "a", 3),
		replica("b", "b", "a-rw.a.svc", ""),
		replica("c", "c", "b-rw.b.svc", ""),
		replica("ghost", "ghost", "nope-rw.nope.svc", ""),
		replica("ip", "ip", "10.0.0.5", ""),
		replica("ext", "ext", "db.example.com", ""),
		replica("stranger", "stranger", "plain.a.svc", ""),
		replica("orphan", "orphan", "lost-rw.lost.svc", ""),
		replica("split", "split", "a-rw.a.svc", ""),
		distributed,
		noExternal,
		noEnabled,
	}
	kube := []runtime.Object{
		rw("a", "a"), rw("b", "b"),
		service("a", "plain", "Deployment", "apps/v1", "web"),
		service("lost", "lost-rw", "Cluster", "postgresql.cnpg.io/v1", "lost"),
		node("n1", "main"), node("n13", "offsite"),
		instancePod("split", "split", "split-1", "n1"),
		instancePod("split", "split", "split-2", "n13"),
	}
	got := run(t, testConfig(), clusters, kube)

	want := map[string]bool{
		"c/c": true, "d/d": true, "e/e": true, "f/f": true, "ghost/ghost": true, "ip/ip": true,
		"ext/ext": true, "stranger/stranger": true, "orphan/orphan": true, "split/split": true,
	}
	if len(got.Problems) != len(want) {
		t.Fatalf("want %d problems, got %+v", len(want), got.Problems)
	}
	for _, p := range got.Problems {
		if !want[p.Cluster] {
			t.Errorf("unexpected problem %+v", p)
		}
	}
	if len(got.Families) != 1 || len(got.Families[0].Live) != 1 || got.Families[0].Live[0].Key() != "b/b" {
		t.Fatalf("family = %+v", got.Families)
	}
}

func TestParseServiceHost(t *testing.T) {
	type res struct {
		name, ns string
		ok       bool
	}
	cases := map[string]res{
		"cluster-1-rw.cluster-1.svc":               {"cluster-1-rw", "cluster-1", true},
		"cluster-1-rw.cluster-1.svc.cluster.local": {"cluster-1-rw", "cluster-1", true},
		"cluster-1-rw.cluster-1.svc.corp.internal": {"cluster-1-rw", "cluster-1", true},
		"cluster-1-rw.cluster-1":                   {"cluster-1-rw", "cluster-1", true},
		"anything":                                 {"anything", "home", true},
		"CAPS.Ns.svc":                              {"caps", "ns", true},
		"10.0.0.5":                                 {"", "", false},
		"db.example.com":                           {"", "", false},
		"":                                         {"", "", false},
		".x":                                       {"", "", false},
	}
	for in, want := range cases {
		n, ns, ok := parseServiceHost(in, "home")
		if (res{n, ns, ok}) != want {
			t.Errorf("parseServiceHost(%q) = %q %q %v, want %+v", in, n, ns, ok, want)
		}
	}
}
