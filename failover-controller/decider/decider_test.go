package decider

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sassendes/bedrock/failover-controller/act"
	"github.com/sassendes/bedrock/failover-controller/decide"
	"github.com/sassendes/bedrock/failover-controller/metrics"
	"github.com/sassendes/bedrock/failover-controller/observe"
	"github.com/sassendes/bedrock/failover-controller/store"
	"github.com/sassendes/bedrock/failover-controller/testenv"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8stesting "k8s.io/client-go/testing"
)

const target = "cluster-1/cluster-1"

var podsGVR = schema.GroupVersionResource{Version: "v1", Resource: "pods"}

type journal struct {
	mu     sync.Mutex
	events []string
}

func (j *journal) add(e string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.events = append(j.events, e)
}

func (j *journal) indexContains(prefix, part string) int {
	j.mu.Lock()
	defer j.mu.Unlock()
	for i, e := range j.events {
		if strings.HasPrefix(e, prefix) && strings.Contains(e, part) {
			return i
		}
	}
	return -1
}

func (j *journal) index(prefix string) int {
	j.mu.Lock()
	defer j.mu.Unlock()
	for i, e := range j.events {
		if strings.HasPrefix(e, prefix) {
			return i
		}
	}
	return -1
}

type lab struct {
	t       *testing.T
	w       *testenv.World
	st      *store.Store
	clock   time.Time
	seq     uint64
	journal *journal
	leader  bool
	m       *metrics.Registry
	verify  bool
	approve bool
	ups     map[string]bool
	liars   map[string]bool
}

func newLab(t *testing.T) *lab {
	w := testenv.New()
	testenv.Optimistic(w.Kube, "configmaps")
	l := &lab{t: t, w: w, st: store.New(w.Kube, store.DefaultConfig()), clock: time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC), journal: &journal{}, leader: true, m: metrics.New(), verify: true, approve: true}
	w.Kube.PrependReactor("delete", "pods", func(a k8stesting.Action) (bool, runtime.Object, error) {
		l.journal.add("delete pod " + a.GetNamespace() + "/" + a.(k8stesting.DeleteAction).GetName())
		return false, nil, nil
	})
	w.Kube.PrependReactor("patch", "services", func(a k8stesting.Action) (bool, runtime.Object, error) {
		l.journal.add("flip " + a.(k8stesting.PatchAction).GetName())
		return false, nil, nil
	})
	w.Dyn.PrependReactor("patch", "clusters", func(a k8stesting.Action) (bool, runtime.Object, error) {
		p := a.(k8stesting.PatchAction)
		l.journal.add("patch " + p.GetNamespace() + " " + string(p.GetPatch()))
		return false, nil, nil
	})
	return l
}

func (l *lab) decider(identity string) *Decider {
	cfg := DefaultConfig()
	cfg.Identity = identity
	a := act.New(l.w.Dyn, l.w.Kube, act.DefaultConfig())
	d, err := New(l.w.Dyn, l.w.Kube, a.Config().Discovery, l.st, a, cfg, func() bool { return l.leader }, l.m, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		l.t.Fatal(err)
	}
	d.now = func() time.Time { return l.clock }
	return d
}

func (l *lab) vote(ballots map[string]observe.Ballot) {
	ctx := context.Background()
	l.ups = map[string]bool{}
	for node, b := range ballots {
		if b == observe.BallotUp {
			l.ups[node] = true
		}
		l.seq++
		v := observe.Vote{Target: target, Ballot: b, Seq: l.seq}
		if err := l.st.PublishVotes(ctx, node, "", map[string]observe.Vote{target: v}); err != nil {
			l.t.Fatal(err)
		}
	}
}

// verifier stands in for the real one: every up in the lab is treated as signed, and an open claim is
// approved while approve is set.
func (l *lab) verifier() {
	ctx := context.Background()
	var ups []string
	for n := range l.ups {
		if !l.liars[n] {
			ups = append(ups, n)
		}
	}
	if err := l.st.PutRound(ctx, store.Round{Number: 1, Challenge: "c", ValidUps: map[string][]string{target: ups}}); err != nil {
		l.t.Fatal(err)
	}
	c := l.claim()
	if c != nil && c.State == store.ClaimOpen && c.Step == store.StepClaimed && l.approve {
		if err := l.st.SetApproval(ctx, target, store.Approval{State: store.ApprovalValid, Epoch: c.Epoch, Round: 1}); err != nil {
			l.t.Fatal(err)
		}
	}
}

func allDown() map[string]observe.Ballot {
	return map[string]observe.Ballot{"n2": observe.BallotDown, "n3": observe.BallotDown, "n4": observe.BallotDown, "n5": observe.BallotDown}
}

func (l *lab) run(d *Decider, seconds int, ballots func() map[string]observe.Ballot) {
	for i := 0; i < seconds; i++ {
		if ballots != nil {
			l.vote(ballots())
		}
		if l.verify {
			l.verifier()
		}
		if err := d.Tick(context.Background()); err != nil {
			l.t.Fatalf("tick: %v", err)
		}
		l.clock = l.clock.Add(time.Second)
	}
}

func (l *lab) claim() *store.Claim {
	c, err := l.st.GetClaim(context.Background(), target)
	if err != nil {
		l.t.Fatal(err)
	}
	return c
}

func (l *lab) promoted() bool {
	enabled, _, _ := unstructured.NestedBool(l.w.Cluster("cluster-2").Object, "spec", "replica", "enabled")
	return !enabled
}

func TestFullFailoverInOrder(t *testing.T) {
	l := newLab(t)
	d := l.decider("decider-0")
	l.run(d, 45, allDown)

	c := l.claim()
	if c == nil || c.State != store.ClaimClosed || c.Step != store.StepFlipped || c.Epoch != 1 {
		t.Fatalf("claim %+v", c)
	}
	if c.Candidate != "cluster-2/cluster-2" {
		t.Fatalf("promoted %s, the delayed replica must never be picked", c.Candidate)
	}
	an := l.w.Cluster("cluster-1").GetAnnotations()
	if an["cnpg.io/reconciliationLoop"] != "disabled" || an["cnpg.io/hibernation"] != "on" {
		t.Fatalf("old primary annotations %v", an)
	}
	if n := len(l.w.Pods("cluster-1", "cluster-1")); n != 0 {
		t.Fatalf("%d old primary pods left", n)
	}
	if !l.promoted() {
		t.Fatal("cluster-2 not promoted")
	}
	src, _, _ := unstructured.NestedString(l.w.Cluster("cluster-3").Object, "spec", "replica", "source")
	if src != "cluster-2" {
		t.Fatalf("cluster-3 follows %q", src)
	}
	svc, err := l.w.Kube.CoreV1().Services("application").Get(context.Background(), "postgres-rw", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if svc.Spec.ExternalName != "cluster-2-pooler-rw.cluster-2.svc.cluster.local" || svc.Labels["bedrock.io/points-at"] != "cluster-2" {
		t.Fatalf("postgres-rw %s %v", svc.Spec.ExternalName, svc.Labels)
	}

	fence := l.journal.index("patch cluster-1 ")
	lastDelete := -1
	for _, p := range []string{"delete pod cluster-1/cluster-1-1", "delete pod cluster-1/cluster-1-2"} {
		i := l.journal.index(p)
		if i < 0 {
			t.Fatalf("missing %q in %v", p, l.journal.events)
		}
		if i > lastDelete {
			lastDelete = i
		}
	}
	promote := l.journal.indexContains("patch cluster-2 ", `"replica":{"enabled":false}`)
	flip := l.journal.index("flip postgres-rw")
	if !(fence >= 0 && fence < lastDelete && lastDelete < promote && promote < flip) {
		t.Fatalf("wrong order: fence %d, delete %d, promote %d, flip %d: %v", fence, lastDelete, promote, flip, l.journal.events)
	}

	l.run(d, 120, allDown)
	if c := l.claim(); c.Epoch != 1 {
		t.Fatalf("a fenced primary failed over again: epoch %d", c.Epoch)
	}
}

func TestNotBeforeDeadAfter(t *testing.T) {
	l := newLab(t)
	l.run(l.decider("decider-0"), 29, allDown)
	if l.claim() != nil {
		t.Fatal("claimed before dead after")
	}
}

func TestFreshUpVetoes(t *testing.T) {
	l := newLab(t)
	l.run(l.decider("decider-0"), 120, func() map[string]observe.Ballot {
		b := allDown()
		b["n5"] = observe.BallotUp
		return b
	})
	if l.claim() != nil || l.promoted() {
		t.Fatal("failed over with a fresh up")
	}
}

func TestPrimaryNodeVoteDoesNotCount(t *testing.T) {
	l := newLab(t)
	d := l.decider("decider-0")
	d.cfg.Params.MaxAbstain = 2
	d.tracker = decide.NewTracker(d.cfg.Params)
	l.run(d, 120, func() map[string]observe.Ballot {
		return map[string]observe.Ballot{"n1": observe.BallotDown, "n2": observe.BallotDown, "n3": observe.BallotDown, "n4": observe.BallotAbstain, "n5": observe.BallotAbstain}
	})
	if l.claim() != nil {
		t.Fatal("the primary's own node helped reach quorum")
	}
}

func TestQuorumUnreachableDoesNothing(t *testing.T) {
	l := newLab(t)
	l.run(l.decider("decider-0"), 120, func() map[string]observe.Ballot {
		return map[string]observe.Ballot{"n2": observe.BallotDown, "n3": observe.BallotDown}
	})
	if l.claim() != nil {
		t.Fatal("2 of 4 eligible observers were enough")
	}
	if l.m.Count("bedrock_failover_page") == 0 {
		t.Fatal("no page for an unreachable quorum")
	}
}

func TestStaleVotesDoNotCount(t *testing.T) {
	l := newLab(t)
	l.vote(allDown())
	l.run(l.decider("decider-0"), 120, nil)
	if l.claim() != nil {
		t.Fatal("votes that never changed were treated as fresh")
	}
}

func TestNotLeaderDoesNothing(t *testing.T) {
	l := newLab(t)
	l.leader = false
	l.run(l.decider("decider-0"), 120, allDown)
	if l.claim() != nil {
		t.Fatal("a decider without the lease claimed")
	}
}

func TestUnreachableNodeHaltsBeforePromote(t *testing.T) {
	l := newLab(t)
	d := l.decider("decider-0")
	l.run(d, 32, allDown)
	if c := l.claim(); c == nil || c.State != store.ClaimOpen {
		t.Fatalf("expected an open claim, got %+v", c)
	}
	l.w.SetNodeReady("n1", false, l.clock)
	l.run(d, 180, allDown)
	c := l.claim()
	if c.State != store.ClaimHalted || !strings.Contains(c.Reason, "n1") {
		t.Fatalf("claim %+v", c)
	}
	if l.promoted() {
		t.Fatal("promoted while a node of the old primary was unreachable")
	}
}

func TestPodsThatNeverGoAwayHalt(t *testing.T) {
	l := newLab(t)
	l.w.Kube.PrependReactor("delete", "pods", func(k8stesting.Action) (bool, runtime.Object, error) { return true, nil, nil })
	l.run(l.decider("decider-0"), 200, allDown)
	if c := l.claim(); c == nil || c.State != store.ClaimHalted {
		t.Fatalf("claim %+v", c)
	}
	if l.promoted() {
		t.Fatal("promoted while old instance pods still existed")
	}
}

func TestNoReadyLiveReplicaTouchesNothing(t *testing.T) {
	l := newLab(t)
	l.w.SetStatus("cluster-2", "Cluster in healthy state", 0)
	l.run(l.decider("decider-0"), 120, allDown)
	if l.claim() != nil || len(l.w.Pods("cluster-1", "cluster-1")) != 2 {
		t.Fatal("fenced with nothing to promote")
	}
}

func TestOperatorFailingOverInsideTheClusterWaits(t *testing.T) {
	l := newLab(t)
	l.w.SetStatus("cluster-1", "Failing over", 1)
	l.run(l.decider("decider-0"), 120, allDown)
	if l.claim() != nil {
		t.Fatal("raced CNPG's own failover")
	}
}

func TestPauseHolds(t *testing.T) {
	l := newLab(t)
	obj := l.w.Cluster("cluster-1")
	obj.SetAnnotations(map[string]string{"bedrock.io/failover-pause": "true"})
	if _, err := l.w.Dyn.Resource(testenv.Clusters).Namespace("cluster-1").Update(context.Background(), obj, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	l.run(l.decider("decider-0"), 120, allDown)
	if l.claim() != nil {
		t.Fatal("paused cluster failed over")
	}
}

func TestNewLeaderResumesTheSameClaim(t *testing.T) {
	l := newLab(t)
	a := l.decider("decider-0")
	l.run(a, 33, allDown)
	c := l.claim()
	if c == nil || c.Step != store.StepFencing {
		t.Fatalf("expected the fence to be under way, got %+v", c)
	}
	b := l.decider("decider-1")
	l.run(b, 10, allDown)
	c = l.claim()
	if c.State != store.ClaimClosed || c.Decider != "decider-0" || c.Epoch != 1 {
		t.Fatalf("claim %+v", c)
	}
	promotes := 0
	for _, e := range l.journal.events {
		if strings.HasPrefix(e, "patch cluster-2 ") && strings.Contains(e, `"replica":{"enabled":false}`) {
			promotes++
		}
	}
	if promotes != 1 {
		t.Fatalf("%d promotions", promotes)
	}
}

func TestVetoBetweenClaimAndFence(t *testing.T) {
	l := newLab(t)
	d := l.decider("decider-0")
	l.run(d, 32, allDown)
	if c := l.claim(); c == nil || c.Step != store.StepClaimed {
		t.Fatalf("expected a fresh claim, got %+v", c)
	}
	up := allDown()
	up["n5"] = observe.BallotUp
	l.vote(up)
	l.run(d, 1, nil)
	c := l.claim()
	if c.State != store.ClaimClosed || !strings.Contains(c.Reason, "vetoed") || len(l.w.Pods("cluster-1", "cluster-1")) != 2 {
		t.Fatalf("claim %+v", c)
	}
	l.run(d, 31, allDown)
	if c := l.claim(); c.Epoch != 2 {
		t.Fatalf("a vetoed run must not start the cooldown: epoch %d", c.Epoch)
	}
}

func TestNodeThatFlappedAfterTheClaimHalts(t *testing.T) {
	l := newLab(t)
	d := l.decider("decider-0")
	l.w.Kube.PrependReactor("delete", "pods", func(k8stesting.Action) (bool, runtime.Object, error) { return true, nil, nil })
	l.run(d, 33, allDown)
	if c := l.claim(); c == nil || c.Step != store.StepFencing {
		t.Fatalf("expected the fence under way, got %+v", c)
	}
	l.w.SetNodeReady("n1", false, l.clock)
	for _, p := range []string{"cluster-1-1", "cluster-1-2"} {
		if err := l.w.Kube.Tracker().Delete(podsGVR, "cluster-1", p); err != nil {
			t.Fatal(err)
		}
	}
	l.run(d, 3, allDown)
	l.w.SetNodeReady("n1", true, l.clock)
	l.run(d, 120, allDown)
	c := l.claim()
	if c.State != store.ClaimHalted || !strings.Contains(c.Reason, "became Ready again") {
		t.Fatalf("claim %+v", c)
	}
	if l.promoted() {
		t.Fatal("promoted on a node that came back with a force deleted primary")
	}
}

func TestPrimaryPodMissingRefusesToClaim(t *testing.T) {
	l := newLab(t)
	if err := l.w.Kube.Tracker().Delete(podsGVR, "cluster-1", "cluster-1-1"); err != nil {
		t.Fatal(err)
	}
	l.run(l.decider("decider-0"), 120, allDown)
	if l.claim() != nil {
		t.Fatal("claimed while the old primary's pod was missing from the API")
	}
}

func TestLosingTheLeaseFreezesAnOpenClaim(t *testing.T) {
	l := newLab(t)
	d := l.decider("decider-0")
	l.run(d, 32, allDown)
	before := l.claim()
	if before == nil || before.Step != store.StepClaimed {
		t.Fatalf("expected a fresh claim, got %+v", before)
	}
	l.leader = false
	l.run(d, 60, allDown)
	if c := l.claim(); c.Step != before.Step || c.State != store.ClaimOpen || len(l.w.Pods("cluster-1", "cluster-1")) != 2 {
		t.Fatalf("a decider without the lease moved the claim: %+v", c)
	}
}

func TestNoApprovalNoFence(t *testing.T) {
	l := newLab(t)
	l.approve = false
	l.run(l.decider("decider-0"), 400, allDown)
	c := l.claim()
	if c == nil || c.State != store.ClaimHalted || len(l.w.Pods("cluster-1", "cluster-1")) != 2 {
		t.Fatalf("acted without the verifier's approval: %+v", c)
	}
}

func TestRevokedApprovalClosesTheClaim(t *testing.T) {
	l := newLab(t)
	l.approve = false
	d := l.decider("decider-0")
	l.run(d, 32, allDown)
	c := l.claim()
	if c == nil || c.Step != store.StepClaimed {
		t.Fatalf("claim %+v", c)
	}
	if err := l.st.SetApproval(context.Background(), target, store.Approval{State: store.ApprovalRevoked, Epoch: c.Epoch}); err != nil {
		t.Fatal(err)
	}
	l.run(d, 3, allDown)
	c = l.claim()
	if c.State != store.ClaimClosed || !strings.Contains(c.Reason, "revoked") || len(l.w.Pods("cluster-1", "cluster-1")) != 2 {
		t.Fatalf("claim %+v", c)
	}
}

func TestAnApprovalForAnotherEpochIsNotPermission(t *testing.T) {
	l := newLab(t)
	l.approve = false
	d := l.decider("decider-0")
	if err := l.st.SetApproval(context.Background(), target, store.Approval{State: store.ApprovalValid, Epoch: 7}); err != nil {
		t.Fatal(err)
	}
	l.run(d, 60, allDown)
	if len(l.w.Pods("cluster-1", "cluster-1")) != 2 {
		t.Fatal("fenced on an approval for another epoch")
	}
}

func TestALiarsUnprovenUpDoesNotVeto(t *testing.T) {
	l := newLab(t)
	l.liars = map[string]bool{"n5": true}
	d := l.decider("decider-0")
	l.run(d, 45, func() map[string]observe.Ballot {
		b := allDown()
		b["n5"] = observe.BallotUp
		return b
	})
	if c := l.claim(); c == nil || !l.promoted() {
		t.Fatalf("a liar's up without proof blocked the failover: %+v", c)
	}
}

func TestEveryWriteCarriesTheEpoch(t *testing.T) {
	l := newLab(t)
	l.run(l.decider("decider-0"), 45, allDown)
	n := 0
	for _, e := range l.journal.events {
		if strings.HasPrefix(e, "patch ") {
			n++
			if !strings.Contains(e, `"bedrock.io/failover-epoch":"1"`) {
				t.Fatalf("write without the epoch: %s", e)
			}
		}
	}
	if n < 3 {
		t.Fatalf("only %d writes seen", n)
	}
}
