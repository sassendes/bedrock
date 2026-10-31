package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sassendes/bedrock/failover-controller/observe"
	"github.com/sassendes/bedrock/failover-controller/testenv"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"
)

func optimistic() *kubefake.Clientset {
	c := kubefake.NewSimpleClientset()
	testenv.Optimistic(c, "configmaps")
	return c
}

func TestVotesRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := New(optimistic(), DefaultConfig())
	v := map[string]observe.Vote{"cluster-1/cluster-1": {Target: "cluster-1/cluster-1", Ballot: observe.BallotDown, Seq: 7}}
	if err := s.PublishVotes(ctx, "n1", "key", v); err != nil {
		t.Fatal(err)
	}
	v["cluster-1/cluster-1"] = observe.Vote{Target: "cluster-1/cluster-1", Ballot: observe.BallotUp, Seq: 8}
	if err := s.PublishVotes(ctx, "n1", "key", v); err != nil {
		t.Fatal(err)
	}
	b, err := s.ListBallots(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) != 1 || b[0].Node != "n1" || b[0].Votes["cluster-1/cluster-1"].Seq != 8 {
		t.Fatalf("got %+v", b)
	}
}

func TestVoteObjectMustNameItsNode(t *testing.T) {
	ctx := context.Background()
	kube := kubefake.NewSimpleClientset(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Namespace: "bedrock-failover", Name: "vote-n1", Labels: map[string]string{"bedrock.io/failover-role": "vote"}},
		Data:       map[string]string{"node": "n2", "votes": "{}"},
	})
	b, err := New(kube, DefaultConfig()).ListBallots(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) != 0 {
		t.Fatalf("a vote object claiming another node was read: %+v", b)
	}
}

func TestOnlyOneClaimWins(t *testing.T) {
	ctx := context.Background()
	s := New(optimistic(), DefaultConfig())
	now := time.Now()
	a := &Claim{Target: "cluster-1/cluster-1", Epoch: 1, State: ClaimOpen, Decider: "a", StartedAt: now}
	b := &Claim{Target: "cluster-1/cluster-1", Epoch: 1, State: ClaimOpen, Decider: "b", StartedAt: now}
	if err := s.Open(ctx, a, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Open(ctx, b, nil); !errors.Is(err, ErrConflict) {
		t.Fatalf("second claim got %v", err)
	}
	got, err := s.GetClaim(ctx, a.Target)
	if err != nil || got.Decider != "a" {
		t.Fatalf("got %+v %v", got, err)
	}
	if err := s.Open(ctx, b, got); !errors.Is(err, ErrConflict) {
		t.Fatalf("claim over an open run got %v", err)
	}
}

func TestStaleClaimWriteConflicts(t *testing.T) {
	ctx := context.Background()
	s := New(optimistic(), DefaultConfig())
	c := &Claim{Target: "cluster-1/cluster-1", Epoch: 1, State: ClaimOpen, Decider: "a", StartedAt: time.Now(), Times: map[string]string{}}
	if err := s.Open(ctx, c, nil); err != nil {
		t.Fatal(err)
	}
	stale, _ := s.GetClaim(ctx, c.Target)
	c.Step = StepFencing
	if err := s.Save(ctx, c); err != nil {
		t.Fatal(err)
	}
	stale.Step = StepFlipped
	if err := s.Save(ctx, stale); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale write got %v", err)
	}
	closed, _ := s.GetClaim(ctx, c.Target)
	closed.State = ClaimClosed
	if err := s.Save(ctx, closed); err != nil {
		t.Fatal(err)
	}
	next := &Claim{Target: c.Target, Epoch: 2, State: ClaimOpen, Decider: "b", StartedAt: time.Now()}
	if err := s.Open(ctx, next, stale); !errors.Is(err, ErrConflict) {
		t.Fatalf("open from a stale read got %v", err)
	}
	if err := s.Open(ctx, next, closed); err != nil {
		t.Fatal(err)
	}
}

func TestOpenFromAClosedReadThatWentStaleConflicts(t *testing.T) {
	ctx := context.Background()
	s := New(optimistic(), DefaultConfig())
	first := &Claim{Target: "cluster-1/cluster-1", Epoch: 1, State: ClaimClosed, Decider: "a", StartedAt: time.Now()}
	if err := s.Open(ctx, first, nil); err != nil {
		t.Fatal(err)
	}
	read, _ := s.GetClaim(ctx, first.Target)
	other := &Claim{Target: first.Target, Epoch: 2, State: ClaimOpen, Decider: "b", StartedAt: time.Now()}
	if err := s.Open(ctx, other, read); err != nil {
		t.Fatal(err)
	}
	mine := &Claim{Target: first.Target, Epoch: 2, State: ClaimOpen, Decider: "c", StartedAt: time.Now()}
	if err := s.Open(ctx, mine, read); !errors.Is(err, ErrConflict) {
		t.Fatalf("two deciders opened epoch 2: %v", err)
	}
}
