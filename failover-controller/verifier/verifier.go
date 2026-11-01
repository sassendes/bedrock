package verifier

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"time"

	"github.com/sassendes/bedrock/failover-controller/act"
	"github.com/sassendes/bedrock/failover-controller/discovery"
	"github.com/sassendes/bedrock/failover-controller/judge"
	"github.com/sassendes/bedrock/failover-controller/metrics"
	"github.com/sassendes/bedrock/failover-controller/observe"
	"github.com/sassendes/bedrock/failover-controller/proof"
	"github.com/sassendes/bedrock/failover-controller/store"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

type Config struct {
	DeadNeeded        int
	PerNode           int
	GraceRounds       int64
	ObserverSelector  string
	ObserverNamespace string
}

func DefaultConfig() Config {
	return Config{DeadNeeded: 3, PerNode: 2, GraceRounds: 15, ObserverSelector: "app.kubernetes.io/name=bedrock-observer", ObserverNamespace: "bedrock-failover"}
}

type Witnesses interface {
	Gather(ctx context.Context, f discovery.Family) map[judge.Witness]judge.Answer
}

// Verifier is the judge. It issues every round's challenge, closes the round on a timer, judges the
// snapshot, and writes the only thing admission accepts as permission. It touches no database.
type Verifier struct {
	dyn       dynamic.Interface
	kube      kubernetes.Interface
	disc      discovery.Config
	store     *store.Store
	actor     *act.Actor
	witnesses Witnesses
	keys      proof.Keys
	cfg       Config
	metrics   *metrics.Registry
	log       *slog.Logger
	now       func() time.Time
	nonce     func() (string, error)
	replay    func(ctx context.Context, m discovery.Member) (string, error)
}

func New(dyn dynamic.Interface, kube kubernetes.Interface, disc discovery.Config, st *store.Store, actor *act.Actor, w Witnesses, keys proof.Keys, cfg Config, m *metrics.Registry, log *slog.Logger) (*Verifier, error) {
	if cfg.DeadNeeded < 1 || cfg.DeadNeeded > len(judge.AllWitnesses) {
		return nil, errors.New("dead witnesses needed must be between 1 and 4")
	}
	if cfg.PerNode < 1 || cfg.GraceRounds < 0 {
		return nil, errors.New("observers per node must be at least 1 and the cnpg window not negative")
	}
	return &Verifier{dyn: dyn, kube: kube, disc: disc, store: st, actor: actor, witnesses: w, keys: keys, cfg: cfg, metrics: m, log: log, now: time.Now, nonce: observe.NewNonce, replay: func(context.Context, discovery.Member) (string, error) { return "", errors.New("no replay reader") }}, nil
}

// ReadReplayWith lets the verifier ask a candidate how far it replayed, with the observer login.
func (v *Verifier) ReadReplayWith(check observe.CheckConfig) {
	v.replay = func(ctx context.Context, m discovery.Member) (string, error) {
		ip, err := v.podIP(ctx, m.Namespace, m.CurrentPrimary)
		if err != nil {
			return "", err
		}
		c := check
		c.Host = ip
		return observe.ReplayLSN(ctx, c)
	}
}

// Tick closes the current round and opens the next. It runs on a timer whatever the evidence looks
// like, so a signature taken before a crash dies with its round.
func (v *Verifier) Tick(ctx context.Context) error {
	pages := metrics.NewSet()
	defer func() { v.metrics.Replace("bedrock_verifier_page", pages) }()

	prev, err := v.store.GetRound(ctx)
	if err != nil {
		return err
	}
	next := store.Round{ValidUps: map[string][]string{}, Signers: map[string][]string{}, Outcomes: map[string]string{}, Split: map[string]bool{}, ProvenLSN: map[string]string{}, DeadSince: map[string]store.DeadMark{}}
	if prev != nil {
		if err := v.judge(ctx, *prev, &next, pages); err != nil {
			return err
		}
		next.Number = prev.Number + 1
	} else {
		next.Number = 1
	}
	c, err := v.nonce()
	if err != nil {
		return err
	}
	next.Challenge, next.IssuedAt = c, v.now()
	if err := v.store.PutRound(ctx, next); err != nil {
		return err
	}
	v.metrics.Set("bedrock_verifier_round", nil, float64(next.Number))
	return nil
}

func (v *Verifier) judge(ctx context.Context, round store.Round, next *store.Round, pages *metrics.Set) error {
	disc, err := discovery.Discover(ctx, v.dyn, v.kube, v.disc)
	if err != nil {
		return err
	}
	ballots, err := v.store.ListBallots(ctx)
	if err != nil {
		return err
	}
	claims, err := v.store.ListClaims(ctx)
	if err != nil {
		return err
	}
	approvals, err := v.store.GetApprovals(ctx)
	if err != nil {
		return err
	}
	halts, err := v.store.GetHalts(ctx)
	if err != nil {
		return err
	}
	halted := store.Halted(halts)
	byTarget := map[string]*store.Claim{}
	for _, c := range claims {
		byTarget[c.Target] = c
	}
	nodes, err := v.observerNodes(ctx)
	if err != nil {
		return err
	}

	for _, f := range disc.Families {
		target := f.Primary.Key()
		obj, err := v.actor.Get(ctx, target)
		if err != nil {
			return err
		}
		if v.actor.Fenced(obj) {
			continue
		}
		in := judge.Input{
			Target:     target,
			Challenge:  round.Challenge,
			Eligible:   without(nodes, v.primaryNode(ctx, f.Primary)),
			Ballots:    map[string]observe.Vote{},
			NodeOf:     map[string]string{},
			PerNode:    v.cfg.PerNode,
			Witnesses:  v.witnesses.Gather(ctx, f),
			DeadNeeded: v.cfg.DeadNeeded,
		}
		for _, b := range ballots {
			if vote, ok := b.Votes[target]; ok && vote.Challenge == round.Challenge {
				in.Ballots[b.ID()] = vote
				in.NodeOf[b.ID()] = b.Node
			}
		}
		verdict := judge.Judge(v.keys, in)
		next.ProvenLSN[target] = round.ProvenLSN[target]
		for _, id := range verdict.ValidUps {
			next.ProvenLSN[target] = maxLSN(next.ProvenLSN[target], in.Ballots[id].Proof.Signed.LSN)
		}
		if verdict.Outcome == judge.ProvenDead {
			mark, ok := round.DeadSince[target]
			if !ok || mark.Instance != f.Primary.CurrentPrimary {
				mark = store.DeadMark{Instance: f.Primary.CurrentPrimary, Round: round.Number}
			}
			next.DeadSince[target] = mark
		}
		next.ValidUps[target] = verdict.ValidUps
		next.Signers[target] = verdict.PrimarySigners
		next.Outcomes[target] = string(verdict.Outcome)
		v.record(verdict, pages)

		if judge.SplitBrain(round.Signers[target], verdict.PrimarySigners) {
			next.Split[target] = true
			pages.Add(map[string]string{"target": target, "reason": "split brain: two instances signed as primary two rounds in a row"})
		}
		if verdict.Outcome == judge.Contradiction {
			pages.Add(map[string]string{"target": target, "reason": "contradiction: signed up while proven down, never acting"})
		}
		gate := gate{halted: halted, family: f, mark: next.DeadSince[target], proven: next.ProvenLSN[target], pages: pages}
		if err := v.decide(ctx, target, verdict, byTarget[target], approvals[store.ApprovalKey(target)], round.Number, gate); err != nil {
			pages.Add(map[string]string{"target": target, "reason": "approval write failed: " + err.Error()})
		}
	}

	for _, c := range claims {
		a := approvals[store.ApprovalKey(c.Target)]
		if c.State != store.ClaimOpen && a.State == store.ApprovalValid && a.Epoch == c.Epoch {
			if err := v.store.SetApproval(ctx, c.Target, store.Approval{State: store.ApprovalClosed, Epoch: c.Epoch, Round: round.Number}); err != nil {
				return err
			}
		}
	}
	return nil
}

type gate struct {
	halted []string
	family discovery.Family
	mark   store.DeadMark
	proven string
	pages  *metrics.Set
}

// decide approves a claimed run only when every gate holds: a proven death of the instance the claim
// names, no checker halt, cnpg given its window when the cluster has standbys, and a candidate that
// replayed every write postgres proved. It revokes before the fence when one stops holding.
func (v *Verifier) decide(ctx context.Context, target string, verdict judge.Verdict, c *store.Claim, a store.Approval, round int64, g gate) error {
	if c == nil || c.State != store.ClaimOpen || c.Step != store.StepClaimed {
		return nil
	}
	valid := a.Epoch == c.Epoch && a.State == store.ApprovalValid
	stop := ""
	switch {
	case len(g.halted) > 0:
		stop = "the checker holds everything"
	case verdict.Outcome == judge.Up || verdict.Outcome == judge.Contradiction:
		stop = "the database proved it is alive"
	case c.Instance == "" || c.Instance != g.family.Primary.CurrentPrimary:
		stop = "the primary is no longer " + c.Instance + ", cnpg moved on"
	}
	if stop != "" {
		if valid {
			v.log.Warn("revoked", "target", target, "epoch", c.Epoch, "round", round, "why", stop)
			return v.store.SetApproval(ctx, target, store.Approval{State: store.ApprovalRevoked, Epoch: c.Epoch, Round: round, Instance: c.Instance})
		}
		return nil
	}
	if verdict.Outcome != judge.ProvenDead || valid {
		return nil
	}
	if standbys := v.actor.ReadyStandbys(ctx, g.family.Primary); standbys > 0 && round-g.mark.Round < v.cfg.GraceRounds {
		return nil
	}
	if behind, why := v.behind(ctx, g.family, c.Candidate, g.proven); behind {
		g.pages.Add(map[string]string{"target": target, "reason": "holding: " + why})
		return nil
	}
	v.log.Warn("approved", "target", target, "epoch", c.Epoch, "round", round, "instance", c.Instance, "witnesses", verdict.DeadWitnesses, "downs", verdict.Downs)
	return v.store.SetApproval(ctx, target, store.Approval{State: store.ApprovalValid, Epoch: c.Epoch, Round: round, Instance: c.Instance})
}

func (v *Verifier) behind(ctx context.Context, f discovery.Family, candidate, proven string) (bool, string) {
	want, ok := observe.ParseLSN(proven)
	if proven == "" {
		return false, ""
	}
	if !ok {
		return true, "the last proven position " + proven + " is unreadable"
	}
	for _, m := range f.Live {
		if m.Key() != candidate {
			continue
		}
		got, err := v.replay(ctx, m)
		if err != nil {
			return true, "cannot read how far " + candidate + " replayed: " + err.Error()
		}
		if n, ok := observe.ParseLSN(got); !ok || n < want {
			return true, candidate + " replayed to " + got + ", the old primary proved " + proven
		}
		return false, ""
	}
	return true, "the candidate " + candidate + " is not a live replica"
}

func (v *Verifier) podIP(ctx context.Context, ns, name string) (string, error) {
	p, err := v.kube.CoreV1().Pods(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	if p.Status.PodIP == "" {
		return "", errors.New("pod " + name + " has no ip")
	}
	return p.Status.PodIP, nil
}

func maxLSN(a, b string) string {
	x, okA := observe.ParseLSN(a)
	y, okB := observe.ParseLSN(b)
	switch {
	case !okB:
		return a
	case !okA || y > x:
		return b
	}
	return a
}

func (v *Verifier) record(verdict judge.Verdict, pages *metrics.Set) {
	l := map[string]string{"target": verdict.Target}
	for _, o := range []judge.Outcome{judge.Up, judge.ProvenDead, judge.Contradiction, judge.Unknown} {
		val := 0.0
		if verdict.Outcome == o {
			val = 1
		}
		v.metrics.Set("bedrock_verifier_outcome", map[string]string{"target": verdict.Target, "outcome": string(o)}, val)
	}
	v.metrics.Set("bedrock_verifier_dead_witnesses", l, float64(len(verdict.DeadWitnesses)))
	v.metrics.Set("bedrock_verifier_valid_ups", l, float64(len(verdict.ValidUps)))
	nodes := make([]string, 0, len(verdict.RejectedProofs))
	for n := range verdict.RejectedProofs {
		nodes = append(nodes, n)
	}
	sort.Strings(nodes)
	for _, n := range nodes {
		pages.Add(map[string]string{"target": verdict.Target, "reason": "observer " + n + " claimed up without proof: " + verdict.RejectedProofs[n]})
	}
}

func (v *Verifier) observerNodes(ctx context.Context) ([]string, error) {
	pods, err := v.kube.CoreV1().Pods(v.cfg.ObserverNamespace).List(ctx, metav1.ListOptions{LabelSelector: v.cfg.ObserverSelector})
	if err != nil {
		return nil, err
	}
	nodes, err := v.kube.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	zone := map[string]string{}
	for _, n := range nodes.Items {
		zone[n.Name] = n.Labels[v.disc.ZoneLabel]
	}
	set := map[string]bool{}
	for _, p := range pods.Items {
		if p.Spec.NodeName == "" || p.DeletionTimestamp != nil || p.Status.Phase != corev1.PodRunning || v.disc.DRZones[zone[p.Spec.NodeName]] {
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

func (v *Verifier) primaryNode(ctx context.Context, m discovery.Member) string {
	if m.CurrentPrimary == "" {
		return ""
	}
	p, err := v.kube.CoreV1().Pods(m.Namespace).Get(ctx, m.CurrentPrimary, metav1.GetOptions{})
	if err != nil {
		return ""
	}
	return p.Spec.NodeName
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
