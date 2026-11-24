---- MODULE Runs ----
EXTENDS Naturals, Sequences

CONSTANTS MaxT, Cooldown, Cap, Window, StepCeiling, MaxFailures, GOneRun, GCooldown, GCap, GCeiling

VARIABLES now, runs, open, stepAge, failures, lastDone, history, paged

vars == <<now, runs, open, stepAge, failures, lastDone, history, paged>>

Init ==
    /\ now = 0
    /\ runs = 0
    /\ open = 0
    /\ stepAge = 0
    /\ failures = 0
    /\ lastDone = 0
    /\ history = <<>>
    /\ paged = FALSE

Tick ==
    /\ now < MaxT
    /\ (GCeiling => ~(open > 0 /\ stepAge >= StepCeiling))
    /\ now' = now + 1
    /\ stepAge' = IF open > 0 THEN stepAge + 1 ELSE 0
    /\ UNCHANGED <<runs, open, failures, lastDone, history, paged>>

Fail ==
    /\ failures < MaxFailures
    /\ failures' = failures + 1
    /\ UNCHANGED <<now, runs, open, stepAge, lastDone, history, paged>>

RunsInWindow == runs

Allowed ==
    /\ (GOneRun => open = 0)
    /\ (GCooldown => (runs = 0 \/ now >= lastDone + Cooldown))
    /\ (GCap => RunsInWindow < Cap \/ now >= Window)

OpenRun ==
    /\ failures > runs
    /\ Allowed
    /\ open' = open + 1
    /\ runs' = runs + 1
    /\ stepAge' = 0
    /\ history' = history \o <<now>>
    /\ UNCHANGED <<now, failures, lastDone, paged>>

HoldAndPage ==
    /\ failures > runs
    /\ ~Allowed
    /\ ~paged
    /\ paged' = TRUE
    /\ UNCHANGED <<now, runs, open, stepAge, failures, lastDone, history>>

StepDone ==
    /\ open > 0
    /\ open' = open - 1
    /\ lastDone' = now
    /\ stepAge' = 0
    /\ UNCHANGED <<now, runs, failures, history, paged>>

StepTimeout ==
    /\ GCeiling
    /\ open > 0
    /\ stepAge >= StepCeiling
    /\ open' = open - 1
    /\ paged' = TRUE
    /\ lastDone' = now
    /\ stepAge' = 0
    /\ UNCHANGED <<now, runs, failures, history>>

Next == Tick \/ Fail \/ OpenRun \/ HoldAndPage \/ StepDone \/ StepTimeout

Spec == Init /\ [][Next]_vars

OneRunAtATime == open <= 1
NoWaitPastCeiling == stepAge <= StepCeiling
RunsRespectCooldown ==
    \A i \in 1..(Len(history) - 1) : history[i + 1] >= history[i] + Cooldown

CapHolds == Len(SelectSeq(history, LAMBDA x : x < Window)) <= Cap

CanaryNeverRuns == runs = 0
CanaryNeverHolds == ~paged
====
