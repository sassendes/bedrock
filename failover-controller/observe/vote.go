package observe

import "fmt"

type Ballot string

const (
	BallotUp      Ballot = "up"
	BallotDown    Ballot = "down"
	BallotAbstain Ballot = "abstain"
)

type Vote struct {
	Target string `json:"target"`
	Ballot Ballot `json:"ballot"`
	Reason string `json:"reason"`
	Proof  Proof  `json:"proof"`
	Seq    uint64 `json:"seq"`
}

// Valid is the dumb check every reader runs on an up: a primary that wrote my nonce, at the expected system.
func Valid(p Proof, expectedSystemID string) bool {
	return p.Result == ResultUp && p.Nonce != "" && p.EchoNonce == p.Nonce && !p.InRecovery &&
		p.LSN != "" && p.SystemID != "" && (expectedSystemID == "" || p.SystemID == expectedSystemID)
}

// Cast turns my own check, and the exchange verdict when my check failed, into my one vote.
// Peers never vote for me: their answers can only make me abstain.
func Cast(target, expectedSystemID string, own Proof, j *Judgement) Vote {
	v := Vote{Target: target, Proof: own}
	if own.Result == ResultUp {
		if Valid(own, expectedSystemID) {
			v.Ballot, v.Reason = BallotUp, "strong check passed"
			return v
		}
		v.Ballot, v.Reason = BallotAbstain, fmt.Sprintf("up without valid proof (system %q)", own.SystemID)
		return v
	}
	if j == nil {
		v.Ballot, v.Reason = BallotAbstain, "own check failed and no exchange was run: "+own.Reason
		return v
	}
	switch j.Verdict {
	case VerdictConfirmedDown:
		v.Ballot, v.Reason = BallotDown, own.Reason+"; "+j.Reason
	case VerdictOwnPathBroken:
		v.Ballot, v.Reason = BallotAbstain, "observer path degraded: "+j.Reason
	default:
		v.Ballot, v.Reason = BallotAbstain, "isolated: "+j.Reason
	}
	return v
}
