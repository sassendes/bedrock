package judge

import (
	"sort"

	"github.com/sassendes/bedrock/failover-controller/observe"
	"github.com/sassendes/bedrock/failover-controller/proof"
)

type Answer string

const (
	Alive  Answer = "alive"
	Dead   Answer = "dead"
	Silent Answer = "silent"
)

type Witness string

const (
	Replicas Witness = "replicas"
	Manager  Witness = "manager"
	Kubelet  Witness = "kubelet"
	API      Witness = "api"
)

var AllWitnesses = []Witness{Replicas, Manager, Kubelet, API}

type Outcome string

const (
	Up            Outcome = "up"
	ProvenDead    Outcome = "proven-dead"
	Contradiction Outcome = "contradiction"
	Unknown       Outcome = "unknown"
)

type Input struct {
	Target     string
	Challenge  string
	Eligible   []string
	Ballots    map[string]observe.Vote
	NodeOf     map[string]string
	PerNode    int
	Witnesses  map[Witness]Answer
	DeadNeeded int
}

type Verdict struct {
	Target         string
	Outcome        Outcome
	ValidUps       []string
	Downs          int
	Q              int
	DeadWitnesses  []Witness
	PrimarySigners []string
	RejectedProofs map[string]string
	ProvenDown     bool
}

// ValidUp is grade A: postgres signed this round's challenge, for this observer, as a primary.
func ValidUp(keys proof.Keys, challenge, observer string, v observe.Vote) (bool, string) {
	s := v.Proof.Signed
	switch {
	case s == nil:
		return false, "no signature"
	case s.Challenge != challenge:
		return false, "signed another round's challenge"
	case s.Observer != observer:
		return false, "signed for another observer"
	case s.InRecovery:
		return false, "signed as a replica"
	}
	if err := keys.Verify(*s); err != nil {
		return false, err.Error()
	}
	return true, ""
}

// Judge applies H.9.2 to one round's snapshot. Silence is never proof of death: only witnesses that
// answer dead count, and one valid A outweighs every down.
func Judge(keys proof.Keys, in Input) Verdict {
	v := Verdict{Target: in.Target, Q: len(in.Eligible)/2 + 1, RejectedProofs: map[string]string{}}
	eligible := map[string]bool{}
	for _, n := range in.Eligible {
		eligible[n] = true
	}
	signers := map[string]bool{}
	ids := make([]string, 0, len(in.Ballots))
	for id := range in.Ballots {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	perNode := map[string][]observe.Ballot{}
	for _, id := range ids {
		b := in.Ballots[id]
		node, known := in.NodeOf[id]
		ok, why := ValidUp(keys, in.Challenge, id, b)
		if ok {
			v.ValidUps = append(v.ValidUps, id)
			signers[b.Proof.Signed.Instance+"/"+b.Proof.Signed.SystemID] = true
		} else if b.Ballot == observe.BallotUp {
			v.RejectedProofs[id] = why
		}
		if !known {
			continue
		}
		switch {
		case ok:
			perNode[node] = append(perNode[node], observe.BallotUp)
		case b.Ballot == observe.BallotDown:
			perNode[node] = append(perNode[node], observe.BallotDown)
		default:
			perNode[node] = append(perNode[node], observe.BallotAbstain)
		}
	}
	for _, n := range in.Eligible {
		if observe.NodeBallot(perNode[n], in.PerNode) == observe.BallotDown {
			v.Downs++
		}
	}
	for s := range signers {
		v.PrimarySigners = append(v.PrimarySigners, s)
	}
	sort.Strings(v.PrimarySigners)
	for _, w := range AllWitnesses {
		if in.Witnesses[w] == Dead {
			v.DeadWitnesses = append(v.DeadWitnesses, w)
		}
	}
	v.ProvenDown = len(v.DeadWitnesses) >= in.DeadNeeded
	switch {
	case len(v.ValidUps) > 0 && v.ProvenDown:
		v.Outcome = Contradiction
	case len(v.ValidUps) > 0:
		v.Outcome = Up
	case len(in.Eligible) > 0 && v.Downs >= v.Q && v.ProvenDown:
		v.Outcome = ProvenDead
	default:
		v.Outcome = Unknown
	}
	return v
}

// SplitBrain is two different instances signing as primary in two consecutive rounds. One round of it is
// a switchover: the old primary signs before it steps down.
func SplitBrain(previous, current []string) bool {
	return len(previous) > 1 && len(current) > 1
}
