package cli

import (
	"bytes"
	"crypto/x509"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
	Kubeconfig  string
	MetricsAddr string
	Namespace   string
	drZones     string
	Act         act.Config
}

func Register(fs *flag.FlagSet) *Common {
	c := &Common{Act: act.DefaultConfig()}
	d := &c.Act.Discovery
	fs.StringVar(&c.Kubeconfig, "kubeconfig", os.Getenv("KUBECONFIG"), "path to a kubeconfig, empty means in-cluster")
	fs.StringVar(&c.MetricsAddr, "metrics-addr", ":9090", "address for /metrics and /healthz")
	fs.StringVar(&c.Namespace, "namespace", Env("POD_NAMESPACE", "bedrock-failover"), "namespace of the failover components")
	fs.StringVar(&c.drZones, "dr-zones", "", "comma separated zones that hold dr clusters")
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
	fs.StringVar(&c.Act.ClusterDomain, "cluster-domain", c.Act.ClusterDomain, "cluster dns domain")
	fs.StringVar(&c.Act.HealthyPhase, "healthy-phase", c.Act.HealthyPhase, "phase of a healthy cluster")
	return c
}

func (c *Common) Finish() {
	c.Act.PoolerGVR.Group = c.Act.Discovery.ClusterGVR.Group
	c.Act.PoolerGVR.Version = c.Act.Discovery.ClusterGVR.Version
	for _, z := range strings.Split(c.drZones, ",") {
		if z = strings.TrimSpace(z); z != "" {
			c.Act.Discovery.DRZones[z] = true
		}
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

func (c *Common) Serve(m *metrics.Registry, log *slog.Logger) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", m)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
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
