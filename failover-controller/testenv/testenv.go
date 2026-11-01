package testenv

import (
	"context"
	"errors"
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	dynfake "k8s.io/client-go/dynamic/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

var (
	Clusters = schema.GroupVersionResource{Group: "postgresql.cnpg.io", Version: "v1", Resource: "clusters"}
	Poolers  = schema.GroupVersionResource{Group: "postgresql.cnpg.io", Version: "v1", Resource: "poolers"}
)

const Namespace = "bedrock-failover"

type World struct {
	Dyn  *dynfake.FakeDynamicClient
	Kube *kubefake.Clientset
}

func ext(name, host string) map[string]any {
	return map[string]any{"name": name, "connectionParameters": map[string]any{"host": host}}
}

func cluster(name string, replica map[string]any, externals ...any) *unstructured.Unstructured {
	spec := map[string]any{"instances": int64(2)}
	if replica != nil {
		spec["replica"] = replica
	}
	if len(externals) > 0 {
		spec["externalClusters"] = externals
	}
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "postgresql.cnpg.io/v1", "kind": "Cluster",
		"metadata": map[string]any{"namespace": name, "name": name, "uid": name + "-uid"},
		"spec":     spec,
		"status":   map[string]any{"readyInstances": int64(2), "currentPrimary": name + "-1", "phase": "Cluster in healthy state"},
	}}
}

func pooler(cluster string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "postgresql.cnpg.io/v1", "kind": "Pooler",
		"metadata": map[string]any{"namespace": cluster, "name": cluster + "-pooler-rw"},
		"spec":     map[string]any{"cluster": map[string]any{"name": cluster}, "type": "rw"},
	}}
}

func owned(ns, name, kind, owner string, selector map[string]string) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name, OwnerReferences: []metav1.OwnerReference{{APIVersion: "postgresql.cnpg.io/v1", Kind: kind, Name: owner, UID: "x"}}},
		Spec:       corev1.ServiceSpec{Selector: selector},
	}
}

func Node(name, zone string) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{"topology.kubernetes.io/zone": zone}},
		Status:     corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}},
	}
}

func Instance(cluster string, i int, node string) *corev1.Pod {
	name := cluster + "-" + string(rune('0'+i))
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: cluster, Name: name, UID: types.UID(name), Labels: map[string]string{"cnpg.io/cluster": cluster, "cnpg.io/podRole": "instance"}},
		Spec:       corev1.PodSpec{NodeName: node},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning, PodIP: "10.0." + string(rune('0'+i)) + ".1"},
	}
}

func Observer(node, slot string) *corev1.Pod {
	ip := "10.1.0."
	if slot == "b" {
		ip = "10.2.0."
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: Namespace, Name: "bedrock-observer-" + slot + "-" + node, Labels: map[string]string{"app.kubernetes.io/name": "bedrock-observer", "bedrock.io/observer-slot": slot}},
		Spec:       corev1.PodSpec{NodeName: node},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning, PodIP: ip + node[len(node)-1:]},
	}
}

// New builds the lab: cluster-1 primary on n1 and n2, cluster-2 live and cluster-3 delayed on n3 to n5,
// observers on every node, and cluster-3 prepared to follow cluster-2 after a failover.
func New() *World {
	scheme := runtime.NewScheme()
	dyn := dynfake.NewSimpleDynamicClientWithCustomListKinds(scheme,
		map[schema.GroupVersionResource]string{Clusters: "ClusterList", Poolers: "PoolerList"},
		cluster("cluster-1", map[string]any{"enabled": false}),
		cluster("cluster-2", map[string]any{"enabled": true, "source": "cluster-1"}, ext("cluster-1", "cluster-1-rw.cluster-1.svc")),
		cluster("cluster-3", map[string]any{"enabled": true, "source": "cluster-1", "minApplyDelay": "5m"},
			ext("cluster-1", "cluster-1-rw.cluster-1.svc"), ext("cluster-2", "cluster-2-rw.cluster-2.svc")),
		pooler("cluster-1"), pooler("cluster-2"), pooler("cluster-3"),
	)
	primary := map[string]string{"cnpg.io/instanceRole": "primary"}
	var objs []runtime.Object
	for _, n := range []string{"n1", "n2", "n3", "n4", "n5"} {
		objs = append(objs, Node(n, "z-"+n), Observer(n, "a"), Observer(n, "b"))
	}
	for _, c := range []string{"cluster-1", "cluster-2", "cluster-3"} {
		objs = append(objs,
			owned(c, c+"-rw", "Cluster", c, primary),
			owned(c, c+"-ro", "Cluster", c, map[string]string{"cnpg.io/instanceRole": "replica"}),
			owned(c, c+"-pooler-rw", "Pooler", c+"-pooler-rw", nil),
		)
	}
	objs = append(objs,
		Instance("cluster-1", 1, "n1"), Instance("cluster-1", 2, "n2"),
		Instance("cluster-2", 1, "n3"), Instance("cluster-2", 2, "n4"),
		Instance("cluster-3", 1, "n4"), Instance("cluster-3", 2, "n5"),
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Namespace: "application", Name: "postgres-rw", Labels: map[string]string{"bedrock.io/points-at": "cluster-1"}},
			Spec:       corev1.ServiceSpec{Type: corev1.ServiceTypeExternalName, ExternalName: "cluster-1-pooler-rw.cluster-1.svc.cluster.local"},
		},
	)
	return &World{Dyn: dyn, Kube: kubefake.NewSimpleClientset(objs...)}
}

func (w *World) Cluster(name string) *unstructured.Unstructured {
	obj, err := w.Dyn.Resource(Clusters).Namespace(name).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		panic(err)
	}
	return obj
}

func (w *World) SetStatus(name string, phase string, ready int64) {
	obj := w.Cluster(name)
	_ = unstructured.SetNestedField(obj.Object, phase, "status", "phase")
	_ = unstructured.SetNestedField(obj.Object, ready, "status", "readyInstances")
	if _, err := w.Dyn.Resource(Clusters).Namespace(name).Update(context.Background(), obj, metav1.UpdateOptions{}); err != nil {
		panic(err)
	}
}

func (w *World) SetNodeReady(name string, ready bool, at time.Time) {
	n, err := w.Kube.CoreV1().Nodes().Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		panic(err)
	}
	st := corev1.ConditionTrue
	if !ready {
		st = corev1.ConditionUnknown
	}
	n.Status.Conditions = []corev1.NodeCondition{{Type: corev1.NodeReady, Status: st, LastTransitionTime: metav1.NewTime(at)}}
	if _, err := w.Kube.CoreV1().Nodes().Update(context.Background(), n, metav1.UpdateOptions{}); err != nil {
		panic(err)
	}
}

func (w *World) Pods(ns, cluster string) []corev1.Pod {
	l, err := w.Kube.CoreV1().Pods(ns).List(context.Background(), metav1.ListOptions{LabelSelector: "cnpg.io/podRole=instance,cnpg.io/cluster=" + cluster})
	if err != nil {
		panic(err)
	}
	return l.Items
}

// Optimistic makes the fake API server do what the real one does: every write bumps resourceVersion
// and an update carrying a stale one is refused with a conflict.
func Optimistic(c *kubefake.Clientset, resource string) {
	var rv int64
	gr := schema.GroupResource{Resource: resource}
	c.PrependReactor("create", resource, func(a k8stesting.Action) (bool, runtime.Object, error) {
		obj := a.(k8stesting.CreateAction).GetObject().(metav1.Object)
		rv++
		obj.SetResourceVersion(strconv.FormatInt(rv, 10))
		return false, nil, nil
	})
	c.PrependReactor("update", resource, func(a k8stesting.Action) (bool, runtime.Object, error) {
		u := a.(k8stesting.UpdateAction)
		obj := u.GetObject().(metav1.Object)
		cur, err := c.Tracker().Get(schema.GroupVersionResource{Version: "v1", Resource: resource}, u.GetNamespace(), obj.GetName())
		if err != nil {
			return true, nil, err
		}
		if obj.GetResourceVersion() != "" && obj.GetResourceVersion() != cur.(metav1.Object).GetResourceVersion() {
			return true, nil, apierrors.NewConflict(gr, obj.GetName(), errors.New("stale resourceVersion"))
		}
		rv++
		obj.SetResourceVersion(strconv.FormatInt(rv, 10))
		return false, nil, nil
	})
}
