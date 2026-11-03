package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sassendes/bedrock/failover-controller/act"
	"github.com/sassendes/bedrock/failover-controller/metrics"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

func Env(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

// Common holds every flag the three binaries share, so a name changed once is changed everywhere.
type Common struct {
	Kubeconfig     string
	MetricsAddr    string
	Namespace      string
	Act            act.Config
	flipNamespaces string
	flipList       []string
}

func Register(fs *flag.FlagSet) *Common {
	c := &Common{Act: act.DefaultConfig()}
	d := &c.Act.Discovery
	fs.StringVar(&c.Kubeconfig, "kubeconfig", os.Getenv("KUBECONFIG"), "path to a kubeconfig, empty means in-cluster")
	fs.StringVar(&c.MetricsAddr, "metrics-addr", ":9090", "address for /metrics and /healthz")
	fs.StringVar(&c.Namespace, "namespace", Env("POD_NAMESPACE", "bedrock-failover"), "namespace of the failover components")
	fs.StringVar(&d.ClusterGVR.Group, "cluster-group", d.ClusterGVR.Group, "api group of the database cluster resource")
	fs.StringVar(&d.ClusterGVR.Version, "cluster-version", d.ClusterGVR.Version, "api version of the database cluster resource")
	fs.StringVar(&d.ClusterGVR.Resource, "cluster-resource", d.ClusterGVR.Resource, "plural resource name of the database cluster")
	fs.StringVar(&d.ClusterKind, "cluster-kind", d.ClusterKind, "kind that owns a cluster's services")
	fs.StringVar(&d.ClusterLabel, "cluster-label", d.ClusterLabel, "pod label holding the cluster name")
	fs.StringVar(&d.InstanceSelector, "instance-selector", d.InstanceSelector, "label selector for database instance pods")
	fs.StringVar(&d.ZoneLabel, "zone-label", d.ZoneLabel, "node label holding the zone")
	fs.StringVar(&c.Act.PoolerGVR.Resource, "pooler-resource", c.Act.PoolerGVR.Resource, "plural resource name of the pooler")
	fs.StringVar(&c.Act.PoolerKind, "pooler-kind", c.Act.PoolerKind, "kind that owns a pooler's service")
	fs.StringVar(&c.Act.ReconcileAnnotation, "reconcile-annotation", c.Act.ReconcileAnnotation, "annotation that stops the operator reconciling a cluster")
	fs.StringVar(&c.Act.ReconcileDisabled, "reconcile-disabled", c.Act.ReconcileDisabled, "value of the reconcile annotation that disables it")
	fs.StringVar(&c.Act.HibernationAnnotation, "hibernation-annotation", c.Act.HibernationAnnotation, "hibernation annotation")
	fs.StringVar(&c.Act.HibernationOn, "hibernation-on", c.Act.HibernationOn, "value that turns hibernation on")
	fs.StringVar(&c.Act.PrimaryServiceSelector, "primary-service-selector", c.Act.PrimaryServiceSelector, "selector a cluster's primary service carries")
	fs.StringVar(&c.Act.FlipLabel, "flip-label", c.Act.FlipLabel, "label on the ExternalName services to repoint")
	fs.StringVar(&c.flipNamespaces, "flip-namespaces", strings.Join(c.Act.FlipNamespaces, ","), "comma separated namespaces whose ExternalName services are repointed, and no others")
	fs.StringVar(&c.Act.ClusterDomain, "cluster-domain", c.Act.ClusterDomain, "cluster dns domain")
	fs.StringVar(&c.Act.HealthyPhase, "healthy-phase", c.Act.HealthyPhase, "phase of a healthy cluster")
	return c
}

func (c *Common) Finish() {
	c.Act.PoolerGVR.Group = c.Act.Discovery.ClusterGVR.Group
	c.Act.PoolerGVR.Version = c.Act.Discovery.ClusterGVR.Version
	for _, ns := range strings.Split(c.flipNamespaces, ",") {
		if ns = strings.TrimSpace(ns); ns != "" {
			c.flipList = append(c.flipList, ns)
		}
	}
	if len(c.flipList) > 0 {
		c.Act.FlipNamespaces = c.flipList
	}
}

func (c *Common) Clients(log *slog.Logger) (dynamic.Interface, kubernetes.Interface) {
	rc, err := clientcmd.BuildConfigFromFlags("", c.Kubeconfig)
	if err != nil {
		log.Error("kube config", "err", err)
		os.Exit(1)
	}
	rc.Timeout = 10 * time.Second
	return dynamic.NewForConfigOrDie(rc), kubernetes.NewForConfigOrDie(rc)
}

// Health is green only while the loop keeps finishing rounds, so a stuck loop gets restarted.
type Health struct {
	mu   sync.Mutex
	last time.Time
	max  time.Duration
}

func NewHealth(max time.Duration) *Health { return &Health{last: time.Now(), max: max} }

func (h *Health) Mark() {
	h.mu.Lock()
	h.last = time.Now()
	h.mu.Unlock()
}

func (h *Health) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	h.mu.Lock()
	age := time.Since(h.last)
	h.mu.Unlock()
	if age > h.max {
		http.Error(w, "last good round "+age.Round(time.Second).String()+" ago", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (c *Common) Serve(m *metrics.Registry, h *Health, log *slog.Logger) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", m)
	mux.Handle("/healthz", h)
	srv := &http.Server{Addr: c.MetricsAddr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := srv.ListenAndServe(); err != nil {
			log.Error("metrics server", "err", err)
			os.Exit(1)
		}
	}()
}

func Logger() *slog.Logger { return slog.New(slog.NewJSONHandler(os.Stdout, nil)) }

// LoadCADir reads every *.crt in dir. The database CAs of all clusters that can become primary live there.
func LoadCADir(dir string) (*x509.CertPool, []byte, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.crt"))
	if err != nil {
		return nil, nil, err
	}
	sort.Strings(files)
	pool := x509.NewCertPool()
	var bundle bytes.Buffer
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			return nil, nil, err
		}
		if !pool.AppendCertsFromPEM(b) {
			return nil, nil, errors.New(f + " holds no certificate")
		}
		bundle.Write(bytes.TrimSpace(b))
		bundle.WriteByte('\n')
	}
	if len(files) == 0 {
		return nil, nil, errors.New("no *.crt in " + dir)
	}
	return pool, bundle.Bytes(), nil
}

// CABundle keeps the database CAs current. A new cluster's CA, or a renewed one, reaches the projected
// volume without a restart; the bundle file postgres connections read is rewritten atomically when it changes.
type CABundle struct {
	dir  string
	out  string
	mu   sync.RWMutex
	pool *x509.CertPool
	sum  [32]byte
}

func NewCABundle(dir, out string) (*CABundle, error) {
	b := &CABundle{dir: dir, out: out}
	if err := b.Refresh(); err != nil {
		return nil, err
	}
	return b, nil
}

func (b *CABundle) Pool() *x509.CertPool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.pool
}

func (b *CABundle) Refresh() error {
	pool, bundle, err := LoadCADir(b.dir)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(bundle)
	b.mu.RLock()
	same := b.pool != nil && sum == b.sum
	b.mu.RUnlock()
	if same {
		return nil
	}
	if b.out != "" {
		tmp := b.out + ".new"
		if err := os.WriteFile(tmp, bundle, 0o600); err != nil {
			return err
		}
		if err := os.Rename(tmp, b.out); err != nil {
			return err
		}
	}
	b.mu.Lock()
	b.pool, b.sum = pool, sum
	b.mu.Unlock()
	return nil
}

func (b *CABundle) Watch(ctx context.Context, every time.Duration, log *slog.Logger) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := b.Refresh(); err != nil {
				log.Warn("database cas not reloaded, keeping the last good ones", "err", err)
			}
		}
	}
}
