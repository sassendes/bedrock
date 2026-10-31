package observe

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// Age is measured on the sender's own clock, so no two clocks are ever compared.
type Report struct {
	Proof     Proof `json:"proof"`
	AgeMillis int64 `json:"ageMillis"`
}

type ProofStore struct {
	mu     sync.Mutex
	latest map[string]storedProof
	now    func() time.Time
}

type storedProof struct {
	proof Proof
	at    time.Time
}

func NewProofStore() *ProofStore {
	return &ProofStore{latest: map[string]storedProof{}, now: time.Now}
}

func (s *ProofStore) Record(p Proof) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.latest[p.Target] = storedProof{proof: p, at: s.now()}
}

func (s *ProofStore) Latest(target string) (Report, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sp, ok := s.latest[target]
	if !ok {
		return Report{}, false
	}
	return Report{Proof: sp.proof, AgeMillis: s.now().Sub(sp.at).Milliseconds()}, true
}

const proofPath = "/proof"

func (s *ProofStore) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(proofPath, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		target := r.URL.Query().Get("target")
		if target == "" {
			http.Error(w, "target required", http.StatusBadRequest)
			return
		}
		rep, ok := s.Latest(target)
		if !ok {
			http.Error(w, "no proof for target", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(rep)
	})
	return mux
}

type PeerAnswer struct {
	Peer   string
	Report *Report
	Err    error
}

const maxReportBytes = 64 << 10

func AskPeers(ctx context.Context, client *http.Client, peers []string, target string, timeout time.Duration) []PeerAnswer {
	answers := make([]PeerAnswer, len(peers))
	var wg sync.WaitGroup
	for i, peer := range peers {
		wg.Add(1)
		go func(i int, peer string) {
			defer wg.Done()
			rep, err := askPeer(ctx, client, peer, target, timeout)
			answers[i] = PeerAnswer{Peer: peer, Report: rep, Err: err}
		}(i, peer)
	}
	wg.Wait()
	return answers
}

func askPeer(ctx context.Context, client *http.Client, peer, target string, timeout time.Duration) (*Report, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	u := peer + proofPath + "?target=" + url.QueryEscape(target)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("peer answered %d", resp.StatusCode)
	}
	var rep Report
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxReportBytes)).Decode(&rep); err != nil {
		return nil, fmt.Errorf("bad report: %w", err)
	}
	return &rep, nil
}

type Verdict string

const (
	// another observer still sees the target up, so the problem is my own path
	VerdictOwnPathBroken Verdict = "own-path-broken"
	VerdictConfirmedDown Verdict = "confirmed-down"
	// no peer gave a usable answer, so abstain
	VerdictIsolated Verdict = "isolated"
)

type Judgement struct {
	Verdict Verdict
	Reason  string
	Valid   int
	Ignored int
}

// Judge: any trusted up wins, then any down, else isolated. It only judges this observer, never votes for a peer.
func Judge(target, expectedSystemID string, answers []PeerAnswer, maxAge time.Duration) Judgement {
	var j Judgement
	ups, downs := 0, 0
	for _, a := range answers {
		ok, up := usable(a, target, expectedSystemID, maxAge)
		if !ok {
			j.Ignored++
			continue
		}
		j.Valid++
		if up {
			ups++
		} else {
			downs++
		}
	}
	switch {
	case ups > 0:
		j.Verdict = VerdictOwnPathBroken
		j.Reason = fmt.Sprintf("%d peer(s) hold fresh proof the target is up, my own path is broken", ups)
	case downs > 0:
		j.Verdict = VerdictConfirmedDown
		j.Reason = fmt.Sprintf("%d peer(s) also fail to reach the target, none sees it up", downs)
	default:
		j.Verdict = VerdictIsolated
		j.Reason = "no peer gave a usable answer, abstaining"
	}
	return j
}

// An up only counts with full proof: echoed nonce, not in recovery, an LSN and the expected database.
func usable(a PeerAnswer, target, expectedSystemID string, maxAge time.Duration) (ok, up bool) {
	if a.Err != nil || a.Report == nil {
		return false, false
	}
	r := a.Report
	p := r.Proof
	if r.AgeMillis < 0 || time.Duration(r.AgeMillis)*time.Millisecond > maxAge {
		return false, false
	}
	if p.Target != target || p.Observer == "" {
		return false, false
	}
	switch p.Result {
	case ResultUp:
		if p.Nonce == "" || p.EchoNonce != p.Nonce || p.InRecovery || p.LSN == "" || p.SystemID == "" {
			return false, false
		}
		if expectedSystemID != "" && p.SystemID != expectedSystemID {
			return false, false
		}
		return true, true
	case ResultDown, ResultNotPrimary:
		return true, false
	}
	return false, false
}
