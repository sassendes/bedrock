package store

import (
	"context"
	"testing"
	"time"
)

func TestRoundRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := New(optimistic(), DefaultConfig())
	if r, err := s.GetRound(ctx); err != nil || r != nil {
		t.Fatalf("empty store returned %+v %v", r, err)
	}
	want := Round{Number: 3, Challenge: "abc", IssuedAt: time.Now().UTC().Truncate(time.Second), ValidUps: map[string][]string{"cluster-1/cluster-1": {"n2"}}}
	if err := s.PutRound(ctx, want); err != nil {
		t.Fatal(err)
	}
	want.Number = 4
	if err := s.PutRound(ctx, want); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetRound(ctx)
	if err != nil || got.Number != 4 || got.Challenge != "abc" || got.ValidUps["cluster-1/cluster-1"][0] != "n2" {
		t.Fatalf("got %+v %v", got, err)
	}
}

func TestOneApprovalSlotPerTargetIsOverwritten(t *testing.T) {
	ctx := context.Background()
	s := New(optimistic(), DefaultConfig())
	if err := s.SetApproval(ctx, "cluster-1/cluster-1", Approval{State: ApprovalValid, Epoch: 1, Round: 10}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetApproval(ctx, "cluster-2/cluster-2", Approval{State: ApprovalValid, Epoch: 1, Round: 11}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetApproval(ctx, "cluster-1/cluster-1", Approval{State: ApprovalValid, Epoch: 2, Round: 40}); err != nil {
		t.Fatal(err)
	}
	all, err := s.GetApprovals(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("%d slots, want one per target: %+v", len(all), all)
	}
	if a := all[ApprovalKey("cluster-1/cluster-1")]; a.Epoch != 2 || a.State != ApprovalValid || a.Round != 40 {
		t.Fatalf("epoch 1 approval survived: %+v", a)
	}
}

func TestOnlyAValidSlotNamesACandidateOrHost(t *testing.T) {
	ctx := context.Background()
	s := New(optimistic(), DefaultConfig())
	for _, st := range []ApprovalState{ApprovalRevoked, ApprovalClosed} {
		if err := s.SetApproval(ctx, "cluster-1/cluster-1", Approval{State: ApprovalValid, Epoch: 1, Candidate: "cluster-2/cluster-2", Host: "h"}); err != nil {
			t.Fatal(err)
		}
		if err := s.SetApproval(ctx, "cluster-1/cluster-1", Approval{State: st, Epoch: 1, Candidate: "cluster-2/cluster-2", Host: "h"}); err != nil {
			t.Fatal(err)
		}
		got, err := s.GetApprovals(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if a := got["cluster-1.cluster-1"]; a.State != st || a.Candidate != "" || a.Host != "" {
			t.Fatalf("a %s slot still names %+v", st, a)
		}
	}
}
