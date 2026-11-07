package cli

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"flag"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"
)

func selfSigned(t *testing.T, cn string) string {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: cn}, NotBefore: time.Now(), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &k.PublicKey, k)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func caMap(name, ns, crt string, labelled bool) *corev1.ConfigMap {
	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns}, Data: map[string]string{"ca.crt": crt}}
	if labelled {
		cm.Labels = map[string]string{"bedrock.io/db-ca": "true"}
	}
	return cm
}

func TestConfigMapCAsFollowNewClusters(t *testing.T) {
	ctx := context.Background()
	kube := kubefake.NewSimpleClientset(
		caMap("db-ca-cluster-2", "bedrock-failover", selfSigned(t, "two"), true),
		caMap("db-ca-cluster-1", "bedrock-failover", selfSigned(t, "one"), true),
		caMap("unrelated", "bedrock-failover", selfSigned(t, "nope"), false),
		caMap("db-ca-elsewhere", "other", selfSigned(t, "far"), true),
	)
	out := filepath.Join(t.TempDir(), "bundle.crt")
	b, err := NewCABundle(ctx, ConfigMapCAs(kube, "bedrock-failover", "bedrock.io/db-ca=true", "ca.crt"), out)
	if err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(out)
	if n := strings.Count(string(first), "BEGIN CERTIFICATE"); n != 2 {
		t.Fatalf("want the 2 labelled cas of the namespace, got %d", n)
	}

	if _, err := kube.CoreV1().ConfigMaps("bedrock-failover").Create(ctx, caMap("db-ca-cluster-4", "bedrock-failover", selfSigned(t, "four"), true), metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := b.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(out)
	if n := strings.Count(string(second), "BEGIN CERTIFICATE"); n != 3 {
		t.Fatalf("a cluster added later must be picked up without a restart, got %d cas", n)
	}
}

func TestBadOrMissingCAsKeepTheLastGoodBundle(t *testing.T) {
	ctx := context.Background()
	kube := kubefake.NewSimpleClientset(caMap("db-ca-cluster-1", "bedrock-failover", selfSigned(t, "one"), true))
	b, err := NewCABundle(ctx, ConfigMapCAs(kube, "bedrock-failover", "bedrock.io/db-ca=true", "ca.crt"), "")
	if err != nil {
		t.Fatal(err)
	}
	good := b.Pool()
	cm, _ := kube.CoreV1().ConfigMaps("bedrock-failover").Get(ctx, "db-ca-cluster-1", metav1.GetOptions{})
	cm.Data["ca.crt"] = "garbage"
	if _, err := kube.CoreV1().ConfigMaps("bedrock-failover").Update(ctx, cm, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := b.Refresh(ctx); err == nil {
		t.Fatal("a ca that is not a certificate must be an error")
	}
	if b.Pool() != good {
		t.Fatal("a failed refresh must keep the last good pool")
	}
	if _, err := NewCABundle(ctx, ConfigMapCAs(kubefake.NewSimpleClientset(), "bedrock-failover", "bedrock.io/db-ca=true", "ca.crt"), ""); err == nil {
		t.Fatal("no ca at all must refuse to start, not trust nothing silently")
	}
}

func TestDirCAsStillWork(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cluster-1.crt"), []byte(selfSigned(t, "one")), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewCABundle(context.Background(), DirCAs(dir), ""); err != nil {
		t.Fatal(err)
	}
}

func TestEveryBinaryCanAddItsOwnFlagsOnTopOfTheCommonOnes(t *testing.T) {
	for _, name := range []string{"role-label", "claim-prefix", "vote-prefix", "observer-selector", "pg-sslmode", "key-dir"} {
		fs := flag.NewFlagSet("t", flag.ContinueOnError)
		Register(fs)
		RegisterCA(fs)
		if fs.Lookup(name) != nil {
			t.Fatalf("the common flags already define -%s, so a binary that adds its own panics at startup", name)
		}
	}
}
