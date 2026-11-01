package verifier

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
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
)

const target = "cluster-1/cluster-1"

var key = []byte("0123456789abcdef0123456789abcdef")

type fixed map[judge.Witness]judge.Answer

func (f *fixed) Gather(context.Context, discovery.Family) map[judge.Witness]judge.Answer { return *f }

type court struct {
	t   *testing.T
	st  *store.Store
	v   *Verifier
	w   *fixed
	m   *metrics.Registry
	seq uint64
	n   int
}

func newCourt(t *testing.T) *court {
	w := testenv.New()
	testenv.Optimistic(w.Kube, "configmaps")
	st := store.New(w.Kube, store.DefaultConfig())
	a := act.New(w.Dyn, w.Kube, act.DefaultConfig())
	alive := fixed{judge.Replicas: judge.Alive, judge.Manager: judge.Alive, judge.Kubelet: judge.Alive, judge.API: judge.Alive}
	c := &court{t: t, st: st, w: &alive, m: metrics.New()}
	v, err := New(w.Dyn, w.Kube, a.Config().Discovery, st, a, c.w, proof.Keys{"k1": key}, DefaultConfig(), c.m, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	v.nonce = func() (string, error) { c.n++; return "challenge-" + string(rune('a'+c.n)), nil }
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

func (c *court) vote(node string, v observe.Vote) {
	c.seq++
	v.Seq = c.seq
	v.Target = target
	if err := c.st.PublishVotes(context.Background(), node, "", map[string]observe.Vote{target: v}); err != nil {
		c.t.Fatal(err)
	}
}

func (c *court) downs(challenge string, nodes ...string) {
	for _, n := range nodes {
		c.vote(n, observe.Vote{Challenge: challenge, Ballot: observe.BallotDown})
	}
}

func (c *court) claim(epoch int64) {
	cl := &store.Claim{Target: target, Epoch: epoch, State: store.ClaimOpen, Step: store.StepClaimed, StartedAt: time.Now(), Times: map[string]string{}}
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
