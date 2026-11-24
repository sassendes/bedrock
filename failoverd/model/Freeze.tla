---- MODULE Freeze ----
EXTENDS Naturals

CONSTANTS MaxSteps, GFreeze, GTestsFirst

VARIABLES runOpen, playbook, testsPassed, steps, log

vars == <<runOpen, playbook, testsPassed, steps, log>>

Books == {"bootstrap", "rotateKey", "addReplica"}

Init ==
    /\ runOpen = FALSE
    /\ playbook = "idle"
    /\ testsPassed = FALSE
    /\ steps = 0
    /\ log = {}

OpenRun ==
    /\ ~runOpen
    /\ runOpen' = TRUE
    /\ UNCHANGED <<playbook, testsPassed, steps, log>>

CloseRun ==
    /\ runOpen
    /\ runOpen' = FALSE
    /\ UNCHANGED <<playbook, testsPassed, steps, log>>

Tests ==
    /\ testsPassed' = ~testsPassed
    /\ UNCHANGED <<runOpen, playbook, steps, log>>

StartBook ==
    /\ steps < MaxSteps
    /\ playbook = "idle"
    /\ \E b \in Books :
         /\ (GFreeze => ~runOpen)
         /\ (GTestsFirst /\ b = "addReplica") => testsPassed
         /\ playbook' = b
         /\ log' = log \cup {[k |-> "start", b |-> b, duringRun |-> runOpen, tests |-> testsPassed]}
    /\ steps' = steps + 1
    /\ UNCHANGED <<runOpen, testsPassed>>

BookStep ==
    /\ playbook # "idle"
    /\ (GFreeze => ~runOpen)
    /\ log' = log \cup {[k |-> "step", b |-> playbook, duringRun |-> runOpen, tests |-> testsPassed]}
    /\ UNCHANGED <<runOpen, playbook, testsPassed, steps>>

FinishBook ==
    /\ playbook # "idle"
    /\ playbook' = "idle"
    /\ UNCHANGED <<runOpen, testsPassed, steps, log>>

Next == OpenRun \/ CloseRun \/ Tests \/ StartBook \/ BookStep \/ FinishBook

Spec == Init /\ [][Next]_vars

NoPlaybookStepDuringARun == \A e \in log : ~e.duringRun
ReplicasOnlyAfterTests == \A e \in log : (e.k = "start" /\ e.b = "addReplica") => e.tests

CanaryNeverRunsABook == log = {}
====
