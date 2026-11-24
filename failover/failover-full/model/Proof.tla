---- MODULE Proof ----
EXTENDS Naturals, FiniteSets

CONSTANTS Observers, Popped, Q, MaxFlips, MaxCuts, BindChallenge, CUpVetoes, NeedKube

Honest == Observers \ Popped

VARIABLES db, kube, flips, signedNow, reach, cuts, seenNow, everSeen, report,
          approved, upAtApproval, kubeFreshAtApproval

vars == <<db, kube, flips, signedNow, reach, cuts, seenNow, everSeen, report,
          approved, upAtApproval, kubeFreshAtApproval>>

Kinds == {"none", "A", "C-down", "C-up"}

Truth == IF db = "up" THEN "ready" ELSE "notready"

Init ==
    /\ db = "up"
    /\ kube = "ready"
    /\ flips = 0
    /\ signedNow = FALSE
    /\ reach = [o \in Observers |-> TRUE]
    /\ cuts = 0
    /\ seenNow = [o \in Observers |-> FALSE]
    /\ everSeen = [o \in Observers |-> FALSE]
    /\ report = [o \in Observers |-> [kind |-> "none", fresh |-> FALSE]]
    /\ approved = FALSE
    /\ upAtApproval = FALSE
    /\ kubeFreshAtApproval = FALSE

Crash ==
    /\ db = "up" /\ flips < MaxFlips
    /\ db' = "down" /\ flips' = flips + 1
    /\ UNCHANGED <<kube, signedNow, reach, cuts, seenNow, everSeen, report, approved, upAtApproval, kubeFreshAtApproval>>

Recover ==
    /\ db = "down" /\ flips < MaxFlips
    /\ db' = "up" /\ flips' = flips + 1
    /\ UNCHANGED <<kube, signedNow, reach, cuts, seenNow, everSeen, report, approved, upAtApproval, kubeFreshAtApproval>>

KubeCatchesUp ==
    /\ kube # Truth
    /\ kube' = Truth
    /\ UNCHANGED <<db, flips, signedNow, reach, cuts, seenNow, everSeen, report, approved, upAtApproval, kubeFreshAtApproval>>

Sign ==
    /\ db = "up" /\ ~signedNow
    /\ signedNow' = TRUE
    /\ UNCHANGED <<db, kube, flips, reach, cuts, seenNow, everSeen, report, approved, upAtApproval, kubeFreshAtApproval>>

Deliver(o) ==
    /\ signedNow /\ ~seenNow[o]
    /\ reach[o] \/ o \in Popped
    /\ seenNow' = [seenNow EXCEPT ![o] = TRUE]
    /\ everSeen' = [everSeen EXCEPT ![o] = TRUE]
    /\ UNCHANGED <<db, kube, flips, signedNow, reach, cuts, report, approved, upAtApproval, kubeFreshAtApproval>>

Cut(o) ==
    /\ o \in Honest /\ reach[o] /\ cuts < MaxCuts
    /\ reach' = [reach EXCEPT ![o] = FALSE] /\ cuts' = cuts + 1
    /\ UNCHANGED <<db, kube, flips, signedNow, seenNow, everSeen, report, approved, upAtApproval, kubeFreshAtApproval>>

Heal(o) ==
    /\ ~reach[o]
    /\ reach' = [reach EXCEPT ![o] = TRUE]
    /\ UNCHANGED <<db, kube, flips, signedNow, cuts, seenNow, everSeen, report, approved, upAtApproval, kubeFreshAtApproval>>

HonestReport(o) ==
    /\ o \in Honest
    /\ report' = [report EXCEPT ![o] =
                    IF seenNow[o] THEN [kind |-> "A", fresh |-> TRUE]
                    ELSE [kind |-> "C-down", fresh |-> TRUE]]
    /\ report' # report
    /\ UNCHANGED <<db, kube, flips, signedNow, reach, cuts, seenNow, everSeen, approved, upAtApproval, kubeFreshAtApproval>>

Lie(o) ==
    /\ o \in Popped
    /\ \E r \in (IF seenNow[o] THEN {[kind |-> "A", fresh |-> TRUE]} ELSE {})
              \cup (IF everSeen[o] THEN {[kind |-> "A", fresh |-> FALSE]} ELSE {})
              \cup {[kind |-> "C-down", fresh |-> TRUE], [kind |-> "C-up", fresh |-> TRUE]} :
         /\ r # report[o]
         /\ report' = [report EXCEPT ![o] = r]
    /\ UNCHANGED <<db, kube, flips, signedNow, reach, cuts, seenNow, everSeen, approved, upAtApproval, kubeFreshAtApproval>>

ValidUp(r) == r.kind = "A" /\ (r.fresh \/ ~BindChallenge)

Vetoed ==
    \/ \E o \in Observers : ValidUp(report[o])
    \/ (CUpVetoes /\ \E o \in Observers : report[o].kind = "C-up" /\ report[o].fresh)

Downs == {o \in Observers : report[o].kind = "C-down" /\ report[o].fresh}

CanApprove ==
    /\ ~approved
    /\ ~Vetoed
    /\ Cardinality(Downs) >= Q
    /\ (~NeedKube \/ kube = "notready")

NewChallenge ==
    /\ ~approved
    /\ \A o \in Honest : report[o].fresh
    /\ ~CanApprove
    /\ signedNow' = FALSE
    /\ seenNow' = [o \in Observers |-> FALSE]
    /\ report' = [o \in Observers |-> [report[o] EXCEPT !.fresh = FALSE]]
    /\ UNCHANGED <<db, kube, flips, reach, cuts, everSeen, approved, upAtApproval, kubeFreshAtApproval>>

Approve ==
    /\ CanApprove
    /\ approved' = TRUE
    /\ upAtApproval' = (db = "up")
    /\ kubeFreshAtApproval' = (kube = Truth)
    /\ UNCHANGED <<db, kube, flips, signedNow, reach, cuts, seenNow, everSeen, report>>

Next ==
    \/ Crash \/ Recover \/ KubeCatchesUp \/ Sign \/ NewChallenge \/ Approve
    \/ \E o \in Observers : Deliver(o) \/ Cut(o) \/ Heal(o) \/ HonestReport(o) \/ Lie(o)

Fairness ==
    /\ WF_vars(KubeCatchesUp) /\ WF_vars(Sign) /\ WF_vars(NewChallenge) /\ WF_vars(Approve)
    /\ \A o \in Honest : WF_vars(HonestReport(o)) /\ WF_vars(Heal(o)) /\ WF_vars(Deliver(o))

Spec == Init /\ [][Next]_vars /\ Fairness

UpCannotBeForged == \A o \in Observers : (report[o].kind = "A" /\ report[o].fresh) => signedNow

NoFailoverOverATruthfulKube == (approved /\ upAtApproval) => ~kubeFreshAtApproval

LiarsCannotBlockARealOutage == (db = "down" /\ flips = MaxFlips) ~> approved

====
