package observer

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/sassendes/bedrock/failover-controller/act"
	"github.com/sassendes/bedrock/failover-controller/metrics"
	"github.com/sassendes/bedrock/failover-controller/observe"
	"github.com/sassendes/bedrock/failover-controller/store"
	"github.com/sassendes/bedrock/failover-controller/testenv"
)

const target = "cluster-1/cluster-1"

type mesh struct {
	mu    sync.Mutex
	nodes map[string]*observe.Node
}

func (m *mesh) Send(ctx context.Context, _, to string, env observe.Envelope) error {
	m.mu.Lock()
	n, ok := m.nodes[to]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("no route to %s", to)
	}
	go n.Handle(context.Background(), env)
	return nil
}

type noAddrs struct{}

func (noAddrs) SetAddrs(map[string]string) {}

type reach struct {
	mu sync.Mutex
	up map[string]bool
}

func (r *reach) set(node string, up bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.up[node] = up
}

func (r *reach) check(_ context.Context, cfg observe.CheckConfig, observer, tgt, nonce string) observe.Proof {
	r.mu.Lock()
	up := r.up[observer]
	r.mu.Unlock()
	p := observe.Proof{Observer: observer, Target: tgt, Nonce: nonce, Result: observe.ResultDown, Reason: "connect: refused"}
	if up && cfg.Host != "" {
		p.Result, p.EchoNonce, p.LSN, p.SystemID, p.Timeline = observe.ResultUp, nonce, "0/3000000", "7400000000000000001", 1
	}
	return p
}

func setup(t *testing.T, nodes ...string) (map[string]*Observer, *reach, *store.Store) {
	w := testenv.New()
	testenv.Optimistic(w.Kube, "configmaps")
	st := store.New(w.Kube, store.DefaultConfig())
	cfg := act.DefaultConfig()
	m := &mesh{nodes: map[string]*observe.Node{}}
	r := &reach{up: map[string]bool{}}
	out := map[string]*Observer{}
	for _, n := range nodes {
		_, key, _ := ed25519.GenerateKey(rand.Reader)
		o := New(w.Kube, w.Dyn, cfg.Discovery, act.New(w.Dyn, w.Kube, cfg), st, Config{
			Node: n, Namespace: testenv.Namespace, Selector: "app.kubernetes.io/name=bedrock-observer",
			ExchangePort: 8443, ProbeInterval: time.Second, SuspectInterval: time.Second, AskTimeout: time.Second,
		}, key, m, noAddrs{}, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
		o.check = r.check
		m.nodes[n] = o.Exchange()
		out[n] = o
		r.set(n, true)
	}
	return out, r, st
}

func ballot(t *testing.T, st *store.Store, node string) observe.Vote {
	b, err := st.ListBallots(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range b {
		if x.Node == node {
			return x.Votes[target]
		}
	}
	t.Fatalf("no ballots from %s", node)
	return observe.Vote{}
}

// roundAll runs two passes: an observer learns its peers' keys from the votes they published, so the
// first one to go in a pass cannot know the others yet.
func roundAll(obs map[string]*Observer) {
	for pass := 0; pass < 2; pass++ {
		for _, o := range obs {
			o.Round(context.Background())
		}
	}
}

func TestUpIsCastWithProof(t *testing.T) {
	obs, _, st := setup(t, "n3", "n4", "n5")
	roundAll(obs)
	v := ballot(t, st, "n3")
	if v.Ballot != observe.BallotUp || v.Proof.EchoNonce != v.Proof.Nonce || v.Proof.Nonce == "" {
		t.Fatalf("vote %+v", v)
	}
}

func TestBrokenPathAbstainsInsteadOfVotingDown(t *testing.T) {
	obs, r, st := setup(t, "n3", "n4", "n5")
	roundAll(obs)
	r.set("n3", false)
	obs["n3"].Round(context.Background())
	v := ballot(t, st, "n3")
	if v.Ballot != observe.BallotAbstain {
		t.Fatalf("n3 voted %s with peers seeing the primary up: %s", v.Ballot, v.Reason)
	}
}

func TestEveryoneDownIsAConfirmedDown(t *testing.T) {
	obs, r, st := setup(t, "n3", "n4", "n5")
	roundAll(obs)
	for n := range obs {
		r.set(n, false)
	}
	obs["n3"].Round(context.Background())
	if v := ballot(t, st, "n3"); v.Ballot != observe.BallotDown {
		t.Fatalf("got %s: %s", v.Ballot, v.Reason)
	}
}

func TestAloneMeansAbstain(t *testing.T) {
	obs, r, st := setup(t, "n3")
	obs["n3"].Round(context.Background())
	r.set("n3", false)
	obs["n3"].Round(context.Background())
	if v := ballot(t, st, "n3"); v.Ballot != observe.BallotAbstain {
		t.Fatalf("an observer with no peers voted %s", v.Ballot)
	}
}

func TestSequenceMovesEveryRound(t *testing.T) {
	obs, _, st := setup(t, "n3")
	obs["n3"].Round(context.Background())
	a := ballot(t, st, "n3").Seq
	obs["n3"].Round(context.Background())
	if b := ballot(t, st, "n3").Seq; b <= a {
		t.Fatalf("seq went from %d to %d", a, b)
	}
}

func TestUnknownTargetIsNeverProbed(t *testing.T) {
	obs, _, _ := setup(t, "n3")
	obs["n3"].Round(context.Background())
	p := obs["n3"].probe(context.Background(), "kube-system/etcd", "x")
	if p.Result != observe.ResultDown || p.Reason != "unknown target" {
		t.Fatalf("probed %+v", p)
	}
}

func TestObserversSignTheVerifiersChallenge(t *testing.T) {
	obs, _, st := setup(t, "n3")
	if err := st.PutRound(context.Background(), store.Round{Number: 9, Challenge: "round-9-challenge"}); err != nil {
		t.Fatal(err)
	}
	obs["n3"].Round(context.Background())
	v := ballot(t, st, "n3")
	if v.Challenge != "round-9-challenge" || v.Proof.Nonce != "round-9-challenge" {
		t.Fatalf("vote %+v did not use the round's challenge", v)
	}
}
