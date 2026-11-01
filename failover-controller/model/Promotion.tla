---- MODULE Promotion ----
EXTENDS Naturals

CONSTANTS MaxLsn, Grace, MaxBreaks, SignAfterCommit, WaitForCnpg, LsnGuard, PinInstance, StuckLimit, CatchUp

VARIABLES primary, standbys, cnpg, lsn, proven, replayed, breaks, pc, waited, standbysAtDeath,
          fenced, crossed, everStuck, fakeUp, earlyFence, lostProven, gen, deadGen, verdict, verdictGen, killedHealthy, stuckFor, stuckPaged,
          archived, catchWait, lossPaged, avoidableLoss

vars == <<primary, standbys, cnpg, lsn, proven, replayed, breaks, pc, waited, standbysAtDeath,
          fenced, crossed, everStuck, fakeUp, earlyFence, lostProven, gen, deadGen, verdict, verdictGen, killedHealthy, stuckFor, stuckPaged,
          archived, catchWait, lossPaged, avoidableLoss>>

Init ==
    /\ primary = "ok"
    /\ standbys \in 0..2
    /\ cnpg \in {"working", "refusing", "down"}
    /\ lsn = 0 /\ proven = 0 /\ replayed = 0
    /\ breaks = 0
    /\ pc = "watch" /\ waited = 0 /\ standbysAtDeath = 0
    /\ fenced = FALSE /\ crossed = FALSE
    /\ everStuck = FALSE /\ fakeUp = FALSE /\ earlyFence = FALSE /\ lostProven = FALSE
    /\ gen = 0 /\ deadGen = 0 /\ verdict = "alive" /\ verdictGen = 0 /\ killedHealthy = FALSE
    /\ stuckFor = 0 /\ stuckPaged = FALSE
    /\ archived = 0 /\ catchWait = 0 /\ lossPaged = FALSE /\ avoidableLoss = FALSE

Write ==
    /\ primary = "ok" /\ ~fenced /\ lsn < MaxLsn
    /\ lsn' = lsn + 1
    /\ UNCHANGED <<primary, standbys, cnpg, proven, replayed, breaks, pc, waited, standbysAtDeath, fenced, crossed, everStuck, fakeUp, earlyFence, lostProven, gen, deadGen, verdict, verdictGen, killedHealthy, stuckFor, stuckPaged, archived, catchWait, lossPaged, avoidableLoss>>

Archive ==
    /\ primary = "ok" /\ ~fenced /\ archived < lsn
    /\ archived' = archived + 1
    /\ UNCHANGED <<primary, standbys, cnpg, lsn, proven, replayed, breaks, pc, waited, standbysAtDeath, fenced, crossed, everStuck, fakeUp, earlyFence, lostProven, gen, deadGen, verdict, verdictGen, killedHealthy, stuckFor, stuckPaged, catchWait, lossPaged, avoidableLoss>>

Reachable == IF primary = "ok" /\ ~fenced THEN lsn ELSE archived

Replicate ==
    /\ replayed < Reachable
    /\ replayed' = replayed + 1
    /\ UNCHANGED <<primary, standbys, cnpg, lsn, proven, breaks, pc, waited, standbysAtDeath, fenced, crossed, everStuck, fakeUp, earlyFence, lostProven, gen, deadGen, verdict, verdictGen, killedHealthy, stuckFor, stuckPaged, archived, catchWait, lossPaged, avoidableLoss>>

Sign ==
    /\ ~fenced
    /\ IF SignAfterCommit THEN primary = "ok" ELSE primary \in {"ok", "stuck"}
    /\ proven' = lsn
    /\ fakeUp' = (fakeUp \/ primary = "stuck")
    /\ pc' = IF pc \in {"watch", "waiting"} THEN "watch" ELSE pc
    /\ waited' = IF pc \in {"watch", "waiting"} THEN 0 ELSE waited
    /\ UNCHANGED <<primary, standbys, cnpg, lsn, replayed, breaks, standbysAtDeath, fenced, crossed, everStuck, earlyFence, lostProven, gen, deadGen, verdict, verdictGen, killedHealthy, stuckFor, stuckPaged, archived, catchWait, lossPaged, avoidableLoss>>

Breaks ==
    /\ primary = "ok" /\ ~fenced /\ breaks < MaxBreaks
    /\ primary' \in {"stuck", "dead"}
    /\ everStuck' = (everStuck \/ primary' = "stuck")
    /\ breaks' = breaks + 1
    /\ UNCHANGED <<standbys, cnpg, lsn, proven, replayed, pc, waited, standbysAtDeath, fenced, crossed, fakeUp, earlyFence, lostProven, gen, deadGen, verdict, verdictGen, killedHealthy, stuckFor, stuckPaged, archived, catchWait, lossPaged, avoidableLoss>>

StuckDies ==
    /\ primary = "stuck" /\ ~fenced
    /\ primary' = "dead"
    /\ UNCHANGED <<standbys, cnpg, lsn, proven, replayed, breaks, pc, waited, standbysAtDeath, fenced, crossed, everStuck, fakeUp, earlyFence, lostProven, gen, deadGen, verdict, verdictGen, killedHealthy, stuckFor, stuckPaged, archived, catchWait, lossPaged, avoidableLoss>>

CnpgPromotes ==
    /\ cnpg = "working" /\ primary = "dead" /\ standbys > 0 /\ ~fenced
    /\ primary' = "ok"
    /\ standbys' = standbys - 1
    /\ gen' = gen + 1
    /\ UNCHANGED <<cnpg, lsn, proven, replayed, breaks, pc, waited, standbysAtDeath, fenced, crossed, everStuck, fakeUp, earlyFence, lostProven, deadGen, verdict, verdictGen, killedHealthy, stuckFor, stuckPaged, archived, catchWait, lossPaged, avoidableLoss>>

ProvenDead == verdict = "dead"

Judge ==
    /\ ~fenced
    /\ verdict' = IF primary = "dead" THEN "dead" ELSE "alive"
    /\ verdictGen' = gen
    /\ UNCHANGED <<primary, standbys, cnpg, lsn, proven, replayed, breaks, pc, waited, standbysAtDeath, fenced, crossed, everStuck, fakeUp, earlyFence, lostProven, gen, deadGen, killedHealthy, stuckFor, stuckPaged, archived, catchWait, lossPaged, avoidableLoss>>

Decide ==
    /\ pc = "watch" /\ ProvenDead
    /\ pc' = "waiting" /\ waited' = 0 /\ standbysAtDeath' = standbys /\ deadGen' = verdictGen
    /\ UNCHANGED <<primary, standbys, cnpg, lsn, proven, replayed, breaks, fenced, crossed, everStuck, fakeUp, earlyFence, lostProven, gen, verdict, verdictGen, killedHealthy, stuckFor, stuckPaged, archived, catchWait, lossPaged, avoidableLoss>>

Tick ==
    /\ pc = "waiting" /\ waited < Grace
    /\ waited' = waited + 1
    /\ UNCHANGED <<primary, standbys, cnpg, lsn, proven, replayed, breaks, pc, standbysAtDeath, fenced, crossed, everStuck, fakeUp, earlyFence, lostProven, gen, deadGen, verdict, verdictGen, killedHealthy, stuckFor, stuckPaged, archived, catchWait, lossPaged, avoidableLoss>>

GraceOver == ~WaitForCnpg \/ standbysAtDeath = 0 \/ waited >= Grace

Fence ==
    /\ pc = "waiting" /\ ProvenDead /\ GraceOver
    /\ (~LsnGuard \/ replayed >= proven \/ catchWait >= CatchUp)
    /\ (~PinInstance \/ gen = deadGen)
    /\ killedHealthy' = (killedHealthy \/ primary # "dead")
    /\ fenced' = TRUE /\ primary' = "dead" /\ standbys' = 0
    /\ earlyFence' = (earlyFence \/ (cnpg = "working" /\ standbysAtDeath > 0 /\ waited < Grace))
    /\ pc' = "fenced"
    /\ UNCHANGED <<cnpg, lsn, proven, replayed, breaks, waited, standbysAtDeath, crossed, everStuck, fakeUp, lostProven, gen, deadGen, verdict, verdictGen, stuckFor, stuckPaged, archived, catchWait, lossPaged, avoidableLoss>>

CatchTick ==
    /\ pc = "waiting" /\ ProvenDead /\ GraceOver /\ LsnGuard /\ replayed < proven /\ catchWait < CatchUp
    /\ catchWait' = catchWait + 1
    /\ UNCHANGED <<primary, standbys, cnpg, lsn, proven, replayed, breaks, pc, waited, standbysAtDeath, fenced, crossed, everStuck, fakeUp, earlyFence, lostProven, gen, deadGen, verdict, verdictGen, killedHealthy, stuckFor, stuckPaged, archived, lossPaged, avoidableLoss>>

Promote ==
    /\ pc = "fenced"
    /\ crossed' = TRUE /\ pc' = "promoted"
    /\ lostProven' = (lostProven \/ replayed < proven)
    /\ lossPaged' = (lossPaged \/ replayed < proven)
    /\ avoidableLoss' = (avoidableLoss \/ (replayed < proven /\ replayed < archived /\ catchWait < CatchUp))
    /\ UNCHANGED <<primary, standbys, cnpg, lsn, proven, replayed, breaks, waited, standbysAtDeath, fenced, everStuck, fakeUp, earlyFence, gen, deadGen, verdict, verdictGen, killedHealthy, stuckFor, stuckPaged, archived, catchWait>>

StuckTick ==
    /\ primary = "stuck" /\ stuckFor < StuckLimit
    /\ stuckFor' = stuckFor + 1
    /\ UNCHANGED <<primary, standbys, cnpg, lsn, proven, replayed, breaks, pc, waited, standbysAtDeath, fenced, crossed, everStuck, fakeUp, earlyFence, lostProven, gen, deadGen, verdict, verdictGen, killedHealthy, stuckPaged, archived, catchWait, lossPaged, avoidableLoss>>

StuckPage ==
    /\ primary = "stuck" /\ stuckFor >= StuckLimit /\ ~stuckPaged
    /\ stuckPaged' = TRUE
    /\ UNCHANGED <<primary, standbys, cnpg, lsn, proven, replayed, breaks, pc, waited, standbysAtDeath, fenced, crossed, everStuck, fakeUp, earlyFence, lostProven, gen, deadGen, verdict, verdictGen, killedHealthy, stuckFor, archived, catchWait, lossPaged, avoidableLoss>>

Next == StuckTick \/ StuckPage \/ Judge \/ Write \/ Archive \/ Replicate \/ Sign \/ Breaks \/ StuckDies \/ CnpgPromotes \/ Decide \/ Tick \/ Fence \/ CatchTick \/ Promote

Spec == Init /\ [][Next]_vars

NoFakeUp == ~fakeUp
NoCrossBeforeCnpgHadItsMinute == ~earlyFence
NoProvenWriteLost == ~lostProven
NeverTwoPrimaries == ~(crossed /\ primary \in {"ok", "stuck"})

CanaryCrosses == ~crossed
CanaryCnpgRecovers == ~(primary = "ok" /\ standbys < standbysAtDeath /\ ~fenced /\ pc = "watch")
NoAvoidableLossInsideTheWindow == ~avoidableLoss
EveryLossIsPaged == lostProven => lossPaged
CanaryPromotesWithLoss == ~lostProven
CanaryCatchesUpThenPromotes == ~(crossed /\ ~lostProven /\ catchWait > 0)
NeverKillsAHealthyPrimary == ~killedHealthy
StuckPagesOnlyWhenStuck == stuckPaged => everStuck
CanaryStuckPages == ~stuckPaged
StuckForeverNeverFailsOver == ~(crossed /\ everStuck /\ breaks = 1 /\ primary = "stuck")
====
