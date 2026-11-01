package observer

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/sassendes/bedrock/failover-controller/act"
	"github.com/sassendes/bedrock/failover-controller/discovery"
	"github.com/sassendes/bedrock/failover-controller/metrics"
	"github.com/sassendes/bedrock/failover-controller/observe"
	"github.com/sassendes/bedrock/failover-controller/store"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

type Config struct {
	Node            string
	Namespace       string
	Selector        string
	ExchangePort    int
	Check           observe.CheckConfig
	ProbeInterval   time.Duration
	SuspectInterval time.Duration
	AskTimeout      time.Duration
}

type Addresses interface{ SetAddrs(map[string]string) }

type Observer struct {
	kube    kubernetes.Interface
	dyn     dynamic.Interface
	disc    discovery.Config
	actor   *act.Actor
	store   *store.Store
	cfg     Config
	exch    *observe.Node
	addrs   Addresses
	pub     string
	metrics *metrics.Registry
	log     *slog.Logger
	check   func(ctx context.Context, cfg observe.CheckConfig, observer, target, nonce string) observe.Proof
	nonce   func() (string, error)

	published bool

	mu      sync.Mutex
	targets map[string]string
	systems map[string]string
	seq     uint64
}

func New(kube kubernetes.Interface, dyn dynamic.Interface, disc discovery.Config, actor *act.Actor, st *store.Store, cfg Config, key ed25519.PrivateKey, tr observe.Transport, addrs Addresses, m *metrics.Registry, log *slog.Logger) *Observer {
	o := &Observer{
		kube: kube, dyn: dyn, disc: disc, actor: actor, store: st, cfg: cfg, addrs: addrs, metrics: m, log: log,
		pub:     base64.StdEncoding.EncodeToString(key.Public().(ed25519.PublicKey)),
		check:   observe.StrongCheckWithNonce,
		nonce:   observe.NewNonce,
		targets: map[string]string{},
		systems: map[string]string{},
		seq:     uint64(time.Now().UnixNano()),
	}
	o.exch = observe.NewNode(cfg.Node, key, observe.Keyring{}, nil, tr, observe.ProberFunc(o.probe), 2)
	return o
}

func (o *Observer) Exchange() *observe.Node { return o.exch }

// probe answers a peer: the same strong check, with the peer's nonce, against the pod I believe is primary.
func (o *Observer) probe(ctx context.Context, target, nonce string) observe.Proof {
	o.mu.Lock()
	ip := o.targets[target]
	o.mu.Unlock()
	if ip == "" {
		return observe.Proof{Observer: o.cfg.Node, Target: target, Result: observe.ResultDown, Nonce: nonce, Reason: "unknown target"}
	}
	c := o.cfg.Check
	c.Host = ip
	return o.check(ctx, c, o.cfg.Node, target, nonce)
}

// Run returns only when ctx ends. One round every probe interval, faster while any target looks down.
func (o *Observer) Run(ctx context.Context, done func()) {
	for {
		wait := o.cfg.ProbeInterval
		if down := o.Round(ctx); down {
			wait = o.cfg.SuspectInterval
		}
		if o.published {
			done()
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

func (o *Observer) Round(ctx context.Context) bool {
	if err := o.refreshMembers(ctx); err != nil {
		o.log.Warn("refresh peers", "err", err)
	}
	disc, err := discovery.Discover(ctx, o.dyn, o.kube, o.disc)
	if err != nil {
		o.log.Warn("discovery", "err", err)
		o.metrics.Set("bedrock_observer_up", nil, 0)
		return false
	}
	o.metrics.Set("bedrock_observer_up", nil, 1)

	targets := map[string]string{}
	var keys []string
	for _, f := range disc.Families {
		key := f.Primary.Key()
		obj, err := o.actor.Get(ctx, key)
		if err != nil || o.actor.Fenced(obj) {
			continue
		}
		keys = append(keys, key)
		targets[key] = o.podIP(ctx, f.Primary)
	}
	o.mu.Lock()
	o.targets = targets
	o.mu.Unlock()

	challenge := ""
	if r, err := o.store.GetRound(ctx); err == nil && r != nil {
		challenge = r.Challenge
	} else if err != nil {
		o.log.Warn("round unreadable, signing a private nonce", "err", err)
	}
	votes := map[string]observe.Vote{}
	anyDown := false
	for _, key := range keys {
		v := o.vote(ctx, key, challenge)
		v.Challenge = challenge
		o.mu.Lock()
		o.seq++
		v.Seq = o.seq
		o.mu.Unlock()
		votes[key] = v
		if v.Ballot != observe.BallotUp {
			anyDown = true
		}
		for _, b := range []observe.Ballot{observe.BallotUp, observe.BallotDown, observe.BallotAbstain} {
			val := 0.0
			if v.Ballot == b {
				val = 1
			}
			o.metrics.Set("bedrock_observer_ballot", map[string]string{"target": key, "ballot": string(b)}, val)
		}
		o.metrics.Set("bedrock_observer_check_latency_seconds", map[string]string{"target": key}, v.Proof.Latency.Seconds())
	}
	err = o.store.PublishVotes(ctx, o.cfg.Node, o.pub, votes)
	o.published = err == nil
	if err != nil {
		o.log.Error("publish votes", "err", err)
		o.metrics.Set("bedrock_observer_publish_ok", nil, 0)
	} else {
		o.metrics.Set("bedrock_observer_publish_ok", nil, 1)
	}
	return anyDown
}

// vote signs the verifier's challenge when there is one, so postgres's signature counts for this round.
func (o *Observer) vote(ctx context.Context, key, challenge string) observe.Vote {
	nonce := challenge
	if nonce == "" {
		n, err := o.nonce()
		if err != nil {
			return observe.Vote{Target: key, Ballot: observe.BallotAbstain, Reason: "nonce: " + err.Error()}
		}
		nonce = n
	}
	own := o.probe(ctx, key, nonce)
	o.mu.Lock()
	expected := o.systems[key]
	o.mu.Unlock()
	if own.Result == observe.ResultUp {
		v := observe.Cast(key, expected, own, nil)
		if v.Ballot == observe.BallotUp {
			o.mu.Lock()
			o.systems[key] = own.SystemID
			o.mu.Unlock()
		}
		return v
	}
	answers, err := o.exch.Ask(ctx, key, o.cfg.AskTimeout)
	if err != nil {
		return observe.Vote{Target: key, Ballot: observe.BallotAbstain, Reason: "exchange: " + err.Error(), Proof: own}
	}
	j := observe.Judge(expected, answers)
	v := observe.Cast(key, expected, own, &j)
	o.log.Info("own check failed", "target", key, "reason", own.Reason, "verdict", j.Verdict, "ups", j.Ups, "downs", j.Downs, "ballot", v.Ballot)
	return v
}

func (o *Observer) podIP(ctx context.Context, m discovery.Member) string {
	if m.CurrentPrimary == "" {
		return ""
	}
	pod, err := o.kube.CoreV1().Pods(m.Namespace).Get(ctx, m.CurrentPrimary, metav1.GetOptions{})
	if err != nil || pod.DeletionTimestamp != nil || pod.Status.Phase != corev1.PodRunning {
		return ""
	}
	return pod.Status.PodIP
}

// refreshMembers builds the peer list from the running observer pods and their keys from the vote
// objects, which only the observer on that node can write.
func (o *Observer) refreshMembers(ctx context.Context) error {
	pods, err := o.kube.CoreV1().Pods(o.cfg.Namespace).List(ctx, metav1.ListOptions{LabelSelector: o.cfg.Selector})
	if err != nil {
		return err
	}
	ballots, err := o.store.ListBallots(ctx)
	if err != nil {
		return err
	}
	pubs := map[string]ed25519.PublicKey{}
	for _, b := range ballots {
		raw, err := base64.StdEncoding.DecodeString(b.PubKey)
		if err == nil && len(raw) == ed25519.PublicKeySize {
			pubs[b.Node] = ed25519.PublicKey(raw)
		}
	}
	addrs := map[string]string{}
	keys := observe.Keyring{}
	var peers []string
	for _, p := range pods.Items {
		n := p.Spec.NodeName
		if n == "" || p.Status.PodIP == "" || p.DeletionTimestamp != nil || p.Status.Phase != corev1.PodRunning {
			continue
		}
		pub, ok := pubs[n]
		if !ok {
			continue
		}
		keys[n] = pub
		if n == o.cfg.Node {
			continue
		}
		addrs[n] = net.JoinHostPort(p.Status.PodIP, strconv.Itoa(o.cfg.ExchangePort))
		peers = append(peers, n)
	}
	o.addrs.SetAddrs(addrs)
	o.exch.SetMembers(peers, keys)
	o.metrics.Set("bedrock_observer_peers", nil, float64(len(peers)))
	return nil
}
