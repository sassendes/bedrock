---- MODULE Decider ----
EXTENDS Naturals

CONSTANTS Deciders, MaxLeaseChanges, MaxFlaps, Naive

None == "none"

VARIABLES
    holder, admitted, leaseChanges,
    claim, snap,
    oldRun, apiPods, reconcile, deleting,
    nodeUp, flaps, flappedSinceClaim,
    promoted, flipped

vars == <<holder, admitted, leaseChanges, claim, snap, oldRun, apiPods, reconcile, deleting,
          nodeUp, flaps, flappedSinceClaim, promoted, flipped>>

NoClaim == [state |-> "none", step |-> "none", epoch |-> 0, ver |-> 0]

Init ==
    /\ holder \in Deciders
    /\ admitted = holder
    /\ leaseChanges = 0
    /\ claim = NoClaim
    /\ snap = [d \in Deciders |-> NoClaim]
    /\ oldRun = "running"
    /\ apiPods = 1
    /\ reconcile = "on"
    /\ deleting = FALSE
    /\ nodeUp = TRUE
    /\ flaps = 0
    /\ flappedSinceClaim = FALSE
    /\ promoted = FALSE
    /\ flipped = FALSE

Admits(d) == admitted = d

Save(d, c) ==
    IF claim.ver = snap[d].ver
    THEN /\ claim' = [c EXCEPT !.ver = claim.ver + 1]
         /\ snap' = [snap EXCEPT ![d] = [c EXCEPT !.ver = claim.ver + 1]]
    ELSE UNCHANGED <<claim, snap>>

FenceHolds ==
    IF Naive THEN apiPods = 0 /\ nodeUp
    ELSE apiPods = 0 /\ nodeUp /\ ~flappedSinceClaim

Read(d) ==
    /\ snap' = [snap EXCEPT ![d] = claim]
    /\ UNCHANGED <<holder, admitted, leaseChanges, claim, oldRun, apiPods, reconcile, deleting, nodeUp, flaps, flappedSinceClaim, promoted, flipped>>

Open(d) ==
    /\ Admits(d)
    /\ snap[d].state = "none"
    /\ reconcile = "on"
    /\ (Naive \/ apiPods = 1)
    /\ Save(d, [state |-> "open", step |-> "claimed", epoch |-> snap[d].epoch + 1, ver |-> 0])
    /\ flappedSinceClaim' = IF claim.ver = snap[d].ver THEN ~nodeUp ELSE flappedSinceClaim
    /\ UNCHANGED <<holder, admitted, leaseChanges, oldRun, apiPods, reconcile, deleting, nodeUp, flaps, promoted, flipped>>

Fence(d) ==
    /\ Admits(d)
    /\ snap[d].state = "open" /\ snap[d].step = "claimed"
    /\ reconcile' = "off"
    /\ deleting' = TRUE
    /\ Save(d, [snap[d] EXCEPT !.step = "fencing"])
    /\ UNCHANGED <<holder, admitted, leaseChanges, oldRun, apiPods, nodeUp, flaps, flappedSinceClaim, promoted, flipped>>

Verify(d) ==
    /\ Admits(d)
    /\ snap[d].state = "open" /\ snap[d].step = "fencing"
    /\ FenceHolds
    /\ Save(d, [snap[d] EXCEPT !.step = "fenced"])
    /\ UNCHANGED <<holder, admitted, leaseChanges, oldRun, apiPods, reconcile, deleting, nodeUp, flaps, flappedSinceClaim, promoted, flipped>>

Halt(d) ==
    /\ Admits(d)
    /\ snap[d].state = "open" /\ snap[d].step \in {"fencing", "fenced"}
    /\ ~FenceHolds
    /\ Save(d, [snap[d] EXCEPT !.state = "halted"])
    /\ UNCHANGED <<holder, admitted, leaseChanges, oldRun, apiPods, reconcile, deleting, nodeUp, flaps, flappedSinceClaim, promoted, flipped>>

Promote(d) ==
    /\ Admits(d)
    /\ snap[d].state = "open" /\ snap[d].step = "fenced"
    /\ FenceHolds
    /\ promoted' = TRUE
    /\ Save(d, [snap[d] EXCEPT !.step = "promoted"])
    /\ UNCHANGED <<holder, admitted, leaseChanges, oldRun, apiPods, reconcile, deleting, nodeUp, flaps, flappedSinceClaim, flipped>>

Flip(d) ==
    /\ Admits(d)
    /\ snap[d].state = "open" /\ snap[d].step = "promoted"
    /\ flipped' = TRUE
    /\ Save(d, [snap[d] EXCEPT !.step = "flipped", !.state = "closed"])
    /\ UNCHANGED <<holder, admitted, leaseChanges, oldRun, apiPods, reconcile, deleting, nodeUp, flaps, flappedSinceClaim, promoted>>

Decide(d) == Read(d) \/ Open(d) \/ Fence(d) \/ Verify(d) \/ Halt(d) \/ Promote(d) \/ Flip(d)

LeaseMoves ==
    /\ leaseChanges < MaxLeaseChanges
    /\ \E d \in Deciders \ {holder} : holder' = d
    /\ leaseChanges' = leaseChanges + 1
    /\ UNCHANGED <<admitted, claim, snap, oldRun, apiPods, reconcile, deleting, nodeUp, flaps, flappedSinceClaim, promoted, flipped>>

AdmissionCatchesUp ==
    /\ admitted # holder
    /\ admitted' = holder
    /\ UNCHANGED <<holder, leaseChanges, claim, snap, oldRun, apiPods, reconcile, deleting, nodeUp, flaps, flappedSinceClaim, promoted, flipped>>

KubeletStops ==
    /\ deleting /\ nodeUp /\ apiPods = 1
    /\ oldRun' = "stopped"
    /\ apiPods' = 0
    /\ UNCHANGED <<holder, admitted, leaseChanges, claim, snap, reconcile, deleting, nodeUp, flaps, flappedSinceClaim, promoted, flipped>>

ForceDeleted ==
    /\ ~nodeUp /\ apiPods = 1
    /\ apiPods' = 0
    /\ UNCHANGED <<holder, admitted, leaseChanges, claim, snap, oldRun, reconcile, deleting, nodeUp, flaps, flappedSinceClaim, promoted, flipped>>

KubeletKillsOrphan ==
    /\ nodeUp /\ apiPods = 0 /\ oldRun = "running"
    /\ oldRun' = "stopped"
    /\ UNCHANGED <<holder, admitted, leaseChanges, claim, snap, apiPods, reconcile, deleting, nodeUp, flaps, flappedSinceClaim, promoted, flipped>>

OperatorRecreates ==
    /\ reconcile = "on" /\ apiPods = 0 /\ nodeUp
    /\ apiPods' = 1
    /\ oldRun' = "running"
    /\ UNCHANGED <<holder, admitted, leaseChanges, claim, snap, reconcile, deleting, nodeUp, flaps, flappedSinceClaim, promoted, flipped>>

NodeDown ==
    /\ nodeUp /\ flaps < MaxFlaps
    /\ nodeUp' = FALSE
    /\ flaps' = flaps + 1
    /\ flappedSinceClaim' = (flappedSinceClaim \/ claim.state # "none")
    /\ UNCHANGED <<holder, admitted, leaseChanges, claim, snap, oldRun, apiPods, reconcile, deleting, promoted, flipped>>

NodeBack ==
    /\ ~nodeUp
    /\ nodeUp' = TRUE
    /\ UNCHANGED <<holder, admitted, leaseChanges, claim, snap, oldRun, apiPods, reconcile, deleting, flaps, flappedSinceClaim, promoted, flipped>>

Environment ==
    \/ LeaseMoves \/ AdmissionCatchesUp
    \/ KubeletStops \/ ForceDeleted \/ KubeletKillsOrphan \/ OperatorRecreates
    \/ NodeDown \/ NodeBack

Next == (\E d \in Deciders : Decide(d)) \/ Environment

Fairness ==
    /\ \A d \in Deciders : WF_vars(Read(d)) /\ WF_vars(Fence(d)) /\ WF_vars(Verify(d)) /\ WF_vars(Halt(d)) /\ WF_vars(Promote(d)) /\ WF_vars(Flip(d))
    /\ WF_vars(AdmissionCatchesUp) /\ WF_vars(KubeletStops) /\ WF_vars(KubeletKillsOrphan) /\ WF_vars(NodeBack)

Spec == Init /\ [][Next]_vars /\ Fairness

NoTwoPrimaries == ~(promoted /\ oldRun = "running")

PromoteOnlyAfterTheFence == promoted => reconcile = "off"

FlipOnlyAfterPromote == flipped => promoted

OneRun == claim.epoch <= 1

ClaimAlwaysEnds == (claim.state = "open") ~> (claim.state \in {"closed", "halted"})

====
