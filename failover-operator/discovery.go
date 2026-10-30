package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

type Config struct {
	ClusterGVR       schema.GroupVersionResource
	ClusterKind      string
	ClusterLabel     string
	InstanceSelector string
	ZoneLabel        string
	DRZones          map[string]bool
}

func defaultConfig() Config {
	return Config{
		ClusterGVR:       schema.GroupVersionResource{Group: "postgresql.cnpg.io", Version: "v1", Resource: "clusters"},
		ClusterKind:      "Cluster",
		ClusterLabel:     "cnpg.io/cluster",
		InstanceSelector: "cnpg.io/podRole=instance",
		ZoneLabel:        "topology.kubernetes.io/zone",
		DRZones:          map[string]bool{},
	}
}

type Role string

const (
	RolePrimary Role = "primary"
	RoleLive    Role = "live"
	RoleDelayed Role = "delayed"
	RoleDR      Role = "dr"
)

type Member struct {
	Namespace      string
	Name           string
	UID            string
	Role           Role
	Instances      int64
	ReadyInstances int64
	CurrentPrimary string
	Phase          string
	ApplyDelay     string
	Zones          []string
	SourceHost     string
}

func (m Member) Key() string { return m.Namespace + "/" + m.Name }

type Family struct {
	Primary Member
	Live    []Member
	Delayed []Member
	DR      []Member
}

type Problem struct {
	Cluster string
	Reason  string
}

type Discovery struct {
	Families []Family
	Problems []Problem
}

func discover(ctx context.Context, dyn dynamic.Interface, kube kubernetes.Interface, cfg Config) (Discovery, error) {
	var out Discovery

	list, err := dyn.Resource(cfg.ClusterGVR).Namespace(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return out, fmt.Errorf("list %s: %w", cfg.ClusterGVR.Resource, err)
	}

	zones, err := clusterZones(ctx, kube, cfg)
	if err != nil {
		return out, err
	}

	members := map[string]*Member{}
	var replicas []*Member

	for i := range list.Items {
		obj := &list.Items[i]
		m := readMember(obj)
		m.Zones = zones[m.Key()]
		key := m.Key()

		if _, set, _ := unstructured.NestedString(obj.Object, "spec", "replica", "primary"); set {
			out.Problems = append(out.Problems, Problem{key, "uses the distributed topology (spec.replica.primary), not supported"})
			continue
		}

		_, hasReplica, _ := unstructured.NestedMap(obj.Object, "spec", "replica")
		enabled, hasEnabled, _ := unstructured.NestedBool(obj.Object, "spec", "replica", "enabled")
		if hasReplica && !hasEnabled {
			out.Problems = append(out.Problems, Problem{key, "replica stanza without replica.enabled, role is ambiguous"})
			continue
		}
		if !enabled {
			m.Role = RolePrimary
			members[key] = &m
			continue
		}

		host, ok := sourceHost(obj)
		if !ok {
			out.Problems = append(out.Problems, Problem{key, "replica source has no matching externalClusters host"})
			continue
		}
		m.SourceHost = host

		inDR, mixed := zoneClass(m.Zones, cfg.DRZones)
		switch {
		case mixed:
			out.Problems = append(out.Problems, Problem{key, "instances spread across dr and main zones"})
			continue
		case inDR:
			m.Role = RoleDR
		case m.ApplyDelay != "":
			m.Role = RoleDelayed
		default:
			m.Role = RoleLive
		}
		members[key] = &m
		replicas = append(replicas, &m)
	}

	families := map[string]*Family{}
	for key, m := range members {
		if m.Role == RolePrimary {
			families[key] = &Family{Primary: *m}
		}
	}

	for _, r := range replicas {
		target, reason, err := resolveSource(ctx, kube, cfg, r.SourceHost, r.Namespace)
		if err != nil {
			return Discovery{}, err
		}
		if reason != "" {
			out.Problems = append(out.Problems, Problem{r.Key(), reason})
			continue
		}
		src, found := members[target]
		if !found {
			out.Problems = append(out.Problems, Problem{r.Key(), fmt.Sprintf("source %s is not a known %s", target, cfg.ClusterKind)})
			continue
		}
		if src.Role != RolePrimary {
			out.Problems = append(out.Problems, Problem{r.Key(), fmt.Sprintf("follows %s, which is a replica itself", target)})
			continue
		}
		f := families[target]
		switch r.Role {
		case RoleLive:
			f.Live = append(f.Live, *r)
		case RoleDelayed:
			f.Delayed = append(f.Delayed, *r)
		case RoleDR:
			f.DR = append(f.DR, *r)
		}
	}

	keys := make([]string, 0, len(families))
	for k := range families {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		f := families[k]
		sortMembers(f.Live)
		sortMembers(f.Delayed)
		sortMembers(f.DR)
		out.Families = append(out.Families, *f)
	}
	sort.Slice(out.Problems, func(i, j int) bool { return out.Problems[i].Cluster < out.Problems[j].Cluster })
	return out, nil
}

func readMember(obj *unstructured.Unstructured) Member {
	m := Member{
		Namespace: obj.GetNamespace(),
		Name:      obj.GetName(),
		UID:       string(obj.GetUID()),
	}
	m.Instances, _, _ = unstructured.NestedInt64(obj.Object, "spec", "instances")
	m.ReadyInstances, _, _ = unstructured.NestedInt64(obj.Object, "status", "readyInstances")
	m.CurrentPrimary, _, _ = unstructured.NestedString(obj.Object, "status", "currentPrimary")
	m.Phase, _, _ = unstructured.NestedString(obj.Object, "status", "phase")

	delay, _, _ := unstructured.NestedString(obj.Object, "spec", "replica", "minApplyDelay")
	if isZeroDelay(delay) {
		delay, _, _ = unstructured.NestedString(obj.Object, "spec", "postgresql", "parameters", "recovery_min_apply_delay")
	}
	if !isZeroDelay(delay) {
		m.ApplyDelay = delay
	}
	return m
}

func isZeroDelay(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return true
	}
	if d, err := time.ParseDuration(s); err == nil {
		return d == 0
	}
	return strings.Trim(s, "0") == ""
}

func sourceHost(obj *unstructured.Unstructured) (string, bool) {
	source, _, _ := unstructured.NestedString(obj.Object, "spec", "replica", "source")
	if source == "" {
		return "", false
	}
	externals, _, _ := unstructured.NestedSlice(obj.Object, "spec", "externalClusters")
	for _, e := range externals {
		ext, ok := e.(map[string]any)
		if !ok {
			continue
		}
		if name, _, _ := unstructured.NestedString(ext, "name"); name != source {
			continue
		}
		host, _, _ := unstructured.NestedString(ext, "connectionParameters", "host")
		return host, host != ""
	}
	return "", false
}

func parseServiceHost(host, defaultNamespace string) (name, namespace string, ok bool) {
	host = strings.ToLower(strings.TrimSpace(host))
	if i := strings.Index(host, ".svc"); i >= 0 {
		host = host[:i]
	}
	parts := strings.Split(host, ".")
	switch {
	case len(parts) == 1 && parts[0] != "":
		return parts[0], defaultNamespace, true
	case len(parts) == 2 && parts[0] != "" && parts[1] != "":
		return parts[0], parts[1], true
	}
	return "", "", false
}

func resolveSource(ctx context.Context, kube kubernetes.Interface, cfg Config, host, defaultNamespace string) (key, reason string, err error) {
	name, ns, ok := parseServiceHost(host, defaultNamespace)
	if !ok {
		return "", fmt.Sprintf("source host %q is not an in-cluster service", host), nil
	}
	svc, err := kube.CoreV1().Services(ns).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return "", fmt.Sprintf("source service %s/%s does not exist", ns, name), nil
	}
	if err != nil {
		return "", "", fmt.Errorf("get service %s/%s: %w", ns, name, err)
	}
	for _, ref := range svc.OwnerReferences {
		gv, perr := schema.ParseGroupVersion(ref.APIVersion)
		if perr != nil || gv.Group != cfg.ClusterGVR.Group || ref.Kind != cfg.ClusterKind {
			continue
		}
		return ns + "/" + ref.Name, "", nil
	}
	return "", fmt.Sprintf("source service %s/%s is not owned by a %s", ns, name, cfg.ClusterKind), nil
}

func clusterZones(ctx context.Context, kube kubernetes.Interface, cfg Config) (map[string][]string, error) {
	nodes, err := kube.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list nodes: %w", err)
	}
	nodeZone := map[string]string{}
	for _, n := range nodes.Items {
		nodeZone[n.Name] = n.Labels[cfg.ZoneLabel]
	}

	pods, err := kube.CoreV1().Pods(metav1.NamespaceAll).List(ctx, metav1.ListOptions{LabelSelector: cfg.InstanceSelector})
	if err != nil {
		return nil, fmt.Errorf("list instance pods: %w", err)
	}
	seen := map[string]map[string]bool{}
	for _, p := range pods.Items {
		cluster := p.Labels[cfg.ClusterLabel]
		if p.Spec.NodeName == "" || cluster == "" || p.DeletionTimestamp != nil {
			continue
		}
		if p.Status.Phase == corev1.PodFailed || p.Status.Phase == corev1.PodSucceeded {
			continue
		}
		key := p.Namespace + "/" + cluster
		if seen[key] == nil {
			seen[key] = map[string]bool{}
		}
		seen[key][nodeZone[p.Spec.NodeName]] = true
	}
	out := map[string][]string{}
	for key, set := range seen {
		for z := range set {
			out[key] = append(out[key], z)
		}
		sort.Strings(out[key])
	}
	return out, nil
}

func zoneClass(zones []string, drZones map[string]bool) (inDR, mixed bool) {
	if len(zones) == 0 || len(drZones) == 0 {
		return false, false
	}
	dr := 0
	for _, z := range zones {
		if drZones[z] {
			dr++
		}
	}
	return dr == len(zones), dr > 0 && dr < len(zones)
}

func sortMembers(ms []Member) {
	sort.Slice(ms, func(i, j int) bool { return ms[i].Key() < ms[j].Key() })
}
