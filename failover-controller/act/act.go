package act

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sassendes/bedrock/failover-controller/discovery"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

type Config struct {
	Discovery              discovery.Config
	PoolerGVR              schema.GroupVersionResource
	PoolerKind             string
	ReconcileAnnotation    string
	ReconcileDisabled      string
	HibernationAnnotation  string
	HibernationOn          string
	PrimaryServiceSelector string
	FlipLabel              string
	ClusterDomain          string
	HealthyPhase           string
	EpochAnnotation        string
}

func DefaultConfig() Config {
	return Config{
		Discovery:              discovery.DefaultConfig(),
		PoolerGVR:              schema.GroupVersionResource{Group: "postgresql.cnpg.io", Version: "v1", Resource: "poolers"},
		PoolerKind:             "Pooler",
		ReconcileAnnotation:    "cnpg.io/reconciliationLoop",
		ReconcileDisabled:      "disabled",
		HibernationAnnotation:  "cnpg.io/hibernation",
		HibernationOn:          "on",
		PrimaryServiceSelector: "cnpg.io/instanceRole=primary",
		FlipLabel:              "bedrock.io/points-at",
		ClusterDomain:          "cluster.local",
		HealthyPhase:           "Cluster in healthy state",
		EpochAnnotation:        "bedrock.io/failover-epoch",
	}
}

type Actor struct {
	dyn  dynamic.Interface
	kube kubernetes.Interface
	cfg  Config
}

func New(dyn dynamic.Interface, kube kubernetes.Interface, cfg Config) *Actor {
	return &Actor{dyn: dyn, kube: kube, cfg: cfg}
}

func (a *Actor) Config() Config { return a.cfg }

func splitKey(key string) (string, string, error) {
	ns, name, ok := strings.Cut(key, "/")
	if !ok || ns == "" || name == "" {
		return "", "", fmt.Errorf("bad cluster key %q", key)
	}
	return ns, name, nil
}

func (a *Actor) cluster(ns string) dynamic.ResourceInterface {
	return a.dyn.Resource(a.cfg.Discovery.ClusterGVR).Namespace(ns)
}

func (a *Actor) Get(ctx context.Context, key string) (*unstructured.Unstructured, error) {
	ns, name, err := splitKey(key)
	if err != nil {
		return nil, err
	}
	return a.cluster(ns).Get(ctx, name, metav1.GetOptions{})
}

// patch stamps every write with the run's epoch, so admission can refuse a decider from an older run.
func (a *Actor) patch(ctx context.Context, key string, epoch int64, body map[string]any) error {
	meta, _ := body["metadata"].(map[string]any)
	if meta == nil {
		meta = map[string]any{}
		body["metadata"] = meta
	}
	an, _ := meta["annotations"].(map[string]any)
	if an == nil {
		an = map[string]any{}
		meta["annotations"] = an
	}
	an[a.cfg.EpochAnnotation] = strconv.FormatInt(epoch, 10)
	ns, name, err := splitKey(key)
	if err != nil {
		return err
	}
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	_, err = a.cluster(ns).Patch(ctx, name, types.MergePatchType, b, metav1.PatchOptions{})
	return err
}

func (a *Actor) Fenced(obj *unstructured.Unstructured) bool {
	an := obj.GetAnnotations()
	return an[a.cfg.ReconcileAnnotation] == a.cfg.ReconcileDisabled || an[a.cfg.HibernationAnnotation] == a.cfg.HibernationOn
}

func (a *Actor) instanceSelector(name string) string {
	return a.cfg.Discovery.InstanceSelector + "," + a.cfg.Discovery.ClusterLabel + "=" + name
}

func (a *Actor) Instances(ctx context.Context, key string) (pods, nodes []string, err error) {
	ns, name, err := splitKey(key)
	if err != nil {
		return nil, nil, err
	}
	list, err := a.kube.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{LabelSelector: a.instanceSelector(name)})
	if err != nil {
		return nil, nil, err
	}
	set := map[string]bool{}
	for _, p := range list.Items {
		pods = append(pods, p.Name)
		if p.Spec.NodeName != "" {
			set[p.Spec.NodeName] = true
		}
	}
	for n := range set {
		nodes = append(nodes, n)
	}
	sort.Strings(pods)
	sort.Strings(nodes)
	return pods, nodes, nil
}

// Fence stops the operator from reconciling the cluster, so nothing recreates its pods, then deletes them.
// Hibernation alone is not enough: CNPG defers it while the cluster is unhealthy, which is exactly now.
func (a *Actor) Fence(ctx context.Context, key string, epoch int64) error {
	ns, name, err := splitKey(key)
	if err != nil {
		return err
	}
	if err := a.patch(ctx, key, epoch, map[string]any{"metadata": map[string]any{"annotations": map[string]any{
		a.cfg.ReconcileAnnotation:   a.cfg.ReconcileDisabled,
		a.cfg.HibernationAnnotation: a.cfg.HibernationOn,
	}}}); err != nil {
		return fmt.Errorf("annotate %s: %w", key, err)
	}
	pods, err := a.kube.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{LabelSelector: a.instanceSelector(name)})
	if err != nil {
		return fmt.Errorf("list instance pods of %s: %w", key, err)
	}
	for _, p := range pods.Items {
		if p.DeletionTimestamp != nil {
			continue
		}
		err := a.kube.CoreV1().Pods(ns).Delete(ctx, p.Name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &p.UID}})
		if err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("delete %s/%s: %w", ns, p.Name, err)
		}
	}
	return nil
}

// VerifyFence holds only when no instance pod object is left, terminating ones included, and every node that
// ran an instance has been Ready without a break since before the claim. A pod object is removed only after
// its kubelet confirms the containers stopped, unless it was force deleted while the node was away: a node
// that dropped and came back may still run that pod, so any Ready transition after the claim fails the fence.
func (a *Actor) VerifyFence(ctx context.Context, key string, nodes []string, readySince time.Time) (bool, string, error) {
	ns, name, err := splitKey(key)
	if err != nil {
		return false, "", err
	}
	for _, n := range nodes {
		node, err := a.kube.CoreV1().Nodes().Get(ctx, n, metav1.GetOptions{})
		if err != nil {
			return false, "", fmt.Errorf("get node %s: %w", n, err)
		}
		ready, since := nodeReady(node)
		if !ready {
			return false, fmt.Sprintf("node %s is not Ready, 0 pods from the API would prove nothing", n), nil
		}
		if since.After(readySince) {
			return false, fmt.Sprintf("node %s became Ready again at %s, after the claim: a force deleted pod may still run there", n, since.UTC().Format(time.RFC3339)), nil
		}
	}
	pods, err := a.kube.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{LabelSelector: a.instanceSelector(name)})
	if err != nil {
		return false, "", err
	}
	if len(pods.Items) > 0 {
		names := make([]string, 0, len(pods.Items))
		for _, p := range pods.Items {
			names = append(names, p.Name)
		}
		sort.Strings(names)
		return false, fmt.Sprintf("%d instance pod(s) still exist: %s", len(names), strings.Join(names, ", ")), nil
	}
	return true, "", nil
}

func nodeReady(n *corev1.Node) (bool, time.Time) {
	for _, c := range n.Status.Conditions {
		if c.Type == corev1.NodeReady {
			return c.Status == corev1.ConditionTrue, c.LastTransitionTime.Time
		}
	}
	return false, time.Time{}
}

func (a *Actor) Promote(ctx context.Context, key string, epoch int64) error {
	return a.patch(ctx, key, epoch, map[string]any{"spec": map[string]any{"replica": map[string]any{"enabled": false}}})
}

func (a *Actor) Promoted(ctx context.Context, key string) (bool, string, error) {
	obj, err := a.Get(ctx, key)
	if err != nil {
		return false, "", err
	}
	enabled, _, _ := unstructured.NestedBool(obj.Object, "spec", "replica", "enabled")
	phase, _, _ := unstructured.NestedString(obj.Object, "status", "phase")
	ready, _, _ := unstructured.NestedInt64(obj.Object, "status", "readyInstances")
	if enabled {
		return false, "replica.enabled is still true", nil
	}
	if phase != a.cfg.HealthyPhase || ready < 1 {
		return false, fmt.Sprintf("phase %q, %d ready", phase, ready), nil
	}
	return true, "", nil
}

// Repoint switches a follower to an externalClusters entry that already exists and resolves to the new
// primary. It never writes hosts or credentials: an entry that was not prepared is reported, not invented.
func (a *Actor) Repoint(ctx context.Context, follower, newPrimary string, epoch int64) (bool, string, error) {
	obj, err := a.Get(ctx, follower)
	if err != nil {
		return false, "", err
	}
	current, _, _ := unstructured.NestedString(obj.Object, "spec", "replica", "source")
	externals, _, _ := unstructured.NestedSlice(obj.Object, "spec", "externalClusters")
	for _, e := range externals {
		ext, ok := e.(map[string]any)
		if !ok {
			continue
		}
		name, _, _ := unstructured.NestedString(ext, "name")
		host, _, _ := unstructured.NestedString(ext, "connectionParameters", "host")
		if name == "" || host == "" {
			continue
		}
		key, _, err := discovery.ResolveHost(ctx, a.kube, a.cfg.Discovery, host, obj.GetNamespace())
		if err != nil {
			return false, "", err
		}
		if key != newPrimary {
			continue
		}
		if name == current {
			return true, "already following " + newPrimary, nil
		}
		if err := a.patch(ctx, follower, epoch, map[string]any{"spec": map[string]any{"replica": map[string]any{"source": name}}}); err != nil {
			return false, "", err
		}
		return true, "now follows " + newPrimary + " through " + name, nil
	}
	return false, fmt.Sprintf("%s has no externalClusters entry that resolves to %s, repoint it by hand", follower, newPrimary), nil
}

// StableHost is the rw pooler of the new primary when one exists, otherwise its own primary Service.
func (a *Actor) StableHost(ctx context.Context, key string) (string, error) {
	ns, name, err := splitKey(key)
	if err != nil {
		return "", err
	}
	poolers, err := a.dyn.Resource(a.cfg.PoolerGVR).Namespace(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", fmt.Errorf("list poolers in %s: %w", ns, err)
	}
	var owners []string
	for _, p := range poolers.Items {
		cluster, _, _ := unstructured.NestedString(p.Object, "spec", "cluster", "name")
		typ, _, _ := unstructured.NestedString(p.Object, "spec", "type")
		if cluster == name && typ == "rw" {
			owners = append(owners, p.GetName())
		}
	}
	sort.Strings(owners)
	svcs, err := a.kube.CoreV1().Services(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", err
	}
	for _, owner := range owners {
		for _, s := range svcs.Items {
			if ownedBy(s.OwnerReferences, a.cfg.PoolerGVR.Group, a.cfg.PoolerKind, owner) {
				return a.fqdn(s.Name, ns), nil
			}
		}
	}
	svc, err := a.primaryService(svcs.Items, name)
	if err != nil {
		return "", fmt.Errorf("no rw pooler for %s: %w", key, err)
	}
	return a.fqdn(svc, ns), nil
}

func (a *Actor) PrimaryService(ctx context.Context, key string) (string, error) {
	ns, name, err := splitKey(key)
	if err != nil {
		return "", err
	}
	svcs, err := a.kube.CoreV1().Services(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", err
	}
	svc, err := a.primaryService(svcs.Items, name)
	if err != nil {
		return "", err
	}
	return svc + "." + ns + ".svc", nil
}

func (a *Actor) primaryService(svcs []corev1.Service, cluster string) (string, error) {
	sel, err := parseSelector(a.cfg.PrimaryServiceSelector)
	if err != nil {
		return "", err
	}
	for _, s := range svcs {
		if ownedBy(s.OwnerReferences, a.cfg.Discovery.ClusterGVR.Group, a.cfg.Discovery.ClusterKind, cluster) && matches(s.Spec.Selector, sel) {
			return s.Name, nil
		}
	}
	return "", fmt.Errorf("no service owned by %s selects %s", cluster, a.cfg.PrimaryServiceSelector)
}

func (a *Actor) fqdn(svc, ns string) string {
	return svc + "." + ns + ".svc." + a.cfg.ClusterDomain
}

func (a *Actor) Flip(ctx context.Context, oldPrimary, newPrimary string, epoch int64) ([]string, error) {
	_, oldName, err := splitKey(oldPrimary)
	if err != nil {
		return nil, err
	}
	_, newName, err := splitKey(newPrimary)
	if err != nil {
		return nil, err
	}
	host, err := a.StableHost(ctx, newPrimary)
	if err != nil {
		return nil, err
	}
	list, err := a.kube.CoreV1().Services(metav1.NamespaceAll).List(ctx, metav1.ListOptions{LabelSelector: a.cfg.FlipLabel + "=" + oldName})
	if err != nil {
		return nil, err
	}
	var flipped []string
	for _, s := range list.Items {
		if s.Spec.Type != corev1.ServiceTypeExternalName {
			continue
		}
		body, _ := json.Marshal(map[string]any{
			"metadata": map[string]any{
				"labels":      map[string]any{a.cfg.FlipLabel: newName},
				"annotations": map[string]any{a.cfg.EpochAnnotation: strconv.FormatInt(epoch, 10)},
			},
			"spec": map[string]any{"externalName": host},
		})
		if _, err := a.kube.CoreV1().Services(s.Namespace).Patch(ctx, s.Name, types.MergePatchType, body, metav1.PatchOptions{}); err != nil {
			return flipped, fmt.Errorf("flip %s/%s: %w", s.Namespace, s.Name, err)
		}
		flipped = append(flipped, s.Namespace+"/"+s.Name+" → "+host)
	}
	if len(flipped) == 0 {
		return nil, fmt.Errorf("no ExternalName service labelled %s=%s", a.cfg.FlipLabel, oldName)
	}
	return flipped, nil
}

func ownedBy(refs []metav1.OwnerReference, group, kind, name string) bool {
	for _, r := range refs {
		gv, err := schema.ParseGroupVersion(r.APIVersion)
		if err == nil && gv.Group == group && r.Kind == kind && r.Name == name {
			return true
		}
	}
	return false
}

func parseSelector(s string) (map[string]string, error) {
	out := map[string]string{}
	for _, part := range strings.Split(s, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok || k == "" {
			return nil, fmt.Errorf("selector %q must be key=value pairs", s)
		}
		out[k] = v
	}
	return out, nil
}

func matches(have, want map[string]string) bool {
	for k, v := range want {
		if have[k] != v {
			return false
		}
	}
	return len(want) > 0
}
