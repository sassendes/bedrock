---- MODULE Halt ----
EXTENDS Naturals

CONSTANTS MaxFlips, CheckerHalts, StaleGuard

VARIABLES dbUp, flips, approval, verifierAlive, checkerSawUp, halt, pc, badFence

vars == <<dbUp, flips, approval, verifierAlive, checkerSawUp, halt, pc, badFence>>

Init ==
    /\ dbUp = FALSE
    /\ flips = 0
    /\ approval = "none"
    /\ verifierAlive = TRUE
    /\ checkerSawUp = FALSE
    /\ halt = FALSE
    /\ pc = "claimed"
    /\ badFence = FALSE

FallbackActive == ~verifierAlive

DBFlips ==
    /\ flips < MaxFlips
    /\ dbUp' = ~dbUp
    /\ flips' = flips + 1
    /\ checkerSawUp' = FALSE
    /\ UNCHANGED <<approval, verifierAlive, halt, pc, badFence>>

Approve ==
    /\ verifierAlive /\ ~dbUp /\ approval = "none" /\ ~halt
    /\ approval' = "valid"
    /\ UNCHANGED <<dbUp, flips, verifierAlive, checkerSawUp, halt, pc, badFence>>

Revoke ==
    /\ verifierAlive /\ dbUp /\ approval = "valid" /\ pc = "claimed"
    /\ approval' = "revoked"
    /\ UNCHANGED <<dbUp, flips, verifierAlive, checkerSawUp, halt, pc, badFence>>

VerifierStalls ==
    /\ verifierAlive
    /\ verifierAlive' = FALSE
    /\ UNCHANGED <<dbUp, flips, approval, checkerSawUp, halt, pc, badFence>>

CheckerLooks ==
    /\ FallbackActive
    /\ checkerSawUp' = dbUp
    /\ halt' = (CheckerHalts /\ dbUp /\ pc = "claimed")
    /\ UNCHANGED <<dbUp, flips, approval, verifierAlive, pc, badFence>>

CheckerSleeps ==
    /\ ~FallbackActive /\ halt
    /\ halt' = FALSE
    /\ UNCHANGED <<dbUp, flips, approval, verifierAlive, checkerSawUp, pc, badFence>>

Admitted == approval = "valid" /\ ~halt

Fence ==
    /\ pc = "claimed" /\ Admitted
    /\ (~StaleGuard \/ verifierAlive)
    /\ pc' = "fenced"
    /\ badFence' = (badFence \/ (dbUp /\ checkerSawUp))
    /\ UNCHANGED <<dbUp, flips, approval, verifierAlive, checkerSawUp, halt>>

Promote ==
    /\ pc = "fenced" /\ Admitted
    /\ pc' = "promoted"
    /\ UNCHANGED <<dbUp, flips, approval, verifierAlive, checkerSawUp, halt, badFence>>

Next == DBFlips \/ Approve \/ Revoke \/ VerifierStalls \/ CheckerLooks \/ CheckerSleeps \/ Fence \/ Promote

Spec == Init /\ [][Next]_vars

NeverFenceWhatTheCheckerSawUp == ~badFence

HaltStopsEverything == halt => ~ENABLED (Fence \/ Promote)

CanaryHalts == ~halt
CanaryPromotes == pc # "promoted"
CanaryPromotesWithVerifierDead == ~(pc = "promoted" /\ ~verifierAlive)
====
