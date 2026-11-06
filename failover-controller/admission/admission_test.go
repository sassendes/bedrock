package admission

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sassendes/bedrock/failover-controller/act"
	"github.com/sassendes/bedrock/failover-controller/store"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// These tests run the real admission.yaml in a real kube-apiserver, the only place CEL is evaluated the
// way the cluster evaluates it. BEDROCK_APISERVER_BIN names a directory with etcd and kube-apiserver.

var clustersGVR = schema.GroupVersionResource{Group: "postgresql.cnpg.io", Version: "v1", Resource: "clusters"}

const admissionFile = "../../tenant-workload/failover/admission.yaml"

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

type apiserver struct {
	admin *rest.Config
}

func start(t *testing.T) *apiserver {
	t.Helper()
	bin := os.Getenv("BEDROCK_APISERVER_BIN")
	if bin == "" {
		t.Skip("set BEDROCK_APISERVER_BIN to a directory with etcd and kube-apiserver")
	}
	dir := t.TempDir()
	etcdPort, peerPort, apiPort := freePort(t), freePort(t), freePort(t)
	etcd := exec.Command(filepath.Join(bin, "etcd"), "--data-dir", filepath.Join(dir, "etcd"),
		"--listen-client-urls", fmt.Sprintf("http://127.0.0.1:%d", etcdPort),
		"--advertise-client-urls", fmt.Sprintf("http://127.0.0.1:%d", etcdPort),
		"--listen-peer-urls", fmt.Sprintf("http://127.0.0.1:%d", peerPort),
		"--initial-advertise-peer-urls", fmt.Sprintf("http://127.0.0.1:%d", peerPort),
		"--initial-cluster", fmt.Sprintf("default=http://127.0.0.1:%d", peerPort))
	var etcdLog bytes.Buffer
	etcd.Stdout, etcd.Stderr = &etcdLog, &etcdLog
	if err := etcd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = etcd.Process.Kill(); _ = etcd.Wait() })

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	keyFile := filepath.Join(dir, "sa.key")
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}), 0o600); err != nil {
		t.Fatal(err)
	}
	tokenFile := filepath.Join(dir, "tokens.csv")
	if err := os.WriteFile(tokenFile, []byte(`admin-token,admin,admin,"system:masters"`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	api := exec.Command(filepath.Join(bin, "kube-apiserver"),
		"--etcd-servers", fmt.Sprintf("http://127.0.0.1:%d", etcdPort),
		"--cert-dir", filepath.Join(dir, "certs"),
		"--bind-address", "127.0.0.1",
		"--secure-port", fmt.Sprint(apiPort),
		"--token-auth-file", tokenFile,
		"--authorization-mode", "RBAC",
		"--service-account-issuer", "https://kubernetes.default.svc",
		"--service-account-key-file", keyFile,
		"--service-account-signing-key-file", keyFile,
		"--service-cluster-ip-range", "10.96.0.0/24",
		"--disable-admission-plugins", "ServiceAccount")
	var apiLog bytes.Buffer
	api.Stdout, api.Stderr = &apiLog, &apiLog
	if err := api.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = api.Process.Kill(); _ = api.Wait() })

	cfg := &rest.Config{
		Host:            fmt.Sprintf("https://127.0.0.1:%d", apiPort),
		BearerToken:     "admin-token",
		TLSClientConfig: rest.TLSClientConfig{Insecure: true},
		Proxy:           func(*http.Request) (*url.URL, error) { return nil, nil },
		QPS:             200,
		Burst:           400,
	}
	kube := kubernetes.NewForConfigOrDie(cfg)
	deadline := time.Now().Add(60 * time.Second)
	for {
		body, err := kube.Discovery().RESTClient().Get().AbsPath("/readyz").DoRaw(context.Background())
		if err == nil && string(body) == "ok" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("apiserver never became ready: %v\n%s\n%s", err, tail(apiLog.String()), tail(etcdLog.String()))
		}
		time.Sleep(200 * time.Millisecond)
	}
	return &apiserver{admin: cfg}
}

func tail(s string) string {
	if len(s) > 4000 {
		return s[len(s)-4000:]
	}
	return s
}

func (a *apiserver) as(user, pod string) *rest.Config {
	c := rest.CopyConfig(a.admin)
	c.Impersonate = rest.ImpersonationConfig{UserName: user, Extra: map[string][]string{"authentication.kubernetes.io/pod-name": {pod}}}
	return c
}

const deciderUser = "system:serviceaccount:bedrock-failover:bedrock-decider"
const deciderPod = "bedrock-decider-0"
const kyvernoUser = "system:serviceaccount:kyverno:kyverno-background-controller"
const strangerUser = "system:serviceaccount:application:grafana"

type world struct {
	t       *testing.T
	ctx     context.Context
	admin   *store.Store
	kube    kubernetes.Interface
	dyn     dynamic.Interface
	dKube   kubernetes.Interface
	dDyn    dynamic.Interface
	decider *act.Actor
	api     *apiserver
}

func mustApply(t *testing.T, ctx context.Context, dyn dynamic.Interface, obj *unstructured.Unstructured) {
	t.Helper()
	gvr := map[string]schema.GroupVersionResource{
		"Lease":                            {Group: "coordination.k8s.io", Version: "v1", Resource: "leases"},
		"ConfigMap":                        {Version: "v1", Resource: "configmaps"},
		"ValidatingAdmissionPolicy":        {Group: "admissionregistration.k8s.io", Version: "v1", Resource: "validatingadmissionpolicies"},
		"ValidatingAdmissionPolicyBinding": {Group: "admissionregistration.k8s.io", Version: "v1", Resource: "validatingadmissionpolicybindings"},
		"CustomResourceDefinition":         {Group: "apiextensions.k8s.io", Version: "v1", Resource: "customresourcedefinitions"},
		"Namespace":                        {Version: "v1", Resource: "namespaces"},
	}[obj.GetKind()]
	if gvr.Resource == "" {
		t.Fatalf("no resource for %s", obj.GetKind())
	}
	var err error
	if obj.GetNamespace() != "" {
		_, err = dyn.Resource(gvr).Namespace(obj.GetNamespace()).Create(ctx, obj, metav1.CreateOptions{})
	} else {
		_, err = dyn.Resource(gvr).Create(ctx, obj, metav1.CreateOptions{})
	}
	if err != nil {
		t.Fatalf("create %s %s: %v", obj.GetKind(), obj.GetName(), err)
	}
}

const clusterCRD = `
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: clusters.postgresql.cnpg.io
spec:
  group: postgresql.cnpg.io
  scope: Namespaced
  names: {kind: Cluster, plural: clusters, singular: cluster, listKind: ClusterList}
  versions:
    - name: v1
      served: true
      storage: true
      subresources: {status: {}}
      schema:
        openAPIV3Schema:
          type: object
          x-kubernetes-preserve-unknown-fields: true
`

func decode(t *testing.T, data []byte) []*unstructured.Unstructured {
	t.Helper()
	var out []*unstructured.Unstructured
	d := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
	for {
		var m map[string]any
		if err := d.Decode(&m); err != nil {
			if err.Error() == "EOF" {
				return out
			}
			t.Fatal(err)
		}
		if len(m) > 0 {
			out = append(out, &unstructured.Unstructured{Object: m})
		}
	}
}

func cluster(name string, replica map[string]any) *unstructured.Unstructured {
	spec := map[string]any{"instances": int64(2)}
	if replica != nil {
		spec["replica"] = replica
	}
	role := "primary"
	if replica != nil {
		role = "live"
		if replica["minApplyDelay"] != nil {
			role = "delayed"
		}
	}
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "postgresql.cnpg.io/v1", "kind": "Cluster",
		"metadata": map[string]any{"name": name, "namespace": name, "labels": map[string]any{"bedrock.io/db-role": role}},
		"spec":     spec,
	}}
}

func setup(t *testing.T) *world {
	a := start(t)
	ctx := context.Background()
	kube := kubernetes.NewForConfigOrDie(a.admin)
	dyn := dynamic.NewForConfigOrDie(a.admin)
	for _, obj := range decode(t, []byte(clusterCRD)) {
		mustApply(t, ctx, dyn, obj)
	}
	for _, ns := range []string{"bedrock-failover", "cluster-1", "cluster-2", "cluster-3", "application"} {
		mustApply(t, ctx, dyn, &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "Namespace", "metadata": map[string]any{"name": ns, "labels": map[string]any{"bedrock.io/failover-scope": "true"}}}})
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		if _, err := dyn.Resource(clustersGVR).Namespace("cluster-1").List(ctx, metav1.ListOptions{}); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("cluster crd never served")
		}
		time.Sleep(100 * time.Millisecond)
	}
	for _, c := range []*unstructured.Unstructured{
		cluster("cluster-1", nil),
		cluster("cluster-2", map[string]any{"enabled": true, "source": "cluster-1"}),
		cluster("cluster-3", map[string]any{"enabled": true, "source": "cluster-1", "minApplyDelay": "5m"}),
	} {
		created, err := dyn.Resource(clustersGVR).Namespace(c.GetNamespace()).Create(ctx, c, metav1.CreateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		_ = unstructured.SetNestedField(created.Object, c.GetName()+"-1", "status", "currentPrimary")
		if _, err := dyn.Resource(clustersGVR).Namespace(c.GetNamespace()).UpdateStatus(ctx, created, metav1.UpdateOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	for i := 1; i <= 2; i++ {
		p := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Namespace: "cluster-1", Name: fmt.Sprintf("cluster-1-%d", i), Labels: map[string]string{"cnpg.io/podRole": "instance", "cnpg.io/cluster": "cluster-1"}},
			Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "postgres", Image: "postgres"}}},
		}
		if _, err := kube.CoreV1().Pods("cluster-1").Create(ctx, p, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Namespace: "application", Name: "postgres-rw", Labels: map[string]string{"bedrock.io/points-at": "cluster-1"}},
		Spec:       corev1.ServiceSpec{Type: corev1.ServiceTypeExternalName, ExternalName: "cluster-1-pooler-rw.cluster-1.svc.cluster.local"},
	}
	if _, err := kube.CoreV1().Services("application").Create(ctx, svc, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(admissionFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, obj := range decode(t, raw) {
		mustApply(t, ctx, dyn, obj)
	}
	lease := map[string]any{"holderIdentity": deciderPod}
	l, err := dyn.Resource(schema.GroupVersionResource{Group: "coordination.k8s.io", Version: "v1", Resource: "leases"}).Namespace("bedrock-failover").Get(ctx, "bedrock-decider", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	l.Object["spec"] = lease
	if _, err := dyn.Resource(schema.GroupVersionResource{Group: "coordination.k8s.io", Version: "v1", Resource: "leases"}).Namespace("bedrock-failover").Update(ctx, l, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}

	rb := []string{
		`{"apiVersion":"rbac.authorization.k8s.io/v1","kind":"ClusterRoleBinding","metadata":{"name":"decider-all"},"roleRef":{"apiGroup":"rbac.authorization.k8s.io","kind":"ClusterRole","name":"cluster-admin"},"subjects":[{"kind":"User","name":"` + deciderUser + `"}]}`,
		`{"apiVersion":"rbac.authorization.k8s.io/v1","kind":"ClusterRoleBinding","metadata":{"name":"others-all"},"roleRef":{"apiGroup":"rbac.authorization.k8s.io","kind":"ClusterRole","name":"cluster-admin"},"subjects":[{"kind":"User","name":"` + kyvernoUser + `"},{"kind":"User","name":"` + strangerUser + `"}]}`,
	}
	for _, r := range rb {
		obj := decode(t, []byte(r))[0]
		if _, err := dyn.Resource(schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "clusterrolebindings"}).Create(ctx, obj, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
	}

	dc := a.as(deciderUser, deciderPod)
	w := &world{t: t, ctx: ctx, api: a, admin: store.New(kube, store.DefaultConfig()), kube: kube, dyn: dyn,
		dKube: kubernetes.NewForConfigOrDie(dc), dDyn: dynamic.NewForConfigOrDie(dc)}
	w.decider = act.New(w.dDyn, w.dKube, act.DefaultConfig())

	deadline = time.Now().Add(30 * time.Second)
	for {
		_, err := w.dDyn.Resource(clustersGVR).Namespace("cluster-3").Patch(ctx, "cluster-3", types.MergePatchType,
			[]byte(`{"metadata":{"annotations":{"bedrock.io/failover-epoch":"9"}},"spec":{"replica":{"enabled":false}}}`),
			metav1.PatchOptions{DryRun: []string{metav1.DryRunAll}})
		if err != nil && strings.Contains(err.Error(), "denied") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("policies never took effect, a promotion with no approval is still admitted: %v", err)
		}
		time.Sleep(200 * time.Millisecond)
	}
	return w
}

func (w *world) approve(a store.Approval) {
	w.t.Helper()
	if err := w.admin.SetApproval(w.ctx, "cluster-1/cluster-1", a); err != nil {
		w.t.Fatal(err)
	}
	time.Sleep(1500 * time.Millisecond)
}

func (w *world) approvalsRaw(mut func(map[string]string)) {
	w.t.Helper()
	cm, err := w.kube.CoreV1().ConfigMaps("bedrock-failover").Get(w.ctx, store.ApprovalsObject, metav1.GetOptions{})
	if err != nil {
		w.t.Fatal(err)
	}
	mut(cm.Data)
	if _, err := w.kube.CoreV1().ConfigMaps("bedrock-failover").Update(w.ctx, cm, metav1.UpdateOptions{}); err != nil {
		w.t.Fatal(err)
	}
	time.Sleep(1500 * time.Millisecond)
}

func (w *world) patchCluster(name string, epoch int64, body map[string]any) error {
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
	an["bedrock.io/failover-epoch"] = fmt.Sprint(epoch)
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	_, err = w.dDyn.Resource(clustersGVR).Namespace(name).Patch(w.ctx, name, types.MergePatchType, b, metav1.PatchOptions{})
	return err
}

func (w *world) flip(host string, epoch int64) error {
	b, _ := json.Marshal(map[string]any{
		"metadata": map[string]any{
			"labels":      map[string]any{"bedrock.io/points-at": "cluster-2"},
			"annotations": map[string]any{"bedrock.io/failover-epoch": fmt.Sprint(epoch)},
		},
		"spec": map[string]any{"externalName": host},
	})
	_, err := w.dKube.CoreV1().Services("application").Patch(w.ctx, "postgres-rw", types.MergePatchType, b, metav1.PatchOptions{})
	return err
}

func denied(t *testing.T, what string, err error, part string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s was admitted", what)
	}
	if !strings.Contains(err.Error(), part) {
		t.Fatalf("%s denied for the wrong reason: %v", what, err)
	}
}

func allowed(t *testing.T, what string, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s was refused: %v", what, err)
	}
}

const host2 = "cluster-2-pooler-rw.cluster-2.svc.cluster.local"

func good(epoch int64) store.Approval {
	return store.Approval{State: store.ApprovalValid, Epoch: epoch, Round: 1, Instance: "cluster-1-1", Candidate: "cluster-2/cluster-2", Host: host2}
}

func TestAdmissionAgainstARealAPIServer(t *testing.T) {
	w := setup(t)

	t.Run("the candidate the verifier did not check is never promoted", func(t *testing.T) {
		w.approve(good(1))
		denied(t, "promoting the delayed replica", w.decider.Promote(w.ctx, "cluster-3/cluster-3", 1), "only the candidate the verifier checked")
	})

	t.Run("an approval with no candidate key promotes nothing", func(t *testing.T) {
		w.approve(good(1))
		w.approvalsRaw(func(d map[string]string) { delete(d, "cluster-1.cluster-1.candidate") })
		denied(t, "promoting with the candidate key missing", w.decider.Promote(w.ctx, "cluster-2/cluster-2", 1), "only the candidate the verifier checked")
	})

	t.Run("an approval for another epoch promotes nothing", func(t *testing.T) {
		w.approve(good(2))
		denied(t, "promoting with an old epoch", w.decider.Promote(w.ctx, "cluster-2/cluster-2", 1), "valid approval for the epoch")
	})

	t.Run("a revoked slot names no candidate and promotes nothing", func(t *testing.T) {
		w.approve(good(1))
		w.approve(store.Approval{State: store.ApprovalRevoked, Epoch: 1, Round: 2, Instance: "cluster-1-1", Candidate: "cluster-2/cluster-2", Host: host2})
		denied(t, "promoting after a revoke", w.decider.Promote(w.ctx, "cluster-2/cluster-2", 1), "valid approval for the epoch")
		w.approvalsRaw(func(d map[string]string) { d["cluster-1.cluster-1.state"] = "valid" })
		denied(t, "promoting from a revoked slot set valid by hand", w.decider.Promote(w.ctx, "cluster-2/cluster-2", 1), "only the candidate the verifier checked")
	})

	t.Run("only the dead primary is fenced", func(t *testing.T) {
		w.approve(good(1))
		denied(t, "fencing the candidate", w.patchCluster("cluster-2", 1, map[string]any{"metadata": map[string]any{"annotations": map[string]any{"cnpg.io/hibernation": "on"}}}), "only the approved dead primary may be fenced")
		denied(t, "fencing the delayed replica", w.patchCluster("cluster-3", 1, map[string]any{"metadata": map[string]any{"annotations": map[string]any{"cnpg.io/reconciliationLoop": "disabled"}}}), "only the approved dead primary may be fenced")
	})

	t.Run("the decider touches nothing in spec.replica but enabled and source", func(t *testing.T) {
		w.approve(good(1))
		denied(t, "dropping the apply delay", w.patchCluster("cluster-3", 1, map[string]any{"spec": map[string]any{"replica": map[string]any{"minApplyDelay": nil}}}), "nothing else in spec.replica")
		denied(t, "turning the primary into a replica", w.patchCluster("cluster-1", 1, map[string]any{"spec": map[string]any{"replica": map[string]any{"enabled": true, "source": "cluster-2"}}}), "never turns a cluster into a replica")
	})

	t.Run("the role label moves only with the promotion", func(t *testing.T) {
		w.approve(good(1))
		denied(t, "relabelling the delayed replica live", w.patchCluster("cluster-3", 1, map[string]any{"metadata": map[string]any{"labels": map[string]any{"bedrock.io/db-role": "live"}}}), "only changes to primary")
		denied(t, "crowning the candidate without promoting it", w.patchCluster("cluster-2", 1, map[string]any{"metadata": map[string]any{"labels": map[string]any{"bedrock.io/db-role": "primary"}}}), "only changes to primary")
		denied(t, "promoting without the label", w.patchCluster("cluster-2", 1, map[string]any{"spec": map[string]any{"replica": map[string]any{"enabled": false}}}), "sets bedrock.io/db-role to primary")
		denied(t, "touching another label", w.patchCluster("cluster-2", 1, map[string]any{"metadata": map[string]any{"labels": map[string]any{"team": "x"}}}), "changes no label but")
	})

	t.Run("only their owners write votes, claims and database cas", func(t *testing.T) {
		write := func(user, name string) error {
			kc := kubernetes.NewForConfigOrDie(w.api.as(user, "some-pod"))
			_, err := kc.CoreV1().ConfigMaps("bedrock-failover").Create(w.ctx, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: name}, Data: map[string]string{"ca.crt": "x"}}, metav1.CreateOptions{})
			return err
		}
		allowed(t, "kyverno writing a database ca", write(kyvernoUser, "db-ca-cluster-9"))
		denied(t, "kyverno writing a vote", write(kyvernoUser, "vote-n1-a"), "no workload but an observer writes votes")
		denied(t, "kyverno writing a claim", write(kyvernoUser, "claim-cluster-1.cluster-1"), "no workload but the decider writes claims")
		denied(t, "kyverno writing anything else", write(kyvernoUser, "something"), "kyverno writes nothing here but the database cas")
		denied(t, "another workload writing a database ca", write(strangerUser, "db-ca-cluster-8"), "no workload but kyverno writes the database cas")
		denied(t, "another workload writing a claim", write(strangerUser, "claim-cluster-2.cluster-2"), "no workload but the decider writes claims")
		denied(t, "another workload writing a vote", write(strangerUser, "vote-n2-b"), "no workload but an observer writes votes")
	})

	t.Run("only a follower is repointed", func(t *testing.T) {
		w.approve(good(1))
		denied(t, "repointing the candidate", w.patchCluster("cluster-2", 1, map[string]any{"spec": map[string]any{"replica": map[string]any{"source": "cluster-3"}}}), "only a follower is repointed")
	})

	t.Run("postgres-rw only points at the approved host", func(t *testing.T) {
		w.approve(good(1))
		denied(t, "flipping to the delayed replica", w.flip("cluster-3-pooler-rw.cluster-3.svc.cluster.local", 1), "only point at the host the verifier approved")
		denied(t, "flipping outside the cluster", w.flip("db.example.com", 1), "only point inside the cluster")
		w.approvalsRaw(func(d map[string]string) { delete(d, "cluster-1.cluster-1.host") })
		denied(t, "flipping with the host key missing", w.flip(host2, 1), "only point at the host the verifier approved")
	})

	t.Run("a failover pod runs only the pinned failover image with limits", func(t *testing.T) {
		pod := func(image string, limits bool) *corev1.Pod {
			c := corev1.Container{Name: "c", Image: image}
			if limits {
				c.Resources.Limits = corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m"), corev1.ResourceMemory: resource.MustParse("64Mi")}
			}
			return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{GenerateName: "p-", Namespace: "bedrock-failover"}, Spec: corev1.PodSpec{Containers: []corev1.Container{c}}}
		}
		const pinned = "ghcr.io/sassendes/bedrock-failover:0.1.0@sha256:2deaf442b7e7dcd7f027a3920696e511213a8fce0391744ff44a284c9782e50a"
		create := func(p *corev1.Pod) error {
			_, err := w.kube.CoreV1().Pods("bedrock-failover").Create(w.ctx, p, metav1.CreateOptions{})
			return err
		}
		allowed(t, "the pinned image", create(pod(pinned, true)))
		denied(t, "a tag without a digest", create(pod("ghcr.io/sassendes/bedrock-failover:latest", true)), "pinned by digest")
		denied(t, "another image", create(pod("docker.io/library/busybox@sha256:"+strings.Repeat("a", 64), true)), "pinned by digest")
		denied(t, "no limits", create(pod(pinned, false)), "limits are required")
	})

	t.Run("the whole approved run is admitted", func(t *testing.T) {
		w.approve(good(1))
		allowed(t, "fence", w.decider.Fence(w.ctx, "cluster-1/cluster-1", 1))
		pods, err := w.kube.CoreV1().Pods("cluster-1").List(w.ctx, metav1.ListOptions{})
		if err != nil || len(pods.Items) != 0 {
			t.Fatalf("fence left %d pods: %v", len(pods.Items), err)
		}
		allowed(t, "repoint the delayed follower", w.patchCluster("cluster-3", 1, map[string]any{"spec": map[string]any{"replica": map[string]any{"source": "cluster-2"}}}))
		allowed(t, "promote", w.decider.Promote(w.ctx, "cluster-2/cluster-2", 1))
		allowed(t, "flip", w.flip(host2, 1))
		svc, err := w.kube.CoreV1().Services("application").Get(w.ctx, "postgres-rw", metav1.GetOptions{})
		if err != nil || svc.Spec.ExternalName != host2 {
			t.Fatalf("postgres-rw %v %v", svc.Spec.ExternalName, err)
		}
	})
}
