package decide

import (
	"math/rand"
	"testing"
	"time"

	"github.com/sassendes/bedrock/failover-controller/observe"
)

var t0 = time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)

func obs(node string, b observe.Ballot) Observation {
	return Observation{Node: node, Ballot: b, Fresh: true}
}

func input(target string, o ...Observation) Input {
	return Input{Target: target, Eligible: []string{"a", "b", "c"}, Observations: o}
}

func downs(target string) Input {
	return input(target, obs("a", observe.BallotDown), obs("b", observe.BallotDown), obs("c", observe.BallotDown))
}

func run(tr *Tracker, from, to time.Duration, step time.Duration, in func() []Input, cap int) []Verdict {
	var last []Verdict
	for d := from; d <= to; d += step {
		last = tr.Step(t0.Add(d), in(), cap)
	}
	return last
}

func TestParamsAreChecked(t *testing.T) {
	if err := DefaultParams().Check(); err != nil {
		t.Fatal(err)
	}
	p := DefaultParams()
	p.Freshness = p.ProbeInterval
	p.DeadAfter = time.Second
	if p.Check() == nil {
		t.Fatal("inconsistent params accepted")
	}
}

func TestQuorumIsAMajorityOfEligible(t *testing.T) {
	for e, q := range map[int]int{1: 1, 2: 2, 3: 2, 4: 3, 5: 3} {
		if Quorum(e) != q {
			t.Fatalf("eligible %d: quorum %d, want %d", e, Quorum(e), q)
		}
	}
}

func TestDeadOnlyAfterDeadAfter(t *testing.T) {
	tr := NewTracker(DefaultParams())
	v := run(tr, 0, 29*time.Second, time.Second, func() []Input { return []Input{downs("db")} }, 0)
	if v[0].State != Suspect {
		t.Fatalf("got %s before dead after", v[0].State)
	}
	v = tr.Step(t0.Add(30*time.Second), []Input{downs("db")}, 0)
	if v[0].State != Dead {
		t.Fatalf("got %s at dead after", v[0].State)
	}
}

func TestOneFreshUpVetoesAndResetsTheTimer(t *testing.T) {
	tr := NewTracker(DefaultParams())
	run(tr, 0, 25*time.Second, time.Second, func() []Input { return []Input{downs("db")} }, 0)
	in := input("db", obs("a", observe.BallotDown), obs("b", observe.BallotDown), obs("c", observe.BallotUp))
	if v := tr.Step(t0.Add(26*time.Second), []Input{in}, 0); v[0].State != Alive {
		t.Fatalf("fresh up did not veto: %s", v[0].State)
	}
	v := run(tr, 27*time.Second, 50*time.Second, time.Second, func() []Input { return []Input{downs("db")} }, 0)
	if v[0].State != Suspect {
		t.Fatalf("timer was not reset: %s", v[0].State)
	}
}

func TestStaleAndIneligibleVotesDoNotCount(t *testing.T) {
	tr := NewTracker(DefaultParams())
	in := Input{Target: "db", Eligible: []string{"a", "b", "c"}, Observations: []Observation{
		obs("a", observe.BallotDown),
		{Node: "b", Ballot: observe.BallotDown, Fresh: false},
		obs("primary-node", observe.BallotDown),
		obs("a", observe.BallotDown),
	}}
	v := run(tr, 0, time.Minute, time.Second, func() []Input { return []Input{in} }, 0)
	if v[0].State != Alive || v[0].Downs != 1 || len(v[0].Pages) == 0 {
		t.Fatalf("got %+v", v[0])
	}
}

func TestStaleUpDoesNotVeto(t *testing.T) {
	tr := NewTracker(DefaultParams())
	in := input("db", obs("a", observe.BallotDown), obs("b", observe.BallotDown), Observation{Node: "c", Ballot: observe.BallotUp})
	v := run(tr, 0, time.Minute, time.Second, func() []Input { return []Input{in} }, 0)
	if v[0].State != Dead {
		t.Fatalf("stale up blocked the decision: %s", v[0].State)
	}
}

func TestBrakesHold(t *testing.T) {
	cases := map[string]func(*Input){
		"paused":     func(i *Input) { i.Paused = true },
		"durability": func(i *Input) { i.DurabilityRequired = true },
		"cooldown":   func(i *Input) { i.LastFailover = t0 },
	}
	for name, mutate := range cases {
		tr := NewTracker(DefaultParams())
		v := run(tr, 0, 2*time.Minute, time.Second, func() []Input { in := downs("db"); mutate(&in); return []Input{in} }, 0)
		if v[0].State == Dead || len(v[0].Pages) == 0 {
			t.Fatalf("%s: got %+v", name, v[0])
		}
	}
	tr := NewTracker(DefaultParams())
	if v := run(tr, 0, 2*time.Minute, time.Second, func() []Input { return []Input{downs("db")} }, 1); v[0].State == Dead {
		t.Fatal("cap did not hold")
	}
}

func TestTransitioningResetsSuspicion(t *testing.T) {
	tr := NewTracker(DefaultParams())
	run(tr, 0, 25*time.Second, time.Second, func() []Input { return []Input{downs("db")} }, 0)
	in := downs("db")
	in.Transitioning = true
	tr.Step(t0.Add(26*time.Second), []Input{in}, 0)
	if v := tr.Step(t0.Add(31*time.Second), []Input{downs("db")}, 0); v[0].State != Suspect {
		t.Fatalf("got %s", v[0].State)
	}
}

func TestTargetBrakeOnlyWithEnoughTargets(t *testing.T) {
	tr := NewTracker(DefaultParams())
	many := func() []Input { return []Input{downs("a"), downs("b"), downs("c")} }
	for _, v := range run(tr, 0, 2*time.Minute, time.Second, many, 0) {
		if v.State == Dead {
			t.Fatalf("brake did not hold for %s", v.Target)
		}
	}
	tr = NewTracker(DefaultParams())
	two := func() []Input { return []Input{downs("a"), downs("b")} }
	for _, v := range run(tr, 0, 2*time.Minute, time.Second, two, 0) {
		if v.State != Dead {
			t.Fatalf("brake held with only %d targets: %s", 2, v.Target)
		}
	}
}

func TestObserverBrake(t *testing.T) {
	tr := NewTracker(DefaultParams())
	in := Input{Target: "db", Eligible: []string{"a", "b", "c", "d", "e"}, Observations: []Observation{
		obs("a", observe.BallotDown), obs("b", observe.BallotDown), obs("c", observe.BallotDown),
	}}
	v := run(tr, 0, 2*time.Minute, time.Second, func() []Input { return []Input{in} }, 0)
	if v[0].State == Dead {
		t.Fatal("two silent observers did not hold the decision")
	}
}

func TestPropertyNeverDeadWithoutTheRule(t *testing.T) {
	p := DefaultParams()
	rng := rand.New(rand.NewSource(1))
	nodes := []string{"a", "b", "c", "d", "e"}
	ballots := []observe.Ballot{observe.BallotUp, observe.BallotDown, observe.BallotDown, observe.BallotDown, observe.BallotAbstain}
	for run := 0; run < 300; run++ {
		tr := NewTracker(p)
		eligible := nodes[:1+rng.Intn(len(nodes))]
		var downSince time.Time
		for s := 0; s < 120; s++ {
			now := t0.Add(time.Duration(s) * time.Second)
			in := Input{Target: "db", Eligible: eligible}
			for _, n := range nodes {
				if rng.Intn(10) == 0 {
					continue
				}
				in.Observations = append(in.Observations, Observation{Node: n, Ballot: ballots[rng.Intn(len(ballots))], Fresh: rng.Intn(6) != 0})
			}
			v := tr.Step(now, []Input{in}, 0)[0]

			ups, downs := 0, 0
			el := map[string]bool{}
			for _, n := range eligible {
				el[n] = true
			}
			seen := map[string]bool{}
			for _, o := range in.Observations {
				if el[o.Node] && o.Fresh && !seen[o.Node] {
					seen[o.Node] = true
					if o.Ballot == observe.BallotUp {
						ups++
					}
					if o.Ballot == observe.BallotDown {
						downs++
					}
				}
			}
			holds := ups == 0 && downs >= Quorum(len(eligible))
			if !holds {
				downSince = time.Time{}
			} else if downSince.IsZero() {
				downSince = now
			}
			if v.State == Dead {
				if !holds {
					t.Fatalf("run %d step %d: dead with %d up and %d down of %d eligible", run, s, ups, downs, len(eligible))
				}
				if now.Sub(downSince) < p.DeadAfter {
					t.Fatalf("run %d step %d: dead after %s of continuous downs", run, s, now.Sub(downSince))
				}
				tr.Clear("db")
				downSince = time.Time{}
			}
		}
	}
}
