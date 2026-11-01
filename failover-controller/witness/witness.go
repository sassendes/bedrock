package witness

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/sassendes/bedrock/failover-controller/act"
	"github.com/sassendes/bedrock/failover-controller/discovery"
	"github.com/sassendes/bedrock/failover-controller/judge"
	"github.com/sassendes/bedrock/failover-controller/observe"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes"
)

type Config struct {
	Check         observe.CheckConfig
	MaxLag        time.Duration
	ManagerScheme string
	ManagerPort   int
	ManagerPath   string
	Container     string
	Roots         *x509.CertPool
}

func DefaultConfig() Config {
	return Config{MaxLag: 10 * time.Second, ManagerScheme: "https", ManagerPort: 8000, ManagerPath: "/readyz", Container: "postgres"}
}

type Gatherer struct {
	kube     kubernetes.Interface
	actor    *act.Actor
	cfg      Config
	receiver func(ctx context.Context, cfg observe.CheckConfig) observe.ReceiverState
}

func New(kube kubernetes.Interface, actor *act.Actor, cfg Config) *Gatherer {
	return &Gatherer{kube: kube, actor: actor, cfg: cfg, receiver: observe.Receiver}
}

// Gather asks the four witnesses about the family's primary. Each one must answer to count:
// an unreachable witness is silent, never dead.
func (g *Gatherer) Gather(ctx context.Context, f discovery.Family) map[judge.Witness]judge.Answer {
	return map[judge.Witness]judge.Answer{
		judge.Replicas: g.replicas(ctx, f),
		judge.Manager:  g.manager(ctx, f.Primary),
		judge.Kubelet:  g.kubelet(ctx, f.Primary),
		judge.API:      g.api(ctx, f.Primary),
	}
}

// replicas are the instances streaming straight from the primary: its own standbys and the designated
// primary of every replica cluster. Dead only when every one of them answers that its stream is gone.
func (g *Gatherer) replicas(ctx context.Context, f discovery.Family) judge.Answer {
	var ips []string
	pods, err := g.kube.CoreV1().Pods(f.Primary.Namespace).List(ctx, metav1.ListOptions{LabelSelector: g.actor.Config().Discovery.InstanceSelector + "," + g.actor.Config().Discovery.ClusterLabel + "=" + f.Primary.Name})
	if err != nil {
		return judge.Silent
	}
	for _, p := range pods.Items {
		if p.Name != f.Primary.CurrentPrimary && p.Status.PodIP != "" && p.DeletionTimestamp == nil {
			ips = append(ips, p.Status.PodIP)
		}
	}
	for _, group := range [][]discovery.Member{f.Live, f.Delayed, f.DR} {
		for _, m := range group {
			if ip := podIP(ctx, g.kube, m.Namespace, m.CurrentPrimary); ip != "" {
				ips = append(ips, ip)
			}
		}
	}
	if len(ips) == 0 {
		return judge.Silent
	}
	dead, alive := 0, 0
	for _, ip := range ips {
		c := g.cfg.Check
		c.Host = ip
		r := g.receiver(ctx, c)
		switch {
		case r.Err != "":
		case r.Status == "streaming" && r.AgeMs >= 0 && time.Duration(r.AgeMs)*time.Millisecond <= g.cfg.MaxLag:
			alive++
		default:
			dead++
		}
	}
	switch {
	case alive > 0:
		return judge.Alive
	case dead == len(ips):
		return judge.Dead
	}
	return judge.Silent
}

func podIP(ctx context.Context, kube kubernetes.Interface, ns, name string) string {
	if name == "" {
		return ""
	}
	p, err := kube.CoreV1().Pods(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil || p.DeletionTimestamp != nil {
		return ""
	}
	return p.Status.PodIP
}

func (g *Gatherer) manager(ctx context.Context, m discovery.Member) judge.Answer {
	ip := podIP(ctx, g.kube, m.Namespace, m.CurrentPrimary)
	if ip == "" {
		return judge.Silent
	}
	tr := &http.Transport{DisableKeepAlives: true}
	if g.cfg.ManagerScheme == "https" {
		name, err := g.actor.PrimaryService(ctx, m.Key())
		if err != nil {
			return judge.Silent
		}
		tr.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: g.cfg.Roots, ServerName: name}
	}
	client := &http.Client{Timeout: g.cfg.Check.Timeout, Transport: tr}
	url := fmt.Sprintf("%s://%s%s", g.cfg.ManagerScheme, net.JoinHostPort(ip, strconv.Itoa(g.cfg.ManagerPort)), g.cfg.ManagerPath)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return judge.Silent
	}
	resp, err := client.Do(req)
	if err != nil {
		return judge.Silent
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode == http.StatusOK {
		return judge.Alive
	}
	return judge.Dead
}

// kubelet reads the container state the kubelet reported. It is only trusted while the node is Ready:
// a node that stopped reporting leaves stale state behind. The kubelet proxy is not used on purpose:
// nodes/proxy get is enough to run commands in pods.
func (g *Gatherer) kubelet(ctx context.Context, m discovery.Member) judge.Answer {
	if m.CurrentPrimary == "" {
		return judge.Silent
	}
	p, err := g.kube.CoreV1().Pods(m.Namespace).Get(ctx, m.CurrentPrimary, metav1.GetOptions{})
	if err != nil {
		return judge.Silent
	}
	node, err := g.kube.CoreV1().Nodes().Get(ctx, p.Spec.NodeName, metav1.GetOptions{})
	if err != nil || !nodeReady(node) {
		return judge.Silent
	}
	for _, c := range p.Status.ContainerStatuses {
		if c.Name != g.cfg.Container {
			continue
		}
		switch {
		case c.State.Terminated != nil, c.State.Waiting != nil, !c.Ready:
			return judge.Dead
		case c.State.Running != nil:
			return judge.Alive
		}
	}
	return judge.Silent
}

func nodeReady(n *corev1.Node) bool {
	for _, c := range n.Status.Conditions {
		if c.Type == corev1.NodeReady {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
}

// api is what the operator last recorded: an instance whose status call failed is written as not primary.
func (g *Gatherer) api(ctx context.Context, m discovery.Member) judge.Answer {
	obj, err := g.actor.Get(ctx, m.Key())
	if apierrors.IsNotFound(err) || err != nil {
		return judge.Silent
	}
	isPrimary, found, _ := unstructured.NestedBool(obj.Object, "status", "instancesReportedState", m.CurrentPrimary, "isPrimary")
	switch {
	case !found:
		return judge.Silent
	case isPrimary:
		return judge.Alive
	}
	return judge.Dead
}
