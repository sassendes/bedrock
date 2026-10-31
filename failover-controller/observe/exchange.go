package observe

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"
)

type Request struct {
	ID     string   `json:"id"`
	Origin string   `json:"origin"`
	Target string   `json:"target"`
	Nonce  string   `json:"nonce"`
	Path   []string `json:"path"`
	Sig    []byte   `json:"sig"`
}

type Answer struct {
	RequestID string   `json:"requestId"`
	Origin    string   `json:"origin"`
	Target    string   `json:"target"`
	Nonce     string   `json:"nonce"`
	Responder string   `json:"responder"`
	Proof     Proof    `json:"proof"`
	Path      []string `json:"path"`
	Relayed   bool     `json:"relayed"`
	Sig       []byte   `json:"sig"`
}

type Envelope struct {
	Request *Request `json:"request,omitempty"`
	Answer  *Answer  `json:"answer,omitempty"`
}

type Transport interface {
	Send(ctx context.Context, from, to string, env Envelope) error
}

type Prober interface {
	Probe(ctx context.Context, target, nonce string) Proof
}

type ProberFunc func(ctx context.Context, target, nonce string) Proof

func (f ProberFunc) Probe(ctx context.Context, target, nonce string) Proof {
	return f(ctx, target, nonce)
}

type Keyring map[string]ed25519.PublicKey

type VerifiedAnswer struct {
	Responder string
	Proof     Proof
	Path      []string
}

type Node struct {
	name      string
	key       ed25519.PrivateKey
	keys      Keyring
	peers     []string
	transport Transport
	prober    Prober
	maxHops   int
	seenTTL   time.Duration
	now       func() time.Time

	mu      sync.Mutex
	seen    map[string]seenRequest
	relayed map[string]time.Time
	pending map[string]*pending
}

type seenRequest struct {
	at   time.Time
	hops int
}

type pending struct {
	req     Request
	answers map[string]VerifiedAnswer
	want    int
	done    chan struct{}
	closed  bool
}

func NewNode(name string, key ed25519.PrivateKey, keys Keyring, peers []string, t Transport, p Prober, maxHops int) *Node {
	return &Node{
		name: name, key: key, keys: keys, peers: peers, transport: t, prober: p, maxHops: maxHops,
		seenTTL: time.Minute, now: time.Now,
		seen: map[string]seenRequest{}, relayed: map[string]time.Time{}, pending: map[string]*pending{},
	}
}

// Ask sends one signed request carrying a fresh nonce to every peer and collects signed answers until
// every peer answered or the timeout passes. Only answers that echo this exact nonce are kept.
func (n *Node) Ask(ctx context.Context, target string, timeout time.Duration) ([]VerifiedAnswer, error) {
	id, err := newNonce()
	if err != nil {
		return nil, err
	}
	nonce, err := newNonce()
	if err != nil {
		return nil, err
	}
	req := Request{ID: id, Origin: n.name, Target: target, Nonce: nonce, Path: []string{n.name}}
	req.Sig = ed25519.Sign(n.key, requestBytes(req))

	p := &pending{req: req, answers: map[string]VerifiedAnswer{}, want: len(n.peers), done: make(chan struct{})}
	n.mu.Lock()
	n.pending[id] = p
	n.seen[id] = seenRequest{at: n.now(), hops: 0}
	n.mu.Unlock()
	defer func() {
		n.mu.Lock()
		delete(n.pending, id)
		n.mu.Unlock()
	}()

	for _, peer := range n.peers {
		go n.send(ctx, peer, Envelope{Request: &req})
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-p.done:
	case <-timer.C:
	case <-ctx.Done():
	}

	n.mu.Lock()
	defer n.mu.Unlock()
	out := make([]VerifiedAnswer, 0, len(p.answers))
	for _, a := range p.answers {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Responder < out[j].Responder })
	return out, nil
}

func (n *Node) Handle(ctx context.Context, env Envelope) {
	switch {
	case env.Request != nil:
		n.handleRequest(ctx, *env.Request)
	case env.Answer != nil:
		n.handleAnswer(ctx, *env.Answer)
	}
}

// A request is probed and answered at most once per node. A later copy that used fewer hops is
// forwarded again with its larger remaining budget, never past maxHops.
func (n *Node) handleRequest(ctx context.Context, r Request) {
	pub, ok := n.keys[r.Origin]
	if !ok || !ed25519.Verify(pub, requestBytes(r), r.Sig) {
		return
	}
	if r.Origin == n.name || len(r.Path) == 0 || r.Path[0] != r.Origin || contains(r.Path, n.name) {
		return
	}
	n.mu.Lock()
	n.prune()
	prev, done := n.seen[r.ID]
	if done && len(r.Path) >= prev.hops {
		n.mu.Unlock()
		return
	}
	n.seen[r.ID] = seenRequest{at: n.now(), hops: len(r.Path)}
	n.mu.Unlock()

	if len(r.Path) < n.maxHops {
		fwd := r
		fwd.Path = append(append([]string{}, r.Path...), n.name)
		for _, peer := range n.peers {
			if !contains(fwd.Path, peer) {
				go n.send(ctx, peer, Envelope{Request: &fwd})
			}
		}
	}

	if done {
		return
	}
	proof := n.prober.Probe(ctx, r.Target, r.Nonce)
	proof.Observer, proof.Target, proof.Nonce = n.name, r.Target, r.Nonce
	a := Answer{RequestID: r.ID, Origin: r.Origin, Target: r.Target, Nonce: r.Nonce, Responder: n.name, Proof: proof, Path: r.Path}
	a.Sig = ed25519.Sign(n.key, answerBytes(a))
	n.route(ctx, a)
}

func (n *Node) handleAnswer(ctx context.Context, a Answer) {
	pub, ok := n.keys[a.Responder]
	if !ok || !ed25519.Verify(pub, answerBytes(a), a.Sig) {
		return
	}
	if a.Origin == n.name {
		n.accept(a)
		return
	}
	key := a.RequestID + "/" + a.Responder
	n.mu.Lock()
	n.prune()
	if _, done := n.relayed[key]; done {
		n.mu.Unlock()
		return
	}
	n.relayed[key] = n.now()
	n.mu.Unlock()
	a.Relayed = true
	n.route(ctx, a)
}

func (n *Node) accept(a Answer) {
	n.mu.Lock()
	defer n.mu.Unlock()
	p, ok := n.pending[a.RequestID]
	if !ok || p.closed {
		return
	}
	r := p.req
	if a.Nonce != r.Nonce || a.Target != r.Target || a.Proof.Nonce != r.Nonce || a.Proof.Observer != a.Responder || a.Responder == n.name {
		return
	}
	if _, dup := p.answers[a.Responder]; dup {
		return
	}
	p.answers[a.Responder] = VerifiedAnswer{Responder: a.Responder, Proof: a.Proof, Path: a.Path}
	if len(p.answers) >= p.want {
		p.closed = true
		close(p.done)
	}
}

// Answers go straight to the origin. If that fails, the responder hands its answer to every peer and
// a relay only steps one hop back along the request's path, so nothing floods.
func (n *Node) route(ctx context.Context, a Answer) {
	if n.send(ctx, a.Origin, Envelope{Answer: &a}) == nil {
		return
	}
	if !a.Relayed {
		for _, peer := range n.peers {
			if peer != a.Origin {
				n.send(ctx, peer, Envelope{Answer: &a})
			}
		}
		return
	}
	if prev := previousHop(a.Path, n.name); prev != "" && prev != a.Origin {
		n.send(ctx, prev, Envelope{Answer: &a})
	}
}

func (n *Node) send(ctx context.Context, to string, env Envelope) error {
	return n.transport.Send(ctx, n.name, to, env)
}

func (n *Node) prune() {
	cut := n.now().Add(-n.seenTTL)
	for k, s := range n.seen {
		if s.at.Before(cut) {
			delete(n.seen, k)
		}
	}
	for k, t := range n.relayed {
		if t.Before(cut) {
			delete(n.relayed, k)
		}
	}
}

func previousHop(path []string, self string) string {
	for i, p := range path {
		if p == self {
			if i > 0 {
				return path[i-1]
			}
			return ""
		}
	}
	if len(path) > 0 {
		return path[len(path)-1]
	}
	return ""
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func requestBytes(r Request) []byte {
	b, _ := json.Marshal(struct{ ID, Origin, Target, Nonce string }{r.ID, r.Origin, r.Target, r.Nonce})
	return b
}

func answerBytes(a Answer) []byte {
	b, _ := json.Marshal(struct {
		RequestID, Origin, Target, Nonce, Responder string
		Proof                                       Proof
	}{a.RequestID, a.Origin, a.Target, a.Nonce, a.Responder, a.Proof})
	return b
}

type Verdict string

const (
	// another observer saw the target up with my own nonce echoed: my own path is broken. Abstain.
	VerdictOwnPathBroken Verdict = "own-path-broken"
	VerdictConfirmedDown Verdict = "confirmed-down"
	// no peer answered at all: I cannot judge anything. Abstain.
	VerdictIsolated Verdict = "isolated"
)

type Judgement struct {
	Verdict Verdict
	Reason  string
	Ups     int
	Downs   int
}

// Judge only ever sees answers that were signed by their responder and echo this request's nonce.
func Judge(expectedSystemID string, answers []VerifiedAnswer) Judgement {
	var j Judgement
	for _, a := range answers {
		p := a.Proof
		up := p.Result == ResultUp && p.EchoNonce == p.Nonce && !p.InRecovery && p.LSN != "" && p.SystemID != "" &&
			(expectedSystemID == "" || p.SystemID == expectedSystemID)
		if up {
			j.Ups++
		} else {
			j.Downs++
		}
	}
	switch {
	case j.Ups > 0:
		j.Verdict = VerdictOwnPathBroken
		j.Reason = fmt.Sprintf("%d peer(s) reached the target and echoed my nonce", j.Ups)
	case j.Downs > 0:
		j.Verdict = VerdictConfirmedDown
		j.Reason = fmt.Sprintf("%d peer(s) also fail to reach the target, none sees it up", j.Downs)
	default:
		j.Verdict = VerdictIsolated
		j.Reason = "no signed answer reached me"
	}
	return j
}
