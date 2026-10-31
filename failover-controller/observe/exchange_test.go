package observe

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const sysID = "7702807239039781397"

type link struct{ from, to string }

type mesh struct {
	mu      sync.Mutex
	nodes   map[string]*Node
	blocked map[link]bool
	delay   map[link]time.Duration
	tamper  func(from, to string, env *Envelope)
	flight  atomic.Int64
	sent    atomic.Int64
}

func (m *mesh) Send(ctx context.Context, from, to string, env Envelope) error {
	m.mu.Lock()
	n, ok := m.nodes[to]
	blocked := m.blocked[link{from, to}]
	wait := m.delay[link{from, to}]
	tamper := m.tamper
	m.mu.Unlock()
	if !ok || blocked {
		return errors.New("unreachable")
	}
	m.sent.Add(1)
	if tamper != nil {
		tamper(from, to, &env)
	}
	m.flight.Add(1)
	go func() {
		defer m.flight.Add(-1)
		time.Sleep(wait)
		n.Handle(ctx, env)
	}()
	return nil
}

func (m *mesh) settle() {
	last := int64(-1)
	for {
		if m.flight.Load() == 0 && m.sent.Load() == last {
			return
		}
		last = m.sent.Load()
		time.Sleep(20 * time.Millisecond)
	}
}

func (m *mesh) cut(a, b string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.blocked[link{a, b}] = true
	m.blocked[link{b, a}] = true
}

func (m *mesh) cutOneWay(from, to string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.blocked[link{from, to}] = true
}

type cluster struct {
	mesh   *mesh
	nodes  map[string]*Node
	keys   map[string]ed25519.PrivateKey
	probes map[string]*atomic.Int64
}

func newCluster(t *testing.T, sees map[string]Result, maxHops int) *cluster {
	t.Helper()
	names := make([]string, 0, len(sees))
	for name := range sees {
		names = append(names, name)
	}
	sort.Strings(names)
	ring := Keyring{}
	privs := map[string]ed25519.PrivateKey{}
	for _, name := range names {
		pub, priv, err := ed25519.GenerateKey(nil)
		if err != nil {
			t.Fatal(err)
		}
		ring[name], privs[name] = pub, priv
	}
	c := &cluster{mesh: &mesh{nodes: map[string]*Node{}, blocked: map[link]bool{}, delay: map[link]time.Duration{}}, nodes: map[string]*Node{}, keys: privs, probes: map[string]*atomic.Int64{}}
	for _, name := range names {
		var peers []string
		for _, other := range names {
			if other != name {
				peers = append(peers, other)
			}
		}
		count := &atomic.Int64{}
		c.probes[name] = count
		result := sees[name]
		prober := ProberFunc(func(ctx context.Context, target, nonce string) Proof {
			count.Add(1)
			if result == ResultUp {
				return Proof{Result: ResultUp, EchoNonce: nonce, LSN: "0/3000000", SystemID: sysID, Timeline: 1}
			}
			return Proof{Result: result, Reason: "cannot reach"}
		})
		n := NewNode(name, privs[name], ring, peers, c.mesh, prober, maxHops)
		c.nodes[name] = n
		c.mesh.nodes[name] = n
	}
	return c
}

func (c *cluster) ask(t *testing.T, origin string) []VerifiedAnswer {
	t.Helper()
	answers, err := c.nodes[origin].Ask(context.Background(), "cluster-1/cluster-1", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	c.mesh.settle()
	return answers
}

func responders(answers []VerifiedAnswer) []string {
	var out []string
	for _, a := range answers {
		out = append(out, a.Responder)
	}
	return out
}

func five(a, b, c, d, e Result) map[string]Result {
	return map[string]Result{"a": a, "b": b, "c": c, "d": d, "e": e}
}

func TestEveryoneAnswersDirectly(t *testing.T) {
	c := newCluster(t, five(ResultDown, ResultUp, ResultDown, ResultDown, ResultDown), 2)
	answers := c.ask(t, "a")
	if fmt.Sprint(responders(answers)) != "[b c d e]" {
		t.Fatalf("got %v", responders(answers))
	}
	if j := Judge(sysID, answers); j.Verdict != VerdictOwnPathBroken || j.Ups != 1 || j.Downs != 3 {
		t.Fatalf("got %+v", j)
	}
}

func TestAllPeersDownConfirmsDown(t *testing.T) {
	c := newCluster(t, five(ResultDown, ResultDown, ResultNotPrimary, ResultDown, ResultDown), 2)
	if j := Judge(sysID, c.ask(t, "a")); j.Verdict != VerdictConfirmedDown || j.Downs != 4 {
		t.Fatalf("got %+v", j)
	}
}

func TestRelayReachesAPeerTheOriginCannot(t *testing.T) {
	c := newCluster(t, five(ResultDown, ResultDown, ResultUp, ResultDown, ResultDown), 2)
	c.mesh.cut("a", "c")
	answers := c.ask(t, "a")
	var fromC *VerifiedAnswer
	for i := range answers {
		if answers[i].Responder == "c" {
			fromC = &answers[i]
		}
	}
	if fromC == nil {
		t.Fatalf("c's answer never reached a: %v", responders(answers))
	}
	if len(fromC.Path) != 2 || fromC.Path[0] != "a" {
		t.Errorf("c's answer should carry the relay path a -> x -> c, got %v", fromC.Path)
	}
	if j := Judge(sysID, answers); j.Verdict != VerdictOwnPathBroken {
		t.Fatalf("got %+v", j)
	}
}

func TestAnswerComesBackThroughAPeerWhenTheDirectWayIsCut(t *testing.T) {
	c := newCluster(t, five(ResultDown, ResultDown, ResultUp, ResultDown, ResultDown), 2)
	c.mesh.cutOneWay("c", "a")
	answers := c.ask(t, "a")
	if fmt.Sprint(responders(answers)) != "[b c d e]" {
		t.Fatalf("c could hear a but not answer it directly, its answer should still arrive: %v", responders(answers))
	}
}

func TestFullyCutOffOriginIsIsolated(t *testing.T) {
	c := newCluster(t, five(ResultDown, ResultUp, ResultUp, ResultUp, ResultUp), 2)
	for _, p := range []string{"b", "c", "d", "e"} {
		c.mesh.cut("a", p)
	}
	answers := c.ask(t, "a")
	if j := Judge(sysID, answers); len(answers) != 0 || j.Verdict != VerdictIsolated {
		t.Fatalf("got %v %+v", responders(answers), j)
	}
}

func TestRelayCannotRewriteAnAnswer(t *testing.T) {
	c := newCluster(t, five(ResultDown, ResultDown, ResultUp, ResultDown, ResultDown), 2)
	c.mesh.cut("a", "c")
	c.mesh.tamper = func(from, to string, env *Envelope) {
		if env.Answer != nil && env.Answer.Responder == "c" && from != "c" {
			forged := *env.Answer
			forged.Proof.Result = ResultDown
			env.Answer = &forged
		}
	}
	answers := c.ask(t, "a")
	for _, a := range answers {
		if a.Responder == "c" {
			t.Fatalf("a rewritten answer from c was accepted: %+v", a)
		}
	}
}

func TestOutsiderCannotInjectAnUp(t *testing.T) {
	c := newCluster(t, five(ResultDown, ResultDown, ResultDown, ResultDown, ResultDown), 2)
	_, stranger, _ := ed25519.GenerateKey(nil)
	var once sync.Once
	c.mesh.tamper = func(from, to string, env *Envelope) {
		if to != "a" || env.Answer == nil {
			return
		}
		once.Do(func() {
			fake := *env.Answer
			fake.Responder = "b"
			fake.Proof = Proof{Observer: "b", Target: fake.Target, Result: ResultUp, Nonce: fake.Nonce, EchoNonce: fake.Nonce, LSN: "0/9", SystemID: sysID}
			fake.Sig = ed25519.Sign(stranger, answerBytes(fake))
			go c.nodes["a"].Handle(context.Background(), Envelope{Answer: &fake})
		})
	}
	answers := c.ask(t, "a")
	if j := Judge(sysID, answers); j.Ups != 0 {
		t.Fatalf("an up signed by an unknown key was accepted: %+v", j)
	}
}

func TestReplayedAnswerFromAnOldRequestIsRejected(t *testing.T) {
	c := newCluster(t, five(ResultDown, ResultUp, ResultDown, ResultDown, ResultDown), 1)
	var mu sync.Mutex
	var captured *Answer
	c.mesh.tamper = func(from, to string, env *Envelope) {
		if env.Answer != nil && env.Answer.Responder == "b" {
			mu.Lock()
			if captured == nil {
				cp := *env.Answer
				captured = &cp
			}
			mu.Unlock()
		}
	}
	c.ask(t, "a")
	c.nodes["b"].prober = ProberFunc(func(ctx context.Context, target, nonce string) Proof {
		return Proof{Result: ResultDown}
	})
	c.mesh.tamper = func(from, to string, env *Envelope) {
		if env.Request != nil && to == "b" {
			mu.Lock()
			old := *captured
			mu.Unlock()
			old.RequestID = env.Request.ID
			go c.nodes["a"].Handle(context.Background(), Envelope{Answer: &old})
		}
	}
	answers := c.ask(t, "a")
	if j := Judge(sysID, answers); j.Ups != 0 {
		t.Fatalf("an old signed up was replayed into a new request: %+v", j)
	}
}

func TestForgedRequestProbesNothing(t *testing.T) {
	c := newCluster(t, five(ResultDown, ResultUp, ResultUp, ResultUp, ResultUp), 2)
	_, stranger, _ := ed25519.GenerateKey(nil)
	req := Request{ID: "x", Origin: "a", Target: "cluster-1/cluster-1", Nonce: "n", Path: []string{"a"}}
	req.Sig = ed25519.Sign(stranger, requestBytes(req))
	for _, p := range []string{"b", "c", "d", "e"} {
		c.nodes[p].Handle(context.Background(), Envelope{Request: &req})
	}
	c.mesh.settle()
	for name, n := range c.probes {
		if n.Load() != 0 {
			t.Fatalf("%s probed the primary for a forged request", name)
		}
	}
}

func TestEachObserverProbesOncePerRequestAndTrafficIsBounded(t *testing.T) {
	c := newCluster(t, five(ResultDown, ResultUp, ResultUp, ResultUp, ResultUp), 3)
	c.ask(t, "a")
	for name, n := range c.probes {
		want := int64(1)
		if name == "a" {
			want = 0
		}
		if got := n.Load(); got != want {
			t.Errorf("%s probed %d times, want %d", name, got, want)
		}
	}
	if sent := c.mesh.sent.Load(); sent > 5*5*5 {
		t.Fatalf("%d messages for one request among 5 observers", sent)
	}
}

func TestHopLimitStopsForwarding(t *testing.T) {
	c := newCluster(t, five(ResultDown, ResultUp, ResultUp, ResultUp, ResultUp), 2)
	chain := []string{"a", "b", "c", "d", "e"}
	for i, x := range chain {
		for j, y := range chain {
			if j > i+1 {
				c.mesh.cut(x, y)
			}
		}
	}
	got := fmt.Sprint(responders(c.ask(t, "a")))
	if got != "[b c]" {
		t.Fatalf("on a chain a-b-c-d-e with 2 hops a should hear only b and c, got %s", got)
	}
}

func TestAnswerFindsTheOnlyPeerThatCanReachTheOrigin(t *testing.T) {
	c := newCluster(t, map[string]Result{"a": ResultDown, "b": ResultUp, "c": ResultDown, "d": ResultDown}, 2)
	allowed := map[link]bool{{"a", "b"}: true, {"b", "c"}: true, {"b", "d"}: true, {"d", "a"}: true}
	for _, x := range []string{"a", "b", "c", "d"} {
		for _, y := range []string{"a", "b", "c", "d"} {
			if x != y && !allowed[link{x, y}] {
				c.mesh.cutOneWay(x, y)
			}
		}
	}
	for i := 0; i < 3; i++ {
		answers := c.ask(t, "a")
		if j := Judge(sysID, answers); j.Ups != 1 {
			t.Fatalf("run %d: b's up must reach a through d, got %v %+v", i, responders(answers), j)
		}
	}
}

func TestResponderFansOutEvenWhenTheLadderLeadsNowhere(t *testing.T) {
	c := newCluster(t, map[string]Result{"a": ResultDown, "b": ResultUp, "c": ResultDown, "d": ResultDown}, 2)
	allowed := map[link]bool{{"a", "c"}: true, {"c", "b"}: true, {"b", "c"}: true, {"b", "d"}: true, {"d", "a"}: true}
	for _, x := range []string{"a", "b", "c", "d"} {
		for _, y := range []string{"a", "b", "c", "d"} {
			if x != y && !allowed[link{x, y}] {
				c.mesh.cutOneWay(x, y)
			}
		}
	}
	answers := c.ask(t, "a")
	if j := Judge(sysID, answers); j.Ups != 1 {
		t.Fatalf("b's up must reach a through d, got %v %+v", responders(answers), j)
	}
}

func TestShorterLateCopyStillGetsForwarded(t *testing.T) {
	c := newCluster(t, map[string]Result{"a": ResultDown, "b": ResultDown, "c": ResultUp, "d": ResultDown}, 2)
	allowed := map[link]bool{{"a", "b"}: true, {"a", "d"}: true, {"b", "a"}: true, {"b", "c"}: true, {"c", "a"}: true, {"d", "a"}: true, {"d", "b"}: true}
	for _, x := range []string{"a", "b", "c", "d"} {
		for _, y := range []string{"a", "b", "c", "d"} {
			if x != y && !allowed[link{x, y}] {
				c.mesh.cutOneWay(x, y)
			}
		}
	}
	c.mesh.delay[link{"a", "b"}] = 200 * time.Millisecond
	answers := c.ask(t, "a")
	if j := Judge(sysID, answers); j.Ups != 1 {
		t.Fatalf("c is two hops away through b and must be heard even when d's longer copy reaches b first, got %v %+v", responders(answers), j)
	}
}
