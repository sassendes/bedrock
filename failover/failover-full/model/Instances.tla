---- MODULE Instances ----
EXTENDS Naturals, FiniteSets

CONSTANTS MaxRounds, SplitPossible, NeedTwoRounds

Inst == {"i1", "i2"}

VARIABLES role, timeline, round, sigs, lastRoundSigs, splitPaged, switchovers, split

vars == <<role, timeline, round, sigs, lastRoundSigs, splitPaged, switchovers, split>>

Init ==
    /\ role = [i \in Inst |-> IF i = "i1" THEN "primary" ELSE "replica"]
    /\ timeline = [i \in Inst |-> 1]
    /\ round = 1
    /\ sigs = {}
    /\ lastRoundSigs = {}
    /\ splitPaged = FALSE
    /\ switchovers = 0
    /\ split = FALSE

Primaries == {i \in Inst : role[i] = "primary"}

Sign(i) ==
    /\ [inst |-> i, role |-> role[i], tl |-> timeline[i]] \notin sigs
    /\ sigs' = sigs \cup {[inst |-> i, role |-> role[i], tl |-> timeline[i]]}
    /\ UNCHANGED <<role, timeline, round, lastRoundSigs, splitPaged, switchovers, split>>

Switchover ==
    /\ switchovers < 1 /\ ~split
    /\ \E old \in Primaries, new \in Inst \ Primaries :
         /\ role' = [role EXCEPT ![old] = "replica", ![new] = "primary"]
         /\ timeline' = [timeline EXCEPT ![new] = timeline[old] + 1, ![old] = timeline[old] + 1]
    /\ switchovers' = switchovers + 1
    /\ UNCHANGED <<round, sigs, lastRoundSigs, splitPaged, split>>

SplitBrain ==
    /\ SplitPossible /\ ~split /\ Cardinality(Primaries) = 1
    /\ \E new \in Inst \ Primaries :
         /\ role' = [role EXCEPT ![new] = "primary"]
         /\ timeline' = [timeline EXCEPT ![new] = @ + 1]
    /\ split' = TRUE
    /\ UNCHANGED <<round, sigs, lastRoundSigs, splitPaged, switchovers>>

PrimarySigners(S) == {x.inst : x \in {y \in S : y.role = "primary"}}

TwoPrimaries(S) == Cardinality(PrimarySigners(S)) > 1

EndRound ==
    /\ round < MaxRounds
    /\ splitPaged' = (splitPaged \/ (TwoPrimaries(sigs) /\ (~NeedTwoRounds \/ TwoPrimaries(lastRoundSigs))))
    /\ lastRoundSigs' = sigs
    /\ sigs' = {}
    /\ round' = round + 1
    /\ UNCHANGED <<role, timeline, switchovers, split>>

Next == EndRound \/ Switchover \/ SplitBrain \/ \E i \in Inst : Sign(i)

Spec == Init /\ [][Next]_vars

NoFalseSplitAlarmOnASwitchover == splitPaged => split

====
