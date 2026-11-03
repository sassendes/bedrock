package judge

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/sassendes/bedrock/failover-controller/observe"
	"github.com/sassendes/bedrock/failover-controller/proof"
)

var key = []byte("0123456789abcdef0123456789abcdef")
var keys = proof.Keys{"k1": key}

func signed(challenge, node string, recovery bool, instance string) *proof.Signed {
	s := proof.Signed{KeyID: "k1", Challenge: challenge, Observer: node, Instance: instance, SystemID: "42", Timeline: 1, LSN: "0/1", InRecovery: recovery, AtMs: 1}
	m := hmac.New(sha256.New, key)
	m.Write([]byte(s.Payload()))
	s.MAC = hex.EncodeToString(m.Sum(nil))
	return &s
}

func up(challenge, node string) observe.Vote {
	return observe.Vote{Ballot: observe.BallotUp, Proof: observe.Proof{Signed: signed(challenge, node, false, "cluster-1@10.0.1.1")}}
}

func down() observe.Vote { return observe.Vote{Ballot: observe.BallotDown} }

func witnesses(dead int) map[Witness]Answer {
	m := map[Witness]Answer{}
	for i, w := range AllWitnesses {
		if i < dead {
			m[w] = Dead
		} else {
			m[w] = Alive
		}
	}
	return m
}

func in(ballots map[string]observe.Vote, w map[Witness]Answer) Input {
	nodeOf := map[string]string{}
	for id := range ballots {
		nodeOf[id] = id
	}
	return Input{Target: "cluster-1/cluster-1", Cluster: "cluster-1", Challenge: "c2", Eligible: []string{"a", "b", "c"}, Ballots: ballots, NodeOf: nodeOf, PerNode: 1, Witnesses: w, DeadNeeded: 3}
}

func paired(ballots map[string]observe.Vote, w map[Witness]Answer) Input {
	nodeOf := map[string]string{}
	for id := range ballots {
		nodeOf[id] = id[:1]
	}
	return Input{Target: "cluster-1/cluster-1", Cluster: "cluster-1", Challenge: "c2", Eligible: []string{"a", "b", "c"}, Ballots: ballots, NodeOf: nodeOf, PerNode: 2, Witnesses: w, DeadNeeded: 3}
}

func TestANodeIsDownOnlyWhenBothItsObserversAre(t *testing.T) {
	both := map[string]observe.Vote{"a/1": down(), "a/2": down(), "b/1": down(), "b/2": down()}
	if v := Judge(keys, paired(both, witnesses(3))); v.Outcome != ProvenDead || v.Downs != 2 {
		t.Fatalf("got %+v", v)
	}
	half := map[string]observe.Vote{"a/1": down(), "a/2": {Ballot: observe.BallotAbstain}, "b/1": down(), "b/2": down(), "c/1": down()}
	if v := Judge(keys, paired(half, witnesses(4))); v.Outcome != Unknown || v.Downs != 1 {
		t.Fatalf("a node with one observer down counted: %+v", v)
	}
}

func TestOneObserversSignatureIsUpForTheWholeNode(t *testing.T) {
	b := map[string]observe.Vote{"a/1": down(), "a/2": down(), "b/1": down(), "b/2": down(), "c/1": down(), "c/2": up("c2", "c/2")}
	if v := Judge(keys, paired(b, witnesses(2))); v.Outcome != Up {
		t.Fatalf("got %+v", v)
	}
}

func TestProvenDeadNeedsAMajorityAndThreeWitnesses(t *testing.T) {
	downs := map[string]observe.Vote{"a": down(), "b": down(), "c": down()}
	if v := Judge(keys, in(downs, witnesses(3))); v.Outcome != ProvenDead {
		t.Fatalf("got %+v", v)
	}
	if v := Judge(keys, in(downs, witnesses(2))); v.Outcome != Unknown {
		t.Fatalf("two witnesses were enough: %+v", v)
	}
	one := map[string]observe.Vote{"a": down()}
	if v := Judge(keys, in(one, witnesses(4))); v.Outcome != Unknown {
		t.Fatalf("one down was enough: %+v", v)
	}
}

func TestSilenceIsNeverDeath(t *testing.T) {
	w := map[Witness]Answer{Replicas: Dead, Manager: Dead, Kubelet: Silent, API: Silent}
	downs := map[string]observe.Vote{"a": down(), "b": down(), "c": down()}
	if v := Judge(keys, in(downs, w)); v.Outcome != Unknown {
		t.Fatalf("silent witnesses counted as dead: %+v", v)
	}
}

func TestOneSignedUpBeatsEveryDown(t *testing.T) {
	b := map[string]observe.Vote{"a": down(), "b": down(), "c": up("c2", "c")}
	v := Judge(keys, in(b, witnesses(0)))
	if v.Outcome != Up || len(v.ValidUps) != 1 {
		t.Fatalf("got %+v", v)
	}
}

func TestUpAndProvenDownIsAContradiction(t *testing.T) {
	b := map[string]observe.Vote{"a": down(), "b": down(), "c": up("c2", "c")}
	if v := Judge(keys, in(b, witnesses(3))); v.Outcome != Contradiction {
		t.Fatalf("got %+v", v)
	}
}

func TestUpsThatProveNothing(t *testing.T) {
	forged := up("c2", "c")
	forged.Proof.Signed.MAC = hex.EncodeToString(make([]byte, 32))
	borrowed := up("c2", "a")
	unsigned := observe.Vote{Ballot: observe.BallotUp}
	replica := observe.Vote{Ballot: observe.BallotUp, Proof: observe.Proof{Signed: signed("c2", "c", true, "cluster-1@10.0.1.2")}}
	cases := map[string]observe.Vote{
		"replayed from the last round": up("c1", "c"),
		"forged":                       forged,
		"another observer's signature": borrowed,
		"no signature at all":          unsigned,
		"signed as a replica":          replica,
	}
	for name, liar := range cases {
		b := map[string]observe.Vote{"a": down(), "b": down(), "c": liar}
		v := Judge(keys, in(b, witnesses(3)))
		if v.Outcome != ProvenDead {
			t.Fatalf("%s blocked a proven death: %+v", name, v)
		}
		if v.RejectedProofs["c"] == "" {
			t.Fatalf("%s was not reported as rejected", name)
		}
	}
}

func TestIneligibleDownsDoNotCount(t *testing.T) {
	b := map[string]observe.Vote{"a": down(), "primary-node": down(), "x": down()}
	if v := Judge(keys, in(b, witnesses(4))); v.Outcome != Unknown {
		t.Fatalf("got %+v", v)
	}
}

func TestSplitBrainNeedsTwoRoundsInARow(t *testing.T) {
	two := []string{"cluster-1@10.0.1.1/42", "cluster-1@10.0.1.2/42"}
	one := []string{"cluster-1@10.0.1.2/42"}
	if SplitBrain(nil, two) || SplitBrain(two, one) || SplitBrain(one, two) {
		t.Fatal("a switchover was called split brain")
	}
	if !SplitBrain(two, two) {
		t.Fatal("two primaries in two rounds were missed")
	}
}

func TestPrimarySignersAreCollected(t *testing.T) {
	a := up("c2", "a")
	b := up("c2", "b")
	b.Proof.Signed = signed("c2", "b", false, "cluster-1@10.0.1.2")
	v := Judge(keys, in(map[string]observe.Vote{"a": a, "b": b}, witnesses(0)))
	if len(v.PrimarySigners) != 2 {
		t.Fatalf("signers %v", v.PrimarySigners)
	}
}

func signedBy(instance, node string) observe.Vote {
	return observe.Vote{Ballot: observe.BallotUp, Proof: observe.Proof{Signed: signed("c2", node, false, instance)}}
}

func TestASiblingPrimarySignatureIsNotAnUpForTheTarget(t *testing.T) {
	v := Judge(keys, in(map[string]observe.Vote{"a": down(), "b": down(), "c": signedBy("cluster-2@10.0.1.1", "c")}, witnesses(3)))
	if v.Outcome != ProvenDead || len(v.ValidUps) != 0 {
		t.Fatalf("cluster-2 shares cluster-1's system id; its signature vetoed cluster-1's failover: %+v", v)
	}
	if v.RejectedProofs["c"] == "" {
		t.Fatalf("the sibling signature was not reported: %+v", v)
	}
}

func TestANameThatOnlyStartsLikeTheTargetIsRejected(t *testing.T) {
	v := Judge(keys, in(map[string]observe.Vote{"c": signedBy("cluster-10@10.0.1.1", "c")}, witnesses(0)))
	if len(v.ValidUps) != 0 {
		t.Fatalf("cluster-10 counted as cluster-1: %+v", v)
	}
}

func TestTheTargetSigningFromAnotherPodStillCounts(t *testing.T) {
	v := Judge(keys, in(map[string]observe.Vote{"c": signedBy("cluster-1@10.0.9.9", "c")}, witnesses(3)))
	if v.Outcome != Contradiction || len(v.ValidUps) != 1 {
		t.Fatalf("a move inside the cluster must still count as up, see model/Signer.tla SignerBindIP: %+v", v)
	}
}

func TestNoClusterNameRejectsEverything(t *testing.T) {
	i := in(map[string]observe.Vote{"c": up("c2", "c")}, witnesses(0))
	i.Cluster = ""
	if v := Judge(keys, i); len(v.ValidUps) != 0 {
		t.Fatalf("an empty expected cluster accepted a signature: %+v", v)
	}
}

func TestTheClusterNameIsCoveredByTheMAC(t *testing.T) {
	vote := up("c2", "c")
	vote.Proof.Signed.Instance = "cluster-1@10.0.1.2"
	if ok, _ := ValidUp(keys, "c2", "c", "cluster-1", vote); ok {
		t.Fatal("a rewritten instance verified")
	}
}
