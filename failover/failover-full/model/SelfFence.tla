---- MODULE SelfFence ----
EXTENDS Naturals

CONSTANTS MaxRestarts, MaxCuts, GSidecarKill, GLeaseGate, GFenceAll, GGraceOneDelete, GVerifyNodeReady

VARIABLES apiCut, obsCut, pg, podObj, restarts, cuts, annotated, deleted, verified, promoted, nodeFlapped, killedInCut, downAfterFence, cleanAtVerify

vars == <<apiCut, obsCut, pg, podObj, restarts, cuts, annotated, deleted, verified, promoted, nodeFlapped, killedInCut, downAfterFence, cleanAtVerify>>

Init ==
    /\ apiCut = FALSE
    /\ obsCut = FALSE
    /\ pg = "up"
    /\ podObj = TRUE
    /\ restarts = 0
    /\ cuts = 0
    /\ annotated = FALSE
    /\ deleted = FALSE
    /\ verified = FALSE
    /\ promoted = FALSE
    /\ nodeFlapped = FALSE
    /\ killedInCut = FALSE
    /\ downAfterFence = FALSE
    /\ cleanAtVerify = FALSE

Cut ==
    /\ cuts < MaxCuts
    /\ cuts' = cuts + 1
    /\ \/ /\ ~apiCut /\ apiCut' = TRUE /\ obsCut' = TRUE /\ nodeFlapped' = TRUE
       \/ /\ ~obsCut /\ obsCut' = TRUE /\ UNCHANGED <<apiCut, nodeFlapped>>
    /\ UNCHANGED <<pg, podObj, restarts, annotated, deleted, verified, promoted, killedInCut, downAfterFence, cleanAtVerify>>

Heal ==
    /\ apiCut \/ obsCut
    /\ apiCut' = FALSE
    /\ obsCut' = FALSE
    /\ killedInCut' = FALSE
    /\ UNCHANGED <<pg, podObj, restarts, cuts, annotated, deleted, verified, promoted, nodeFlapped, downAfterFence, cleanAtVerify>>

SidecarKill ==
    /\ GSidecarKill
    /\ obsCut
    /\ pg = "up"
    /\ pg' = "stopped"
    /\ killedInCut' = (killedInCut \/ apiCut)
    /\ downAfterFence' = (downAfterFence \/ annotated)
    /\ UNCHANGED <<apiCut, obsCut, podObj, restarts, cuts, annotated, deleted, verified, promoted, nodeFlapped, cleanAtVerify>>

StartsAgain ==
    /\ ~(GLeaseGate /\ apiCut)
    /\ ~(GFenceAll /\ annotated /\ ~apiCut)

KubeletRestart ==
    /\ pg = "stopped"
    /\ podObj \/ apiCut
    /\ restarts < MaxRestarts
    /\ restarts' = restarts + 1
    /\ pg' = IF StartsAgain THEN "up" ELSE "stopped"
    /\ UNCHANGED <<apiCut, obsCut, podObj, cuts, annotated, deleted, verified, promoted, nodeFlapped, cleanAtVerify>>
    /\ killedInCut' = (killedInCut /\ apiCut)
    /\ downAfterFence' = (downAfterFence \/ (annotated /\ pg' = "stopped"))

Annotate ==
    /\ obsCut
    /\ ~annotated
    /\ annotated' = TRUE
    /\ UNCHANGED <<apiCut, obsCut, pg, podObj, restarts, cuts, deleted, verified, promoted, nodeFlapped, killedInCut, downAfterFence, cleanAtVerify>>

Delete ==
    /\ annotated
    /\ ~deleted
    /\ deleted' = TRUE
    /\ podObj' = IF GGraceOneDelete /\ ~apiCut THEN podObj ELSE FALSE
    /\ UNCHANGED <<apiCut, obsCut, pg, restarts, cuts, annotated, verified, promoted, nodeFlapped, killedInCut, downAfterFence, cleanAtVerify>>

KubeletKills ==
    /\ deleted
    /\ ~apiCut
    /\ pg # "gone"
    /\ pg' = "gone"
    /\ podObj' = FALSE
    /\ UNCHANGED <<apiCut, obsCut, restarts, cuts, annotated, deleted, verified, promoted, nodeFlapped, cleanAtVerify>>
    /\ killedInCut' = killedInCut
    /\ downAfterFence' = TRUE

Verify ==
    /\ deleted
    /\ ~verified
    /\ ~podObj
    /\ ~apiCut
    /\ (GVerifyNodeReady => ~nodeFlapped)
    /\ verified' = TRUE
    /\ cleanAtVerify' = ~nodeFlapped
    /\ UNCHANGED <<apiCut, obsCut, pg, podObj, restarts, cuts, annotated, deleted, promoted, nodeFlapped, killedInCut, downAfterFence>>

PromoteAfterVerify ==
    /\ verified
    /\ ~promoted
    /\ promoted' = TRUE
    /\ UNCHANGED <<apiCut, obsCut, pg, podObj, restarts, cuts, annotated, deleted, verified, nodeFlapped, killedInCut, downAfterFence, cleanAtVerify>>

Next == Cut \/ Heal \/ SidecarKill \/ KubeletRestart \/ Annotate \/ Delete \/ KubeletKills
        \/ Verify \/ PromoteAfterVerify

Spec == Init /\ [][Next]_vars

NeverTwoWriters == ~(promoted /\ pg = "up")

CanaryNeverPromoted == ~promoted
CanaryNeverRestarted == restarts = 0
NeverPromotesPastAnUnreachableNode == promoted => cleanAtVerify
StaysDownWhileCut == (killedInCut /\ apiCut) => pg # "up"
StaysDownAfterTheFence == downAfterFence => pg # "up"
====
