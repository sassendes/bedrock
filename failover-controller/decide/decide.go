package decide

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/sassendes/bedrock/failover-controller/observe"
)

type Params struct {
	ProbeInterval time.Duration
	Freshness     time.Duration
	DeadAfter     time.Duration
	CnpgGrace     time.Duration
	Cooldown      time.Duration
	CapWindow     time.Duration
	CapCount      int
	BrakeCount    int
	MaxAbstain    int
}

func DefaultParams() Params {
	return Params{
		ProbeInterval: 2 * time.Second,
		Freshness:     6 * time.Second,
		DeadAfter:     30 * time.Second,
		CnpgGrace:     time.Minute,
		Cooldown:      10 * time.Minute,
		CapWindow:     time.Hour,
		CapCount:      1,
		BrakeCount:    2,
		MaxAbstain:    1,
	}
}

func (p Params) Check() error {
	var errs []error
	if p.ProbeInterval <= 0 {
		errs = append(errs, errors.New("probe interval must be positive"))
	}
	if p.Freshness < 2*p.ProbeInterval {
		errs = append(errs, fmt.Errorf("freshness %s must be at least twice the probe interval %s", p.Freshness, p.ProbeInterval))
	}
	if p.DeadAfter < p.Freshness {
		errs = append(errs, fmt.Errorf("dead after %s must be at least the freshness window %s", p.DeadAfter, p.Freshness))
	}
	if p.CapCount < 1 || p.CapWindow <= 0 {
		errs = append(errs, errors.New("the failover cap needs a count of at least 1 and a positive window"))
	}
	if p.BrakeCount < 1 || p.MaxAbstain < 0 {
		errs = append(errs, errors.New("brake count must be at least 1 and max abstain not negative"))
	}
	return errors.Join(errs...)
}

type Observation struct {
	Node   string
	Ballot observe.Ballot
	Fresh  bool
}

type Input struct {
	Target             string
	Eligible           []string
	Observations       []Observation
	Paused             bool
	Transitioning      bool
	StandbysReady      bool
	DurabilityRequired bool
	LastFailover       time.Time
}

type State string

const (
	Alive   State = "alive"
	Suspect State = "suspect"
	Dead    State = "dead"
)

type Verdict struct {
	Target   string
	State    State
	Q        int
	Eligible int
	Ups      int
	Downs    int
	Abstains int
	Since    time.Time
	Pages    []string
}

type Tracker struct {
	p     Params
	since map[string]time.Time
}

func NewTracker(p Params) *Tracker { return &Tracker{p: p, since: map[string]time.Time{}} }

func (t *Tracker) Clear(target string) { delete(t.since, target) }

func Quorum(eligible int) int { return eligible/2 + 1 }

// Step applies the decision rule to every target at once. failoversInWindow counts runs the
// store recorded within CapWindow, so the cap holds across restarts of the decider.
func (t *Tracker) Step(now time.Time, inputs []Input, failoversInWindow int) []Verdict {
	out := make([]Verdict, 0, len(inputs))
	live := map[string]bool{}
	suspects := 0
	for _, in := range inputs {
		live[in.Target] = true
		v := t.evaluate(now, in)
		if v.State == Suspect {
			suspects++
		}
		out = append(out, v)
	}
	for k := range t.since {
		if !live[k] {
			delete(t.since, k)
		}
	}

	brake := len(inputs) > t.p.BrakeCount && suspects >= t.p.BrakeCount
	for i := range out {
		v := &out[i]
		in := inputs[i]
		wait := t.p.DeadAfter
		if in.StandbysReady && t.p.CnpgGrace > wait {
			wait = t.p.CnpgGrace
		}
		if v.State != Suspect || now.Sub(v.Since) < wait {
			continue
		}
		var hold []string
		if brake {
			hold = append(hold, fmt.Sprintf("brake: %d targets suspect at once", suspects))
		}
		if v.Abstains > t.p.MaxAbstain {
			hold = append(hold, fmt.Sprintf("observer brake: %d observers abstain or are silent", v.Abstains))
		}
		if in.Paused {
			hold = append(hold, "paused")
		}
		if in.DurabilityRequired {
			hold = append(hold, "primary has dataDurability required: a dead standby looks like a dead primary")
		}
		if !in.LastFailover.IsZero() && now.Sub(in.LastFailover) < t.p.Cooldown {
			hold = append(hold, fmt.Sprintf("cooldown: last failover %s ago", now.Sub(in.LastFailover).Round(time.Second)))
		}
		if failoversInWindow >= t.p.CapCount {
			hold = append(hold, fmt.Sprintf("cap: %d failover(s) in the last %s", failoversInWindow, t.p.CapWindow))
		}
		if len(hold) > 0 {
			v.Pages = append(v.Pages, hold...)
			continue
		}
		v.State = Dead
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Target < out[j].Target })
	return out
}

func (t *Tracker) evaluate(now time.Time, in Input) Verdict {
	v := Verdict{Target: in.Target, State: Alive, Eligible: len(in.Eligible), Q: Quorum(len(in.Eligible))}
	eligible := map[string]bool{}
	for _, n := range in.Eligible {
		eligible[n] = true
	}
	voted := map[string]bool{}
	for _, o := range in.Observations {
		if !eligible[o.Node] || !o.Fresh || voted[o.Node] {
			continue
		}
		voted[o.Node] = true
		switch o.Ballot {
		case observe.BallotUp:
			v.Ups++
		case observe.BallotDown:
			v.Downs++
		default:
			v.Abstains++
		}
	}
	v.Abstains += len(eligible) - len(voted)

	switch {
	case len(eligible) == 0:
		v.Pages = append(v.Pages, "no eligible observers")
		delete(t.since, in.Target)
	case v.Ups > 0:
		delete(t.since, in.Target)
	case in.Transitioning:
		v.Pages = append(v.Pages, "the operator is failing over inside the cluster, waiting")
		delete(t.since, in.Target)
	case v.Downs >= v.Q:
		since, ok := t.since[in.Target]
		if !ok {
			since = now
			t.since[in.Target] = now
		}
		v.State, v.Since = Suspect, since
	default:
		if v.Downs > 0 {
			v.Pages = append(v.Pages, fmt.Sprintf("quorum unreachable: %d down, %d needed, %d abstain", v.Downs, v.Q, v.Abstains))
		}
		delete(t.since, in.Target)
	}
	return v
}
