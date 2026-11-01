package checker

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/sassendes/bedrock/failover-controller/act"
	"github.com/sassendes/bedrock/failover-controller/discovery"
	"github.com/sassendes/bedrock/failover-controller/metrics"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

type Config struct {
	Scheme   string
	Port     int
	Path     string
	Interval time.Duration
	Timeout  time.Duration
}

func DefaultConfig() Config {
	return Config{Scheme: "https", Port: 8000, Path: "/healthz", Interval: 5 * time.Second, Timeout: 2 * time.Second}
}

// Checker is the fallback when there is no majority to vote. It asks each primary's instance manager
// if it is healthy and exports the answer. It never votes and never acts: alerts page on it.
type Checker struct {
	kube    kubernetes.Interface
	dyn     dynamic.Interface
	disc    discovery.Config
	actor   *act.Actor
	cfg     Config
	roots   *x509.CertPool
	metrics *metrics.Registry
	log     *slog.Logger
	now     func() time.Time
}

func New(kube kubernetes.Interface, dyn dynamic.Interface, disc discovery.Config, actor *act.Actor, cfg Config, roots *x509.CertPool, m *metrics.Registry, log *slog.Logger) *Checker {
	return &Checker{kube: kube, dyn: dyn, disc: disc, actor: actor, cfg: cfg, roots: roots, metrics: m, log: log, now: time.Now}
}

// client verifies the instance's certificate against the database CAs under the primary Service's
// name, which CNPG puts in every server certificate.
func (c *Checker) client(serverName string) *http.Client {
	tr := &http.Transport{DisableKeepAlives: true}
	if c.cfg.Scheme == "https" {
		tr.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: c.roots, ServerName: serverName}
	}
	return &http.Client{Timeout: c.cfg.Timeout, Transport: tr}
}

func (c *Checker) Run(ctx context.Context, done func()) {
	for {
		if c.Round(ctx) {
			done()
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(c.cfg.Interval):
		}
	}
}

func (c *Checker) Round(ctx context.Context) bool {
	d, err := discovery.Discover(ctx, c.dyn, c.kube, c.disc)
	if err != nil {
		c.log.Warn("discovery", "err", err)
		return false
	}
	for _, f := range d.Families {
		p := f.Primary
		if obj, err := c.actor.Get(ctx, p.Key()); err == nil && c.actor.Fenced(obj) {
			continue
		}
		healthy, reason := c.check(ctx, p)
		v := 0.0
		if healthy {
			v = 1
		} else {
			c.log.Warn("primary unhealthy", "target", p.Key(), "reason", reason)
		}
		c.metrics.Set("bedrock_checker_primary_healthy", map[string]string{"target": p.Key()}, v)
	}
	c.metrics.Set("bedrock_checker_last_round_timestamp_seconds", nil, float64(c.now().Unix()))
	return true
}

func (c *Checker) check(ctx context.Context, m discovery.Member) (bool, string) {
	if m.CurrentPrimary == "" {
		return false, "no current primary"
	}
	pod, err := c.kube.CoreV1().Pods(m.Namespace).Get(ctx, m.CurrentPrimary, metav1.GetOptions{})
	if err != nil {
		return false, err.Error()
	}
	if pod.Status.PodIP == "" {
		return false, "primary pod has no ip"
	}
	serverName, err := c.actor.PrimaryService(ctx, m.Key())
	if err != nil {
		return false, err.Error()
	}
	url := fmt.Sprintf("%s://%s%s", c.cfg.Scheme, net.JoinHostPort(pod.Status.PodIP, strconv.Itoa(c.cfg.Port)), c.cfg.Path)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, err.Error()
	}
	resp, err := c.client(serverName).Do(req)
	if err != nil {
		return false, err.Error()
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		return false, resp.Status
	}
	return true, ""
}
