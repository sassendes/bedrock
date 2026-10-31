package observe

import "testing"

func goodUp() Proof {
	return Proof{Observer: "a", Target: "t", Result: ResultUp, Nonce: "n1", EchoNonce: "n1", LSN: "0/1", SystemID: "42", Timeline: 1}
}

func TestValidNeedsEveryPieceOfProof(t *testing.T) {
	if !Valid(goodUp(), "42") {
		t.Fatal("good proof rejected")
	}
	broken := map[string]func(*Proof){
		"no echo":        func(p *Proof) { p.EchoNonce = "" },
		"wrong echo":     func(p *Proof) { p.EchoNonce = "n2" },
		"no nonce":       func(p *Proof) { p.Nonce, p.EchoNonce = "", "" },
		"in recovery":    func(p *Proof) { p.InRecovery = true },
		"no lsn":         func(p *Proof) { p.LSN = "" },
		"no system":      func(p *Proof) { p.SystemID = "" },
		"wrong system":   func(p *Proof) { p.SystemID = "43" },
		"result is down": func(p *Proof) { p.Result = ResultDown },
	}
	for name, f := range broken {
		p := goodUp()
		f(&p)
		if Valid(p, "42") {
			t.Fatalf("%s passed as up", name)
		}
	}
}

func TestCastNeverTurnsDoubtIntoDown(t *testing.T) {
	down := Proof{Result: ResultDown, Reason: "connect: refused"}
	cases := []struct {
		j    *Judgement
		want Ballot
	}{
		{nil, BallotAbstain},
		{&Judgement{Verdict: VerdictOwnPathBroken}, BallotAbstain},
		{&Judgement{Verdict: VerdictIsolated}, BallotAbstain},
		{&Judgement{Verdict: VerdictConfirmedDown}, BallotDown},
	}
	for _, c := range cases {
		if v := Cast("t", "42", down, c.j); v.Ballot != c.want {
			t.Fatalf("judgement %+v cast %s, want %s", c.j, v.Ballot, c.want)
		}
	}
	if v := Cast("t", "42", goodUp(), nil); v.Ballot != BallotUp {
		t.Fatalf("good up cast %s", v.Ballot)
	}
	bad := goodUp()
	bad.EchoNonce = "stale"
	if v := Cast("t", "42", bad, nil); v.Ballot != BallotAbstain {
		t.Fatalf("up without echo cast %s", v.Ballot)
	}
}
