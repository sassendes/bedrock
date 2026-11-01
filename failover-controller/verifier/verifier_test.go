package verifier

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/sassendes/bedrock/failover-controller/act"
	"github.com/sassendes/bedrock/failover-controller/discovery"
	"github.com/sassendes/bedrock/failover-controller/judge"
	"github.com/sassendes/bedrock/failover-controller/metrics"
	"github.com/sassendes/bedrock/failover-controller/observe"
	"github.com/sassendes/bedrock/failover-controller/proof"
	"github.com/sassendes/bedrock/failover-controller/store"
	"github.com/sassendes/bedrock/failover-controller/testenv"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const target = "cluster-1/cluster-1"

var key = []byte("0123456789abcdef0123456789abcdef")

type fixed map[judge.Witness]judge.Answer

func (f *fixed) Gather(context.Context, discovery.Family) map[judge.Witness]judge.Answer { return *f }

type court struct {
	t      *testing.T
	world  *testenv.World
	replay string
	st     *store.Store
	v      *Verifier
	w      *fixed
	m      *metrics.Registry
	seq    uint64
	n      int
}

func newCourt(t *testing.T) *court {
	w := testenv.New()
	testenv.Optimistic(w.Kube, "configmaps")
	st := store.New(w.Kube, store.DefaultConfig())
	a := act.New(w.Dyn, w.Kube, act.DefaultConfig())
	alive := fixed{judge.Replicas: judge.Alive, judge.Manager: judge.Alive, judge.Kubelet: judge.Alive, judge.API: judge.Alive}
	c := &court{t: t, world: w, replay: "0/FFFFFF", st: st, w: &alive, m: metrics.New()}
	v, err := New(w.Dyn, w.Kube, a.Config().Discovery, st, a, c.w, proof.Keys{"k1": key}, DefaultConfig(), c.m, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	v.nonce = func() (string, error) { c.n++; return "challenge-" + string(rune('a'+c.n)), nil }
	v.replay = func(context.Context, discovery.Member) (string, error) { return c.replay, nil }
	c.v = v
	return c
}

func (c *court) tick() *store.Round {
	if err := c.v.Tick(context.Background()); err != nil {
		c.t.Fatal(err)
	}
	r, err := c.st.GetRound(context.Background())
	if err != nil || r == nil {
		c.t.Fatalf("no round: %v", err)
	}
	return r
}

func (c *court) dead(n int) {
	all := []judge.Witness{judge.Replicas, judge.Manager, judge.Kubelet, judge.API}
	for i, w := range all {
		if i < n {
			(*c.w)[w] = judge.Dead
		} else {
			(*c.w)[w] = judge.Alive
		}
	}
}

func sign(challenge, node, instance string) *proof.Signed {
	s := proof.Signed{KeyID: "k1", Challenge: challenge, Observer: node, Instance: instance, SystemID: "42", Timeline: 1, LSN: "0/1", AtMs: time.Now().UnixMilli()}
	m := hmac.New(sha256.New, key)
	m.Write([]byte(s.Payload()))
	s.MAC = hex.EncodeToString(m.Sum(nil))
	return &s
}

func resign(s *proof.Signed, observer string) *proof.Signed {
	if s == nil {
		return nil
	}
	cp := *s
	cp.Observer = observer
	m := hmac.New(sha256.New, key)
	m.Write([]byte(cp.Payload()))
	cp.MAC = hex.EncodeToString(m.Sum(nil))
	return &cp
}

// vote casts the same vote from both observers of a node, each signature made for its own observer.
func (c *court) vote(node string, v observe.Vote) {
	for _, slot := range []string{"a", "b"} {
		c.slotVote(node, slot, v)
	}
}

func (c *court) slotVote(node, slot string, v observe.Vote) {
	c.seq++
	v.Seq = c.seq
	v.Target = target
	if v.Proof.Signed != nil && v.Proof.Signed.Observer == node {
		v.Proof.Signed = resign(v.Proof.Signed, observe.ObserverID(node, slot))
	}
	if err := c.st.PublishVotes(context.Background(), node, slot, "", map[string]observe.Vote{target: v}); err != nil {
		c.t.Fatal(err)
	}
}

func (c *court) downs(challenge string, nodes ...string) {
	for _, n := range nodes {
		c.vote(n, observe.Vote{Challenge: challenge, Ballot: observe.BallotDown})
	}
}

func (c *court) claim(epoch int64) {
	cl := &store.Claim{Target: target, Epoch: epoch, State: store.ClaimOpen, Step: store.StepClaimed, Instance: "cluster-1-1", Candidate: "cluster-2/cluster-2", StartedAt: time.Now(), Times: map[string]string{}}
	if err := c.st.Open(context.Background(), cl, nil); err != nil {
		c.t.Fatal(err)
	}
}

func (c *court) approval() store.Approval {
	all, err := c.st.GetApprovals(context.Background())
	if err != nil {
		c.t.Fatal(err)
	}
	return all[store.ApprovalKey(target)]
}

func TestRoundsAdvanceWithAFreshChallengeEveryTick(t *testing.T) {
	c := newCourt(t)
	a := c.tick()
	b := c.tick()
	if b.Number != a.Number+1 || b.Challenge == a.Challenge || a.Challenge == "" {
		t.Fatalf("rounds %+v then %+v", a, b)
	}
}

func TestProvenDeathIsApprovedForTheClaimsEpoch(t *testing.T) {
	c := newCourt(t)
	r := c.tick()
	c.claim(3)
	c.downs(r.Challenge, "n2", "n3", "n4", "n5")
	c.dead(3)
	c.tick()
	if a := c.approval(); a.State != store.ApprovalValid || a.Epoch != 3 {
		t.Fatalf("approval %+v", a)
	}
}

func TestNoClaimNoApproval(t *testing.T) {
	c := newCourt(t)
	r := c.tick()
	c.downs(r.Challenge, "n2", "n3", "n4", "n5")
	c.dead(4)
	c.tick()
	if a := c.approval(); a.State != "" {
		t.Fatalf("approved without a claim: %+v", a)
	}
}

func TestTwoDeadWitnessesAreNotEnough(t *testing.T) {
	c := newCourt(t)
	r := c.tick()
	c.claim(1)
	c.downs(r.Challenge, "n2", "n3", "n4", "n5")
	c.dead(2)
	c.tick()
	if a := c.approval(); a.State == store.ApprovalValid {
		t.Fatal("approved on two witnesses")
	}
}

func TestDownsFromAnOlderRoundDoNotCount(t *testing.T) {
	c := newCourt(t)
	old := c.tick()
	c.downs(old.Challenge, "n2", "n3", "n4", "n5")
	c.tick()
	c.claim(1)
	c.dead(4)
	c.tick()
	if a := c.approval(); a.State == store.ApprovalValid {
		t.Fatal("approved on stale downs")
	}
}

func TestSignedUpRevokesBeforeTheFence(t *testing.T) {
	c := newCourt(t)
	r := c.tick()
	c.claim(1)
	c.downs(r.Challenge, "n2", "n3", "n4", "n5")
	c.dead(3)
	r = c.tick()
	if c.approval().State != store.ApprovalValid {
		t.Fatal("not approved")
	}
	c.downs(r.Challenge, "n2", "n3", "n4")
	c.vote("n5", observe.Vote{Challenge: r.Challenge, Ballot: observe.BallotUp, Proof: observe.Proof{Signed: sign(r.Challenge, "n5", "cluster-1@10.0.1.1")}})
	c.dead(0)
	c.tick()
	if a := c.approval(); a.State != store.ApprovalRevoked || a.Epoch != 1 {
		t.Fatalf("approval %+v", a)
	}
}

func TestALiarReplayingAPreCrashSignatureCannotBlock(t *testing.T) {
	c := newCourt(t)
	r := c.tick()
	c.claim(1)
	before := sign(r.Challenge, "n5", "cluster-1@10.0.1.1")
	r = c.tick()
	for i := 0; i < 3; i++ {
		c.downs(r.Challenge, "n2", "n3", "n4")
		c.vote("n5", observe.Vote{Challenge: r.Challenge, Ballot: observe.BallotUp, Proof: observe.Proof{Signed: before}})
		c.dead(4)
		r = c.tick()
	}
	if a := c.approval(); a.State != store.ApprovalValid {
		t.Fatalf("a replayed signature blocked the failover: %+v", a)
	}
	if c.m.Count("bedrock_verifier_page") == 0 {
		t.Fatal("the liar was not paged")
	}
}

func TestContradictionNeverApproves(t *testing.T) {
	c := newCourt(t)
	r := c.tick()
	c.claim(1)
	c.downs(r.Challenge, "n2", "n3", "n4")
	c.vote("n5", observe.Vote{Challenge: r.Challenge, Ballot: observe.BallotUp, Proof: observe.Proof{Signed: sign(r.Challenge, "n5", "cluster-1@10.0.1.1")}})
	c.dead(4)
	c.tick()
	if a := c.approval(); a.State == store.ApprovalValid {
		t.Fatal("approved over a signed up")
	}
}

func TestClosedClaimClosesItsApproval(t *testing.T) {
	c := newCourt(t)
	r := c.tick()
	c.claim(1)
	c.downs(r.Challenge, "n2", "n3", "n4", "n5")
	c.dead(3)
	c.tick()
	cl, _ := c.st.GetClaim(context.Background(), target)
	cl.State = store.ClaimClosed
	if err := c.st.Save(context.Background(), cl); err != nil {
		t.Fatal(err)
	}
	c.tick()
	if a := c.approval(); a.State != store.ApprovalClosed {
		t.Fatalf("approval %+v", a)
	}
}

func TestSplitBrainIsPagedOnlyAfterTwoRounds(t *testing.T) {
	c := newCourt(t)
	r := c.tick()
	twoPrimaries := func(ch string) {
		c.vote("n2", observe.Vote{Challenge: ch, Ballot: observe.BallotUp, Proof: observe.Proof{Signed: sign(ch, "n2", "cluster-1@10.0.1.1")}})
		c.vote("n3", observe.Vote{Challenge: ch, Ballot: observe.BallotUp, Proof: observe.Proof{Signed: sign(ch, "n3", "cluster-1@10.0.2.1")}})
	}
	twoPrimaries(r.Challenge)
	r = c.tick()
	if r.Split[target] {
		t.Fatal("one round of two signers was called split brain")
	}
	twoPrimaries(r.Challenge)
	r = c.tick()
	if !r.Split[target] {
		t.Fatal("two rounds of two signers were missed")
	}
}

func TestOneObserverPerNodeIsNotADown(t *testing.T) {
	c := newCourt(t)
	r := c.tick()
	c.claim(1)
	for _, n := range []string{"n2", "n3", "n4", "n5"} {
		c.slotVote(n, "a", observe.Vote{Challenge: r.Challenge, Ballot: observe.BallotDown})
		c.slotVote(n, "b", observe.Vote{Challenge: r.Challenge, Ballot: observe.BallotAbstain})
	}
	c.dead(4)
	c.tick()
	if a := c.approval(); a.State == store.ApprovalValid {
		t.Fatal("approved with only one observer of each node voting down")
	}
}

func TestOneObserversSignedUpBlocks(t *testing.T) {
	c := newCourt(t)
	r := c.tick()
	c.claim(1)
	c.downs(r.Challenge, "n2", "n3", "n4")
	c.slotVote("n5", "a", observe.Vote{Challenge: r.Challenge, Ballot: observe.BallotDown})
	c.slotVote("n5", "b", observe.Vote{Challenge: r.Challenge, Ballot: observe.BallotUp, Proof: observe.Proof{Signed: sign(r.Challenge, "n5/b", "cluster-1@10.0.1.1")}})
	c.dead(3)
	c.tick()
	if a := c.approval(); a.State == store.ApprovalValid {
		t.Fatal("approved over one observer's signed up")
	}
}

func TestCheckerHaltBlocksAndRevokes(t *testing.T) {
	c := newCourt(t)
	r := c.tick()
	c.claim(1)
	if err := c.st.PutHalts(context.Background(), map[string]bool{target: true}); err != nil {
		t.Fatal(err)
	}
	c.downs(r.Challenge, "n2", "n3", "n4", "n5")
	c.dead(4)
	r = c.tick()
	if a := c.approval(); a.State == store.ApprovalValid {
		t.Fatal("approved while the checker held everything")
	}
	if err := c.st.PutHalts(context.Background(), map[string]bool{target: false}); err != nil {
		t.Fatal(err)
	}
	c.downs(r.Challenge, "n2", "n3", "n4", "n5")
	r = c.tick()
	if a := c.approval(); a.State != store.ApprovalValid {
		t.Fatalf("not approved once the halt lifted: %+v", a)
	}
	if err := c.st.PutHalts(context.Background(), map[string]bool{target: true}); err != nil {
		t.Fatal(err)
	}
	c.downs(r.Challenge, "n2", "n3", "n4", "n5")
	c.tick()
	if a := c.approval(); a.State != store.ApprovalRevoked {
		t.Fatalf("the halt did not revoke: %+v", a)
	}
}

func (c *court) standbyReady() { c.standby(corev1.ConditionTrue) }

func (c *court) standby(ready corev1.ConditionStatus) {
	ctx := context.Background()
	p, err := c.world.Kube.CoreV1().Pods("cluster-1").Get(ctx, "cluster-1-2", metav1.GetOptions{})
	if err != nil {
		c.t.Fatal(err)
	}
	p.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: ready}}
	if _, err := c.world.Kube.CoreV1().Pods("cluster-1").UpdateStatus(ctx, p, metav1.UpdateOptions{}); err != nil {
		c.t.Fatal(err)
	}
}

func TestCnpgGetsItsWindowWhileStandbysAreReady(t *testing.T) {
	c := newCourt(t)
	c.standbyReady()
	r := c.tick()
	c.claim(1)
	c.dead(4)
	for i := int64(0); i < c.v.cfg.GraceRounds; i++ {
		c.downs(r.Challenge, "n2", "n3", "n4", "n5")
		r = c.tick()
		if a := c.approval(); a.State == store.ApprovalValid {
			t.Fatalf("approved after %d rounds, inside the cnpg window", i+1)
		}
	}
	c.downs(r.Challenge, "n2", "n3", "n4", "n5")
	c.tick()
	if a := c.approval(); a.State != store.ApprovalValid || a.Instance != "cluster-1-1" {
		t.Fatalf("not approved once the window closed: %+v", a)
	}
}

func TestTheWindowRestartsWhenTheDeathIsNotProvenInARow(t *testing.T) {
	c := newCourt(t)
	c.standbyReady()
	r := c.tick()
	c.claim(1)
	c.dead(4)
	for i := int64(0); i < c.v.cfg.GraceRounds; i++ {
		if i == c.v.cfg.GraceRounds-2 {
			r = c.tick()
			continue
		}
		c.downs(r.Challenge, "n2", "n3", "n4", "n5")
		r = c.tick()
	}
	c.downs(r.Challenge, "n2", "n3", "n4", "n5")
	c.tick()
	if a := c.approval(); a.State == store.ApprovalValid {
		t.Fatal("a gap in the proof did not restart the window")
	}
}

func TestAClaimForAnotherInstanceIsNeverApproved(t *testing.T) {
	c := newCourt(t)
	r := c.tick()
	cl := &store.Claim{Target: target, Epoch: 1, State: store.ClaimOpen, Step: store.StepClaimed, Instance: "cluster-1-2", Candidate: "cluster-2/cluster-2", Times: map[string]string{}}
	if err := c.st.Open(context.Background(), cl, nil); err != nil {
		t.Fatal(err)
	}
	c.downs(r.Challenge, "n2", "n3", "n4", "n5")
	c.dead(4)
	c.tick()
	if a := c.approval(); a.State == store.ApprovalValid {
		t.Fatal("approved a claim about an instance that is not the primary")
	}
}

func TestPrimaryMovingRevokes(t *testing.T) {
	c := newCourt(t)
	r := c.tick()
	c.claim(1)
	c.downs(r.Challenge, "n2", "n3", "n4", "n5")
	c.dead(4)
	r = c.tick()
	if c.approval().State != store.ApprovalValid {
		t.Fatal("not approved")
	}
	obj := c.world.Cluster("cluster-1")
	_ = unstructured.SetNestedField(obj.Object, "cluster-1-2", "status", "currentPrimary")
	if _, err := c.world.Dyn.Resource(testenv.Clusters).Namespace("cluster-1").Update(context.Background(), obj, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	c.downs(r.Challenge, "n2", "n3", "n4", "n5")
	c.tick()
	if a := c.approval(); a.State != store.ApprovalRevoked {
		t.Fatalf("cnpg moved the primary and the approval stood: %+v", a)
	}
}

func (c *court) provenAt(lsn string) *store.Round {
	r := c.tick()
	s := sign(r.Challenge, "n2", "cluster-1@10.0.1.1")
	s.LSN = lsn
	c.vote("n2", observe.Vote{Challenge: r.Challenge, Ballot: observe.BallotUp, Proof: observe.Proof{Signed: s}})
	return c.tick()
}

func TestABehindCandidateGetsTheCatchUpWindowThenIsPromotedAnyway(t *testing.T) {
	c := newCourt(t)
	r := c.provenAt("0/100")
	if r.ProvenLSN[target] != "0/100" {
		t.Fatalf("proven position %q", r.ProvenLSN[target])
	}
	c.claim(1)
	c.replay = "0/40"
	c.dead(4)
	for i := int64(0); i < c.v.cfg.CatchUpRounds; i++ {
		c.downs(r.Challenge, "n2", "n3", "n4", "n5")
		r = c.tick()
		if a := c.approval(); a.State == store.ApprovalValid {
			t.Fatalf("approved after %d rounds, inside the catch-up window", i+1)
		}
	}
	c.downs(r.Challenge, "n2", "n3", "n4", "n5")
	c.tick()
	if a := c.approval(); a.State != store.ApprovalValid {
		t.Fatalf("held past the catch-up window: %+v", a)
	}
	if lost, _ := c.m.Get("bedrock_verifier_promoted_behind_bytes", map[string]string{"target": target, "epoch": "1"}); lost != 0xc0 {
		t.Fatalf("reported %v bytes lost, want %v", lost, 0xc0)
	}
}

func TestACandidateThatCatchesUpIsPromotedAtOnceWithNothingLost(t *testing.T) {
	c := newCourt(t)
	r := c.provenAt("0/100")
	c.claim(1)
	c.replay = "0/FF"
	c.dead(4)
	c.downs(r.Challenge, "n2", "n3", "n4", "n5")
	r = c.tick()
	if a := c.approval(); a.State == store.ApprovalValid {
		t.Fatal("approved a candidate one byte behind, at once")
	}
	c.replay = "0/100"
	c.downs(r.Challenge, "n2", "n3", "n4", "n5")
	c.tick()
	if a := c.approval(); a.State != store.ApprovalValid {
		t.Fatalf("not approved once caught up: %+v", a)
	}
	if lost, ok := c.m.Get("bedrock_verifier_promoted_behind_bytes", map[string]string{"target": target, "epoch": "1"}); !ok || lost != 0 {
		t.Fatalf("lost %v, reported %v", lost, ok)
	}
}

func TestACandidateThatCannotBeReadIsPromotedAfterTheWindowAsUnknown(t *testing.T) {
	c := newCourt(t)
	r := c.provenAt("0/100")
	c.claim(1)
	c.v.replay = func(context.Context, discovery.Member) (string, error) { return "", errors.New("refused") }
	c.dead(4)
	for i := int64(0); i <= c.v.cfg.CatchUpRounds; i++ {
		c.downs(r.Challenge, "n2", "n3", "n4", "n5")
		r = c.tick()
	}
	if a := c.approval(); a.State != store.ApprovalValid {
		t.Fatalf("held forever on an unreadable candidate: %+v", a)
	}
	if lost, _ := c.m.Get("bedrock_verifier_promoted_behind_bytes", map[string]string{"target": target, "epoch": "1"}); lost != -1 {
		t.Fatalf("reported %v, want -1 for unknown", lost)
	}
}

func TestAStandbyThatIsNotReadyGivesNoWindow(t *testing.T) {
	c := newCourt(t)
	c.standby(corev1.ConditionFalse)
	r := c.tick()
	c.claim(1)
	c.downs(r.Challenge, "n2", "n3", "n4", "n5")
	c.dead(4)
	c.tick()
	if a := c.approval(); a.State != store.ApprovalValid {
		t.Fatalf("waited for a standby that is not ready: %+v", a)
	}
}

func TestTheWindowStartsOverForANewPrimary(t *testing.T) {
	c := newCourt(t)
	c.standbyReady()
	r := c.tick()
	c.dead(4)
	for i := int64(0); i <= c.v.cfg.GraceRounds; i++ {
		c.downs(r.Challenge, "n2", "n3", "n4", "n5")
		r = c.tick()
	}
	obj := c.world.Cluster("cluster-1")
	_ = unstructured.SetNestedField(obj.Object, "cluster-1-2", "status", "currentPrimary")
	if _, err := c.world.Dyn.Resource(testenv.Clusters).Namespace("cluster-1").Update(context.Background(), obj, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	p, _ := c.world.Kube.CoreV1().Pods("cluster-1").Get(context.Background(), "cluster-1-1", metav1.GetOptions{})
	p.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}
	if _, err := c.world.Kube.CoreV1().Pods("cluster-1").UpdateStatus(context.Background(), p, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	cl := &store.Claim{Target: target, Epoch: 1, State: store.ClaimOpen, Step: store.StepClaimed, Instance: "cluster-1-2", Candidate: "cluster-2/cluster-2", Times: map[string]string{}}
	if err := c.st.Open(context.Background(), cl, nil); err != nil {
		t.Fatal(err)
	}
	c.downs(r.Challenge, "n2", "n3", "n4", "n5")
	c.tick()
	if a := c.approval(); a.State == store.ApprovalValid {
		t.Fatal("the old primary's window was spent on the new one")
	}
}
