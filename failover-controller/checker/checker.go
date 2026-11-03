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
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sassendes/bedrock/failover-controller/act"
	"github.com/sassendes/bedrock/failover-controller/discovery"
	"github.com/sassendes/bedrock/failover-controller/metrics"
	"github.com/sassendes/bedrock/failover-controller/observe"
	"github.com/sassendes/bedrock/failover-controller/store"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

type Config struct {
	Scheme           string
	Port             int
	Path             string
	Interval         time.Duration
	Timeout          time.Duration
	Namespace        string
	LeaseName        string
	RoundStale       time.Duration
	PerNode          int
	ObserverSelector string
}

func DefaultConfig() Config {
	return Config{
		Scheme: "https", Port: 8000, Path: "/readyz", Interval: 5 * time.Second, Timeout: 2 * time.Second,
		Namespace: "bedrock-failover", LeaseName: "bedrock-decider", RoundStale: 15 * time.Second, PerNode: 2,
		ObserverSelector: "app.kubernetes.io/name=bedrock-observer",
	}
}

// Checker is the fallback. It sleeps while the normal path works. When the verifier stops closing rounds,
// the decider has no leader, or the observers lose quorum on a target, it asks the primary's instance
// manager itself: up stops everything, down pages a human. It never fails anything over.
type Checker struct {
	kube    kubernetes.Interface
	dyn     dynamic.Interface
	disc    discovery.Config
	actor   *act.Actor
	store   *store.Store
	cfg     Config
	roots   func() *x509.CertPool
	metrics *metrics.Registry
	log     *slog.Logger
	now     func() time.Time
	round   struct {
		number int64
		at     time.Time
	}
}

func New(kube kubernetes.Interface, dyn dynamic.Interface, disc discovery.Config, actor *act.Actor, st *store.Store, cfg Config, roots func() *x509.CertPool, m *metrics.Registry, log *slog.Logger) *Checker {
	return &Checker{kube: kube, dyn: dyn, disc: disc, actor: actor, store: st, cfg: cfg, roots: roots, metrics: m, log: log, now: time.Now}
}

// client verifies the instance's certificate against the database CAs under the primary Service's
// name, which CNPG puts in every server certificate.
func (c *Checker) client(serverName string) *http.Client {
	tr := &http.Transport{DisableKeepAlives: true}
	if c.cfg.Scheme == "https" {
		tr.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: c.roots(), ServerName: serverName}
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
	pages := metrics.NewSet()
	defer func() { c.metrics.Replace("bedrock_checker_page", pages) }()
	now := c.now()
	d, err := discovery.Discover(ctx, c.dyn, c.kube, c.disc)
	if err != nil {
		c.log.Warn("discovery", "err", err)
		return false
	}
	broken, err := c.brokenPath(ctx, now)
	if err != nil {
		c.log.Warn("read the normal path", "err", err)
		broken = append(broken, "normal path unreadable: "+err.Error())
	}
	midFailover, err := c.midFailover(ctx)
	if err != nil {
		c.log.Warn("read claims", "err", err)
		return false
	}
	ballots, ballotsErr := c.store.ListBallots(ctx)
	round, roundErr := c.store.GetRound(ctx)
	nodes, nodesErr := c.observerNodes(ctx)

	halts := map[string]bool{}
	for _, f := range d.Families {
		p := f.Primary
		target := p.Key()
		if obj, err := c.actor.Get(ctx, target); err == nil && c.actor.Fenced(obj) {
			continue
		}
		why := append([]string{}, broken...)
		primaryNode, nodeErr := c.primaryNode(ctx, p)
		switch {
		case ballotsErr != nil || roundErr != nil || nodesErr != nil:
			why = append(why, "votes unreadable")
		case nodeErr != nil:
			why = append(why, "primary node unknown: "+nodeErr.Error())
		case !c.quorum(target, ballots, round, without(nodes, primaryNode)):
			why = append(why, "observers lost quorum")
		}
		active := len(why) > 0
		healthy, reason := c.check(ctx, p)
		l := map[string]string{"target": target}
		c.metrics.Set("bedrock_checker_active", l, b2f(active))
		c.metrics.Set("bedrock_checker_primary_healthy", l, b2f(healthy))
		switch {
		case !active:
		case healthy && !midFailover:
			halts[target] = true
			c.log.Warn("normal path down and the database is up: stopping everything", "target", target, "why", why)
		case !healthy:
			c.log.Error("normal path down and the database is down", "target", target, "why", why, "reason", reason)
			pages.Add(map[string]string{"target": target, "reason": "fallback: database down while " + strings.Join(why, ", ")})
		}
		c.metrics.Set("bedrock_checker_halt", l, b2f(halts[target]))
	}
	for _, f := range d.Families {
		if _, ok := halts[f.Primary.Key()]; !ok {
			halts[f.Primary.Key()] = false
		}
	}
	if err := c.store.PutHalts(ctx, halts); err != nil {
		c.log.Error("write halts", "err", err)
		return false
	}
	c.metrics.Set("bedrock_checker_last_round_timestamp_seconds", nil, float64(now.Unix()))
	return true
}

func (c *Checker) brokenPath(ctx context.Context, now time.Time) ([]string, error) {
	var why []string
	r, err := c.store.GetRound(ctx)
	if err != nil {
		return nil, err
	}
	if r != nil && r.Number != c.round.number {
		c.round.number, c.round.at = r.Number, now
	}
	if r == nil || now.Sub(c.round.at) > c.cfg.RoundStale {
		why = append(why, "verifier stalled")
	}
	lease, err := c.kube.CoordinationV1().Leases(c.cfg.Namespace).Get(ctx, c.cfg.LeaseName, metav1.GetOptions{})
	switch {
	case apierrors.IsNotFound(err):
		why = append(why, "no decider lease")
	case err != nil:
		return why, err
	case lease.Spec.HolderIdentity == nil || *lease.Spec.HolderIdentity == "" || lease.Spec.RenewTime == nil || lease.Spec.LeaseDurationSeconds == nil ||
		now.Sub(lease.Spec.RenewTime.Time) > time.Duration(*lease.Spec.LeaseDurationSeconds)*time.Second+c.cfg.RoundStale:
		why = append(why, "decider has no leader")
	}
	return why, nil
}

// midFailover is true once a fence has started: from there finishing is safer than stopping.
func (c *Checker) midFailover(ctx context.Context) (bool, error) {
	claims, err := c.store.ListClaims(ctx)
	if err != nil {
		return false, err
	}
	for _, cl := range claims {
		if cl.State == store.ClaimOpen && cl.Step != store.StepClaimed {
			return true, nil
		}
	}
	return false, nil
}

// quorum is true when a majority of eligible nodes have all their observers voting on this round.
func (c *Checker) quorum(target string, ballots []store.Ballots, round *store.Round, eligible []string) bool {
	if round == nil || len(eligible) == 0 {
		return false
	}
	count := map[string]int{}
	for _, b := range ballots {
		if v, ok := b.Votes[target]; ok && v.Challenge == round.Challenge && v.Ballot != observe.BallotAbstain {
			count[b.Node]++
		}
	}
	voting := 0
	for _, n := range eligible {
		if count[n] >= c.cfg.PerNode {
			voting++
		}
	}
	return voting >= len(eligible)/2+1
}

func (c *Checker) observerNodes(ctx context.Context) ([]string, error) {
	pods, err := c.kube.CoreV1().Pods(c.cfg.Namespace).List(ctx, metav1.ListOptions{LabelSelector: c.cfg.ObserverSelector})
	if err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for _, p := range pods.Items {
		if p.Spec.NodeName == "" || p.DeletionTimestamp != nil || p.Status.Phase != corev1.PodRunning {
			continue
		}
		set[p.Spec.NodeName] = true
	}
	out := make([]string, 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	sort.Strings(out)
	return out, nil
}

func (c *Checker) primaryNode(ctx context.Context, m discovery.Member) (string, error) {
	if m.CurrentPrimary == "" {
		return "", nil
	}
	p, err := c.kube.CoreV1().Pods(m.Namespace).Get(ctx, m.CurrentPrimary, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return p.Spec.NodeName, nil
}

func without(xs []string, x string) []string {
	var out []string
	for _, v := range xs {
		if v != x {
			out = append(out, v)
		}
	}
	return out
}

func b2f(b bool) float64 {
	if b {
		return 1
	}
	return 0
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
