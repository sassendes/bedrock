package decider

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/sassendes/bedrock/failover-controller/act"
	"github.com/sassendes/bedrock/failover-controller/decide"
	"github.com/sassendes/bedrock/failover-controller/discovery"
	"github.com/sassendes/bedrock/failover-controller/metrics"
	"github.com/sassendes/bedrock/failover-controller/observe"
	"github.com/sassendes/bedrock/failover-controller/store"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

type Config struct {
	Identity           string
	Params             decide.Params
	ObserverSelector   string
	ObserverNamespace  string
	PauseAnnotation    string
	TransitionalPhases []string
	FenceVerifyTimeout time.Duration
	PromoteTimeout     time.Duration
	ClaimTimeout       time.Duration
	ClockOffset        time.Duration
}

func DefaultConfig() Config {
	return Config{
		Params:             decide.DefaultParams(),
		ObserverSelector:   "app.kubernetes.io/name=bedrock-observer",
		ObserverNamespace:  "bedrock-failover",
		PauseAnnotation:    "bedrock.io/failover-pause",
		TransitionalPhases: []string{"Failing over", "Switchover in progress", "Upgrading cluster", "Primary instance is being restarted in-place", "Primary instance is being restarted without a switchover", "Promoting to primary cluster"},
		FenceVerifyTimeout: time.Minute,
		PromoteTimeout:     5 * time.Minute,
		ClaimTimeout:       5 * time.Minute,
		ClockOffset:        5 * time.Second,
	}
}

type seen struct {
	seq uint64
	at  time.Time
}

type Decider struct {
	dyn      dynamic.Interface
	kube     kubernetes.Interface
	disc     discovery.Config
	store    *store.Store
	actor    *act.Actor
	cfg      Config
	tracker  *decide.Tracker
	leader   func() bool
	now      func() time.Time
	metrics  *metrics.Registry
	log      *slog.Logger
	freshSeq map[string]seen
}

func New(dyn dynamic.Interface, kube kubernetes.Interface, disc discovery.Config, st *store.Store, actor *act.Actor, cfg Config, leader func() bool, m *metrics.Registry, log *slog.Logger) (*Decider, error) {
	if err := cfg.Params.Check(); err != nil {
		return nil, err
	}
	if cfg.Identity == "" {
		return nil, errors.New("decider identity is empty")
	}
	return &Decider{
		dyn: dyn, kube: kube, disc: disc, store: st, actor: actor, cfg: cfg,
		tracker: decide.NewTracker(cfg.Params), leader: leader, now: time.Now, metrics: m, log: log,
		freshSeq: map[string]seen{},
	}, nil
}

// Tick reads everything again from the API: nothing the decider remembers between ticks is trusted
// for safety except vote freshness, which is measured on its own clock.
func (d *Decider) Tick(ctx context.Context) error {
	pages := metrics.NewSet()
	defer func() { d.metrics.Replace("bedrock_failover_page", pages) }()
	if !d.leader() {
		d.metrics.Set("bedrock_failover_leader", nil, 0)
		return nil
	}
	d.metrics.Set("bedrock_failover_leader", nil, 1)
	now := d.now()

	round, err := d.store.GetRound(ctx)
	if err != nil {
		pages.Add(map[string]string{"target": "", "reason": "round unreadable"})
		return err
	}
	approvals, err := d.store.GetApprovals(ctx)
	if err != nil {
		pages.Add(map[string]string{"target": "", "reason": "approvals unreadable"})
		return err
	}
	ballots, err := d.store.ListBallots(ctx)
	if err != nil {
		pages.Add(map[string]string{"target": "", "reason": "votes unreadable"})
		return err
	}
	claims, err := d.store.ListClaims(ctx)
	if err != nil {
		pages.Add(map[string]string{"target": "", "reason": "decision store unreadable"})
		return err
	}
	byTarget := map[string]*store.Claim{}
	inWindow := 0
	for _, c := range claims {
		byTarget[c.Target] = c
		if ran(c) && now.Sub(c.StartedAt) < d.cfg.Params.CapWindow {
			inWindow++
		}
		d.metrics.Set("bedrock_failover_epoch", map[string]string{"target": c.Target}, float64(c.Epoch))
	}
	open := metrics.NewSet()
	for _, c := range claims {
		if c.State == store.ClaimOpen {
			open.Add(map[string]string{"target": c.Target, "step": string(c.Step)})
		}
	}
	d.metrics.Replace("bedrock_failover_claim_open", open)
	for _, c := range claims {
		switch c.State {
		case store.ClaimOpen:
			if err := d.advance(ctx, c, ballots, round, approvals[store.ApprovalKey(c.Target)], pages); err != nil {
				d.log.Error("advance claim", "target", c.Target, "step", c.Step, "err", err)
				pages.Add(map[string]string{"target": c.Target, "reason": "step " + string(c.Step) + " failed: " + err.Error()})
			}
		case store.ClaimHalted:
			pages.Add(map[string]string{"target": c.Target, "reason": "halted at " + string(c.Step) + ": " + c.Reason})
		}
	}

	disc, err := discovery.Discover(ctx, d.dyn, d.kube, d.disc)
	if err != nil {
		pages.Add(map[string]string{"target": "", "reason": "discovery failed"})
		return err
	}
	for _, p := range disc.Problems {
		pages.Add(map[string]string{"target": p.Cluster, "reason": p.Reason})
	}

	observerNodes, err := d.observerNodes(ctx)
	if err != nil {
		return err
	}

	var inputs []decide.Input
	families := map[string]discovery.Family{}
	for _, f := range disc.Families {
		key := f.Primary.Key()
		obj, err := d.actor.Get(ctx, key)
		if err != nil {
			return err
		}
		if d.actor.Fenced(obj) {
			continue
		}
		if c := byTarget[key]; c != nil && c.State != store.ClaimClosed {
			continue
		}
		primaryNode, err := d.primaryNode(ctx, f.Primary)
		if err != nil {
			return err
		}
		in := decide.Input{Target: key, Eligible: eligible(observerNodes, primaryNode)}
		for _, b := range ballots {
			v, ok := b.Votes[key]
			if !ok {
				continue
			}
			in.Observations = append(in.Observations, decide.Observation{Node: b.Node, Ballot: proven(round, key, b.Node, v.Ballot), Fresh: d.fresh(b.Node, key, v.Seq, now)})
		}
		in.Paused = obj.GetAnnotations()[d.cfg.PauseAnnotation] == "true"
		in.Transitioning = contains(d.cfg.TransitionalPhases, f.Primary.Phase)
		durability, _, _ := unstructured.NestedString(obj.Object, "spec", "postgresql", "synchronous", "dataDurability")
		in.DurabilityRequired = durability == "required"
		if c := byTarget[key]; c != nil && ran(c) {
			in.LastFailover = c.StartedAt
		}
		inputs = append(inputs, in)
		families[key] = f
	}

	for _, v := range d.tracker.Step(now, inputs, inWindow) {
		labels := map[string]string{"target": v.Target}
		d.metrics.Set("bedrock_failover_votes", with(labels, "ballot", "up"), float64(v.Ups))
		d.metrics.Set("bedrock_failover_votes", with(labels, "ballot", "down"), float64(v.Downs))
		d.metrics.Set("bedrock_failover_votes", with(labels, "ballot", "abstain"), float64(v.Abstains))
		d.metrics.Set("bedrock_failover_quorum", labels, float64(v.Q))
		for _, s := range []decide.State{decide.Alive, decide.Suspect, decide.Dead} {
			val := 0.0
			if v.State == s {
				val = 1
			}
			d.metrics.Set("bedrock_failover_state", with(labels, "state", string(s)), val)
		}
		for _, p := range v.Pages {
			pages.Add(map[string]string{"target": v.Target, "reason": p})
		}
		if v.State != decide.Dead {
			continue
		}
		d.log.Warn("target declared dead", "target", v.Target, "down", v.Downs, "quorum", v.Q)
		if err := d.claim(ctx, families[v.Target], byTarget[v.Target], now); err != nil {
			pages.Add(map[string]string{"target": v.Target, "reason": err.Error()})
		}
		d.tracker.Clear(v.Target)
	}
	return nil
}

// fresh measures a vote's age on the decider's own clock, from the moment its sequence changed.
// A vote seen for the first time has an unknown age and does not count until it changes.
func (d *Decider) fresh(node, target string, seq uint64, now time.Time) bool {
	k := node + "|" + target
	s, ok := d.freshSeq[k]
	switch {
	case !ok:
		d.freshSeq[k] = seen{seq: seq}
		return false
	case s.seq != seq:
		d.freshSeq[k] = seen{seq: seq, at: now}
		return true
	}
	return !s.at.IsZero() && now.Sub(s.at) < d.cfg.Params.Freshness
}

func (d *Decider) observerNodes(ctx context.Context) ([]string, error) {
	pods, err := d.kube.CoreV1().Pods(d.cfg.ObserverNamespace).List(ctx, metav1.ListOptions{LabelSelector: d.cfg.ObserverSelector})
	if err != nil {
		return nil, err
	}
	nodes, err := d.kube.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	zone := map[string]string{}
	for _, n := range nodes.Items {
		zone[n.Name] = n.Labels[d.disc.ZoneLabel]
	}
	set := map[string]bool{}
	for _, p := range pods.Items {
		if p.Spec.NodeName == "" || p.DeletionTimestamp != nil || p.Status.Phase != corev1.PodRunning || d.disc.DRZones[zone[p.Spec.NodeName]] {
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

func (d *Decider) primaryNode(ctx context.Context, m discovery.Member) (string, error) {
	if m.CurrentPrimary == "" {
		return "", nil
	}
	pod, err := d.kube.CoreV1().Pods(m.Namespace).Get(ctx, m.CurrentPrimary, metav1.GetOptions{})
	if err != nil {
		return "", nil
	}
	return pod.Spec.NodeName, nil
}

func ran(c *store.Claim) bool { return c.Step != store.StepClaimed }

// proven keeps an up only if the verifier checked its signature last round. A liar's plain up is an abstention.
func proven(round *store.Round, target, node string, b observe.Ballot) observe.Ballot {
	if b != observe.BallotUp {
		return b
	}
	if round != nil && contains(round.ValidUps[target], node) {
		return b
	}
	return observe.BallotAbstain
}

func (d *Decider) freshUp(target string, ballots []store.Ballots, round *store.Round, now time.Time) string {
	for _, b := range ballots {
		if v, ok := b.Votes[target]; ok && proven(round, target, b.Node, v.Ballot) == observe.BallotUp && d.fresh(b.Node, target, v.Seq, now) {
			return b.Node
		}
	}
	return ""
}

func eligible(nodes []string, exclude string) []string {
	var out []string
	for _, n := range nodes {
		if n != exclude {
			out = append(out, n)
		}
	}
	return out
}

func candidate(f discovery.Family) (discovery.Member, bool) {
	for _, m := range f.Live {
		if m.ReadyInstances > 0 {
			return m, true
		}
	}
	return discovery.Member{}, false
}

// claim opens the run. Delayed and DR members are never promoted automatically.
func (d *Decider) claim(ctx context.Context, f discovery.Family, prev *store.Claim, now time.Time) error {
	target := f.Primary.Key()
	cand, ok := candidate(f)
	if !ok {
		return errors.New("dead but no ready live replica to promote, not touching anything")
	}
	pods, nodes, err := d.actor.Instances(ctx, target)
	if err != nil {
		return err
	}
	if f.Primary.CurrentPrimary == "" || !contains(pods, f.Primary.CurrentPrimary) {
		return fmt.Errorf("the old primary's pod %q is not in the API, it may still run on a node nobody can see", f.Primary.CurrentPrimary)
	}
	var followers []string
	for _, group := range [][]discovery.Member{f.Live, f.Delayed, f.DR} {
		for _, m := range group {
			if m.Key() != cand.Key() {
				followers = append(followers, m.Key())
			}
		}
	}
	epoch := int64(1)
	if prev != nil {
		epoch = prev.Epoch + 1
	}
	c := &store.Claim{
		Target: target, Epoch: epoch, State: store.ClaimOpen, Step: store.StepClaimed, Decider: d.cfg.Identity,
		Candidate: cand.Key(), Followers: followers, Nodes: nodes, StartedAt: now, StepAt: now,
		Times: map[string]string{},
	}
	if !d.leader() {
		return errors.New("lost the lease before claiming")
	}
	if err := d.store.Open(ctx, c, prev); err != nil {
		return fmt.Errorf("claim: %w", err)
	}
	d.log.Warn("claimed failover", "target", target, "epoch", epoch, "candidate", c.Candidate, "nodes", nodes)
	return nil
}

func (d *Decider) move(ctx context.Context, c *store.Claim, to store.Step) error {
	now := d.now()
	c.Times[string(c.Step)+"→"+string(to)] = now.Sub(c.StepAt).Round(time.Millisecond).String()
	c.Step, c.StepAt = to, now
	if to == store.StepFlipped {
		c.State = store.ClaimClosed
		c.Times["total"] = now.Sub(c.StartedAt).Round(time.Millisecond).String()
	}
	return d.store.Save(ctx, c)
}

func (d *Decider) halt(ctx context.Context, c *store.Claim, reason string) error {
	c.State, c.Reason = store.ClaimHalted, reason
	d.log.Error("failover halted", "target", c.Target, "step", c.Step, "reason", reason)
	return d.store.Save(ctx, c)
}

// advance moves an open claim one step. Any decider holding the lease resumes the claim the same way,
// since every step is fixed in the claim and safe to repeat.
func (d *Decider) advance(ctx context.Context, c *store.Claim, ballots []store.Ballots, round *store.Round, approval store.Approval, pages *metrics.Set) error {
	if !d.leader() {
		return nil
	}
	now := d.now()
	if c.Step == store.StepClaimed || c.Step == store.StepFencing {
		if now.Sub(c.StartedAt) > d.cfg.ClaimTimeout {
			return d.halt(ctx, c, "claim timed out before the fence was verified")
		}
	}
	switch c.Step {
	case store.StepClaimed:
		if approval.Epoch == c.Epoch && approval.State == store.ApprovalRevoked {
			c.State, c.Reason = store.ClaimClosed, "the verifier revoked the approval: the database proved it is alive"
			d.log.Warn("claim revoked", "target", c.Target)
			return d.store.Save(ctx, c)
		}
		if up := d.freshUp(c.Target, ballots, round, d.now()); up != "" {
			c.State, c.Reason = store.ClaimClosed, "vetoed before the fence: fresh up from "+up
			d.log.Warn("claim vetoed", "target", c.Target, "by", up)
			return d.store.Save(ctx, c)
		}
		if approval.Epoch != c.Epoch || approval.State != store.ApprovalValid {
			pages.Add(map[string]string{"target": c.Target, "reason": "waiting for the verifier's approval"})
			return nil
		}
		if err := d.actor.Fence(ctx, c.Target, c.Epoch); err != nil {
			return err
		}
		return d.move(ctx, c, store.StepFencing)

	case store.StepFencing:
		ok, reason, err := d.actor.VerifyFence(ctx, c.Target, c.Nodes, c.StartedAt.Add(-d.cfg.ClockOffset))
		if err != nil {
			return err
		}
		if !ok {
			if now.Sub(c.StepAt) > d.cfg.FenceVerifyTimeout {
				return d.halt(ctx, c, "fence unverified: "+reason)
			}
			pages.Add(map[string]string{"target": c.Target, "reason": "waiting for fence: " + reason})
			return nil
		}
		return d.move(ctx, c, store.StepFenced)

	case store.StepFenced:
		ok, reason, err := d.actor.VerifyFence(ctx, c.Target, c.Nodes, c.StartedAt.Add(-d.cfg.ClockOffset))
		if err != nil {
			return err
		}
		if !ok {
			return d.halt(ctx, c, "fence no longer holds before promote: "+reason)
		}
		if err := d.actor.Promote(ctx, c.Candidate, c.Epoch); err != nil {
			return err
		}
		return d.move(ctx, c, store.StepPromoting)

	case store.StepPromoting:
		ok, reason, err := d.actor.Promoted(ctx, c.Candidate)
		if err != nil {
			return err
		}
		if !ok {
			if now.Sub(c.StepAt) <= d.cfg.PromoteTimeout {
				return nil
			}
			c.Notes = append(c.Notes, "promotion not healthy after "+d.cfg.PromoteTimeout.String()+": "+reason+"; flipping anyway, the old primary is fenced")
			pages.Add(map[string]string{"target": c.Target, "reason": "promotion of " + c.Candidate + " not healthy: " + reason})
		}
		return d.move(ctx, c, store.StepPromoted)

	case store.StepPromoted:
		for _, f := range c.Followers {
			ok, msg, err := d.actor.Repoint(ctx, f, c.Candidate, c.Epoch)
			if err != nil {
				return err
			}
			if !ok {
				pages.Add(map[string]string{"target": f, "reason": msg})
			}
			c.Notes = append(c.Notes, msg)
		}
		return d.move(ctx, c, store.StepRepointed)

	case store.StepRepointed:
		flipped, err := d.actor.Flip(ctx, c.Target, c.Candidate, c.Epoch)
		if err != nil {
			return err
		}
		c.Notes = append(c.Notes, "flipped "+strings.Join(flipped, ", "))
		d.log.Warn("failover done", "target", c.Target, "new", c.Candidate, "epoch", c.Epoch)
		return d.move(ctx, c, store.StepFlipped)
	}
	return d.halt(ctx, c, "unknown step "+string(c.Step))
}

func with(l map[string]string, k, v string) map[string]string {
	out := map[string]string{k: v}
	for a, b := range l {
		out[a] = b
	}
	return out
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
