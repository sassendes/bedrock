---- MODULE HalfFence ----
EXTENDS Naturals

CONSTANTS SaveFirst, MaxFlaps, MaxErrors

VARIABLES claim, step, annotated, podsGone, upSeen, flaps, errors, paged

vars == <<claim, step, annotated, podsGone, upSeen, flaps, errors, paged>>

Init ==
    /\ claim = "open"
    /\ step = "claimed"
    /\ annotated = FALSE
    /\ podsGone = FALSE
    /\ upSeen = FALSE
    /\ flaps = 0
    /\ errors = 0
    /\ paged = FALSE

EvidenceFlaps ==
    /\ flaps < MaxFlaps /\ ~podsGone
    /\ upSeen' = ~upSeen
    /\ flaps' = flaps + 1
    /\ UNCHANGED <<claim, step, annotated, podsGone, errors, paged>>

Veto ==
    /\ claim = "open" /\ step = "claimed" /\ upSeen
    /\ claim' = "closed"
    /\ UNCHANGED <<step, annotated, podsGone, upSeen, flaps, errors, paged>>

SaveIntent ==
    /\ SaveFirst
    /\ claim = "open" /\ step = "claimed" /\ ~upSeen
    /\ step' = "fencing"
    /\ UNCHANGED <<claim, annotated, podsGone, upSeen, flaps, errors, paged>>

CanFence == IF SaveFirst THEN step = "fencing" ELSE step = "claimed" /\ ~upSeen

Annotate ==
    /\ claim = "open" /\ CanFence /\ ~annotated
    /\ annotated' = TRUE
    /\ UNCHANGED <<claim, step, podsGone, upSeen, flaps, errors, paged>>

DeleteFails ==
    /\ claim = "open" /\ CanFence /\ annotated /\ ~podsGone /\ errors < MaxErrors
    /\ errors' = errors + 1
    /\ UNCHANGED <<claim, step, annotated, podsGone, upSeen, flaps, paged>>

Delete ==
    /\ claim = "open" /\ CanFence /\ annotated /\ ~podsGone
    /\ podsGone' = TRUE
    /\ UNCHANGED <<claim, step, annotated, upSeen, flaps, errors, paged>>

SaveAfter ==
    /\ ~SaveFirst
    /\ claim = "open" /\ step = "claimed" /\ annotated /\ podsGone
    /\ step' = "fencing"
    /\ UNCHANGED <<claim, annotated, podsGone, upSeen, flaps, errors, paged>>

Verified ==
    /\ claim = "open" /\ step = "fencing" /\ annotated /\ podsGone
    /\ step' = "fenced"
    /\ UNCHANGED <<claim, annotated, podsGone, upSeen, flaps, errors, paged>>

Timeout ==
    /\ claim = "open" /\ step = "fencing" /\ ~podsGone
    /\ claim' = "halted"
    /\ paged' = TRUE
    /\ UNCHANGED <<step, annotated, podsGone, upSeen, flaps, errors>>

Next == EvidenceFlaps \/ Veto \/ SaveIntent \/ Annotate \/ DeleteFails \/ Delete \/ SaveAfter \/ Verified \/ Timeout

Spec == Init /\ [][Next]_vars

NoOrphanFence == annotated => claim = "open" \/ (claim = "halted" /\ paged)

CanaryNeverFenced == step # "fenced"
CanaryNeverVetoed == claim # "closed"
CanaryNeverHalts == claim # "halted"
====
