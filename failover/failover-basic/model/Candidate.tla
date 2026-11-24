---- MODULE Candidate ----
EXTENDS Naturals, FiniteSets

CONSTANTS MaxEpochs, MaxMislabels, MaxFlips, MaxRevokes, MaxFalls, Far,
          DeciderBuggy, PartialWrite,
          GLabel, GDelay, GLag, GStall, UnknownWaits,
          BindCandidate, BindHost, BindFence, MissingKeyDenies, ClearSlot

Original == "c1"
Live     == {"c2"}
Delayed  == {"c3"}
Spares   == <<"c4", "c5">>
SpareSet == {Spares[i] : i \in 1..2}
Members  == {Original} \cup Live \cup Delayed \cup SpareSet
Labels   == {"live", "delayed"}
None     == ""

TrueKind(m) == IF m \in Delayed THEN "delayed" ELSE "live"

VARIABLES primary, inPlay, label, ready, readable, lag, epoch, step, cand, slot, checked,
          promoted, lastPromoted, fenced, rw, refused, refusedGood, badFence, revokes, flips,
          falls, added, waited, earlyApprove, approvedUnknown, approvedFar, floor, avoidable

vars == <<primary, inPlay, label, ready, readable, lag, epoch, step, cand, slot, checked,
          promoted, lastPromoted, fenced, rw, refused, refusedGood, badFence, revokes, flips,
          falls, added, waited, earlyApprove, approvedUnknown, approvedFar, floor, avoidable>>

EmptySlot == [state |-> "none", epoch |-> 0, cand |-> None, host |-> None]

Start == {Original} \cup Live \cup Delayed

Init ==
    /\ primary = Original
    /\ inPlay = Start
    /\ \E wrong \in SUBSET (Start \ {Original}) :
         /\ Cardinality(wrong) <= MaxMislabels
         /\ label \in { f \in [Members -> Labels] :
                          \A m \in Members : (m \notin wrong) => f[m] = TrueKind(m) }
    /\ ready \in [Members -> BOOLEAN]
    /\ readable \in [Members -> BOOLEAN]
    /\ lag \in { f \in [Members -> 0..Far] : \A m \in SpareSet : f[m] = Far }
    /\ epoch = 0
    /\ step = "idle"
    /\ cand = None
    /\ slot = EmptySlot
    /\ checked = {}
    /\ promoted = {}
    /\ lastPromoted = Original
    /\ fenced = {}
    /\ rw = Original
    /\ refused = FALSE
    /\ refusedGood = FALSE
    /\ badFence = FALSE
    /\ revokes = 0
    /\ flips = 0
    /\ falls = 0
    /\ added = 0
    /\ waited = FALSE
    /\ earlyApprove = FALSE
    /\ approvedUnknown = FALSE
    /\ approvedFar = FALSE
    /\ floor \in { f \in [Members -> 0..Far] : \A m \in Members : f[m] <= lag[m] }
    /\ avoidable = FALSE

Others == inPlay \ ({primary} \cup fenced)

HonestChoices == { m \in Others : label[m] = "live" /\ ready[m] }

Choices == IF DeciderBuggy THEN Others ELSE HonestChoices

AddReplica ==
    /\ step = "idle" /\ promoted # {} /\ added < 2
    /\ added' = added + 1
    /\ inPlay' = inPlay \cup {Spares[added + 1]}
    /\ UNCHANGED <<waited, earlyApprove, approvedUnknown, approvedFar, primary, label, ready, readable, lag, epoch, step, cand, slot, checked, promoted, lastPromoted, fenced, rw, refused, refusedGood, badFence, revokes, flips, falls, floor, avoidable>>

ReadyFlips ==
    /\ flips < MaxFlips
    /\ \E m \in inPlay \ {primary} :
         \/ ready' = [ready EXCEPT ![m] = ~@] /\ UNCHANGED readable
         \/ readable' = [readable EXCEPT ![m] = ~@] /\ UNCHANGED ready
    /\ flips' = flips + 1
    /\ UNCHANGED <<waited, earlyApprove, approvedUnknown, approvedFar, primary, inPlay, label, lag, epoch, step, cand, slot, checked, promoted, lastPromoted, fenced, rw, refused, refusedGood, badFence, revokes, falls, added, floor, avoidable>>

FallBehind ==
    /\ step = "idle" /\ falls < MaxFalls
    /\ \E m \in inPlay \ {primary} :
         /\ lag[m] < Far
         /\ lag' = [lag EXCEPT ![m] = @ + 1]
    /\ falls' = falls + 1
    /\ UNCHANGED <<waited, earlyApprove, approvedUnknown, approvedFar, primary, inPlay, label, ready, readable, epoch, step, cand, slot, checked, promoted, lastPromoted, fenced, rw, refused, refusedGood, badFence, revokes, flips, added, floor, avoidable>>

CatchUp ==
    /\ \E m \in inPlay \ {primary} :
         /\ lag[m] > floor[m]
         /\ lag' = [lag EXCEPT ![m] = @ - 1]
    /\ UNCHANGED <<waited, earlyApprove, approvedUnknown, approvedFar, primary, inPlay, label, ready, readable, epoch, step, cand, slot, checked, promoted, lastPromoted, fenced, rw, refused, refusedGood, badFence, revokes, flips, falls, added, floor, avoidable>>

Claim ==
    /\ step = "idle" /\ epoch < MaxEpochs
    /\ Choices # {}
    /\ \E m \in Choices :
         /\ cand' = m
         /\ epoch' = epoch + 1
         /\ step' = "claimed"
    /\ waited' = FALSE
    /\ UNCHANGED <<earlyApprove, approvedUnknown, approvedFar, primary, inPlay, label, ready, readable, lag, slot, checked, promoted, lastPromoted, fenced, rw, refused, refusedGood, badFence, revokes, flips, falls, added, floor, avoidable>>

CaughtUp(m) == readable[m] /\ lag[m] = 0

WindowOk(m) ==
    IF ~GLag THEN TRUE
    ELSE waited \/ CaughtUp(m) \/ (~readable[m] /\ ~UnknownWaits)

Stalled(m) == lag[m] = floor[m]

Wait ==
    /\ step = "claimed" /\ ~waited /\ ~CaughtUp(cand)
    /\ (~GStall \/ Stalled(cand))
    /\ waited' = TRUE
    /\ UNCHANGED <<earlyApprove, approvedUnknown, approvedFar, primary, inPlay, label, ready, readable, lag, epoch, step, cand, slot, checked, promoted, lastPromoted, fenced, rw, refused, refusedGood, badFence, revokes, flips, falls, added, floor, avoidable>>

Recheck(m) ==
    /\ m \in Others
    /\ ready[m]
    /\ (~GLabel \/ label[m] = "live")
    /\ (~GDelay \/ m \notin Delayed)

TrulyGood(m) == m \notin Delayed /\ m \in Others /\ ready[m]

Approve ==
    /\ step = "claimed"
    /\ slot.state # "valid" \/ slot.epoch # epoch
    /\ Recheck(cand)
    /\ WindowOk(cand)
    /\ checked' = checked \cup {<<epoch, cand>>}
    /\ earlyApprove' = (earlyApprove \/ ~(CaughtUp(cand) \/ waited))
    /\ approvedUnknown' = (approvedUnknown \/ ~readable[cand])
    /\ approvedFar' = (approvedFar \/ (readable[cand] /\ lag[cand] = Far))
    /\ avoidable' = (avoidable \/ lag[cand] > floor[cand])
    /\ \/ slot' = [state |-> "valid", epoch |-> epoch, cand |-> cand, host |-> cand]
       \/ PartialWrite /\ slot' = [slot EXCEPT !.state = "valid", !.epoch = epoch]
    /\ UNCHANGED <<waited, primary, inPlay, label, ready, readable, lag, epoch, step, cand, promoted, lastPromoted, fenced, rw, refused, refusedGood, badFence, revokes, flips, falls, added, floor>>

Refuse ==
    /\ step = "claimed"
    /\ ~Recheck(cand)
    /\ refused' = TRUE
    /\ refusedGood' = (refusedGood \/ (TrulyGood(cand) /\ label[cand] = "live"))
    /\ step' = "halted"
    /\ UNCHANGED <<waited, earlyApprove, approvedUnknown, approvedFar, primary, inPlay, label, ready, readable, lag, epoch, cand, slot, checked, promoted, lastPromoted, fenced, rw, badFence, revokes, flips, falls, added, floor, avoidable>>

Cleared(s, st) ==
    IF ClearSlot THEN [state |-> st, epoch |-> s.epoch, cand |-> None, host |-> None]
    ELSE [s EXCEPT !.state = st]

Revoke ==
    /\ step = "claimed" /\ slot.state = "valid" /\ slot.epoch = epoch
    /\ revokes < MaxRevokes
    /\ slot' = Cleared(slot, "revoked")
    /\ revokes' = revokes + 1
    /\ step' = "idle"
    /\ UNCHANGED <<waited, earlyApprove, approvedUnknown, approvedFar, primary, inPlay, label, ready, readable, lag, epoch, cand, checked, promoted, lastPromoted, fenced, rw, refused, refusedGood, badFence, flips, falls, added, floor, avoidable>>

Valid == slot.state = "valid" /\ slot.epoch = epoch

SlotSays(v, x) == IF v = None THEN ~MissingKeyDenies ELSE v = x

AdmitFence(x)   == Valid /\ (~BindFence \/ x = primary)
AdmitPromote(y) == Valid /\ (~BindCandidate \/ SlotSays(slot.cand, y))
AdmitFlip(h)    == Valid /\ (~BindHost \/ SlotSays(slot.host, h))

FenceTargets   == IF DeciderBuggy THEN inPlay \ fenced ELSE {primary}
PromoteTargets == IF DeciderBuggy THEN inPlay \ ({primary} \cup fenced) ELSE {cand}
FlipTargets    == IF DeciderBuggy THEN inPlay ELSE {cand}

Fence ==
    /\ step = "claimed"
    /\ \E x \in FenceTargets :
         /\ AdmitFence(x)
         /\ fenced' = fenced \cup {x}
         /\ badFence' = (badFence \/ x # primary)
         /\ step' = IF x = primary THEN "fenced" ELSE step
    /\ UNCHANGED <<waited, earlyApprove, approvedUnknown, approvedFar, primary, inPlay, label, ready, readable, lag, epoch, cand, slot, checked, promoted, lastPromoted, rw, refused, refusedGood, revokes, flips, falls, added, floor, avoidable>>

Promote ==
    /\ step = "fenced"
    /\ \E y \in PromoteTargets :
         /\ AdmitPromote(y)
         /\ promoted' = promoted \cup {<<epoch, y, lag[y]>>}
         /\ lastPromoted' = y
         /\ primary' = y
         /\ lag' = [lag EXCEPT ![y] = 0]
         /\ floor' = [floor EXCEPT ![y] = 0]
         /\ step' = "promoted"
    /\ UNCHANGED <<waited, earlyApprove, approvedUnknown, approvedFar, inPlay, label, ready, readable, epoch, cand, slot, checked, fenced, rw, refused, refusedGood, badFence, revokes, flips, falls, added, avoidable>>

Flip ==
    /\ step = "promoted"
    /\ \E h \in FlipTargets :
         /\ AdmitFlip(h)
         /\ rw' = h
         /\ slot' = Cleared(slot, "closed")
         /\ step' = "idle"
    /\ UNCHANGED <<waited, earlyApprove, approvedUnknown, approvedFar, primary, inPlay, label, ready, readable, lag, epoch, cand, checked, promoted, lastPromoted, fenced, refused, refusedGood, badFence, revokes, flips, falls, added, floor, avoidable>>

Next == AddReplica \/ ReadyFlips \/ FallBehind \/ CatchUp \/ Claim \/ Wait \/ Approve \/ Refuse \/ Revoke \/ Fence \/ Promote \/ Flip

Spec == Init /\ [][Next]_vars

PromotedMembers == { p[2] : p \in promoted }

NeverPromotesDelayed == PromotedMembers \cap Delayed = {}
ApprovesBehindOrUnknownOnlyAfterTheWindow == ~earlyApprove
NeverLosesWhatItCouldStillReplay == ~avoidable
OnlyWhatTheVerifierCheckedIsPromoted == { <<p[1], p[2]>> : p \in promoted } \subseteq checked
RwFollowsThePromotion == step # "promoted" => rw = lastPromoted
NeverFencesAnythingButTheTarget == ~badFence
NeverRefusesAGoodCandidateItCanSee == ~refusedGood
OnePromotionPerEpoch == \A p, q \in promoted : p[1] = q[1] => p = q

CanaryNeverPromoted == promoted = {}
CanaryNeverFlipped == rw = Original
CanaryNeverTwoRuns == Cardinality(promoted) < 2
CanaryNeverRefuses == ~refused
CanaryNeverRevokedThenPromoted == ~(revokes > 0 /\ promoted # {})
CanaryNeverRefusesMislabeledDelayed == ~(\E m \in Delayed : label[m] = "live" /\ refused /\ cand = m)
CanaryNeverPromotesWithLoss == \A p \in promoted : p[3] = 0
CanaryNeverPromotesAnAddedReplica == PromotedMembers \cap SpareSet = {}
CanaryNeverRefusesABootstrappingReplica == ~(refused /\ cand \in SpareSet /\ ~ready[cand])
CanaryNeverApprovesUnknownLoss == ~approvedUnknown
CanaryNeverApprovesFarBehind == ~approvedFar
====
