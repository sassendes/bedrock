---- MODULE Evidence ----
EXTENDS Naturals, FiniteSets

CONSTANTS Observers, Popped, Q, DeadNeeded, MaxFlips, MaxCuts, Broken, CanHang

Honest == Observers \ Popped
Witnesses == {"replicas", "manager", "kubelet", "api"}

VARIABLES db, repl, flips, cuts, reach, signedNow, seenNow, everSeen, report, view,
          approved, dbAtApproval, staleAtApproval, contradiction

vars == <<db, repl, flips, cuts, reach, signedNow, seenNow, everSeen, report, view,
          approved, dbAtApproval, staleAtApproval, contradiction>>

DBStates == IF CanHang THEN {"up", "down", "hung"} ELSE {"up", "down"}

Truth(w) ==
    CASE w = "replicas" -> IF db # "up" \/ repl = "broken" THEN "dead" ELSE "alive"
      [] w = "manager"  -> IF db # "up" THEN "dead" ELSE "alive"
      [] w = "kubelet"  -> IF db # "up" THEN "dead" ELSE "alive"
      [] w = "api"      -> IF db # "up" THEN "dead" ELSE "alive"

Init ==
    /\ db = "up"
    /\ repl = "ok"
    /\ flips = 0
    /\ cuts = 0
    /\ reach = [o \in Observers |-> TRUE]
    /\ signedNow = FALSE
    /\ seenNow = [o \in Observers |-> FALSE]
    /\ everSeen = [o \in Observers |-> FALSE]
    /\ report = [o \in Observers |-> [kind |-> "none", fresh |-> FALSE]]
    /\ view = [w \in Witnesses |-> "alive"]
    /\ approved = FALSE
    /\ dbAtApproval = "up"
    /\ staleAtApproval = FALSE
    /\ contradiction = FALSE

Keep(vs) == UNCHANGED vs

DBChanges ==
    /\ flips < MaxFlips
    /\ \E s \in DBStates \ {db} : db' = s
    /\ flips' = flips + 1
    /\ UNCHANGED <<repl, cuts, reach, signedNow, seenNow, everSeen, report, view, approved, dbAtApproval, staleAtApproval, contradiction>>

ReplicationBreaks ==
    /\ repl = "ok" /\ cuts < MaxCuts
    /\ repl' = "broken" /\ cuts' = cuts + 1
    /\ UNCHANGED <<db, flips, reach, signedNow, seenNow, everSeen, report, view, approved, dbAtApproval, staleAtApproval, contradiction>>

ReplicationHeals ==
    /\ repl = "broken"
    /\ repl' = "ok"
    /\ UNCHANGED <<db, flips, cuts, reach, signedNow, seenNow, everSeen, report, view, approved, dbAtApproval, staleAtApproval, contradiction>>

Cut(o) ==
    /\ o \in Honest /\ reach[o] /\ cuts < MaxCuts
    /\ reach' = [reach EXCEPT ![o] = FALSE] /\ cuts' = cuts + 1
    /\ UNCHANGED <<db, repl, flips, signedNow, seenNow, everSeen, report, view, approved, dbAtApproval, staleAtApproval, contradiction>>

Heal(o) ==
    /\ ~reach[o]
    /\ reach' = [reach EXCEPT ![o] = TRUE]
    /\ UNCHANGED <<db, repl, flips, cuts, signedNow, seenNow, everSeen, report, view, approved, dbAtApproval, staleAtApproval, contradiction>>

Sign(o) ==
    /\ db = "up" /\ (reach[o] \/ o \in Popped) /\ ~seenNow[o]
    /\ signedNow' = TRUE
    /\ seenNow' = [seenNow EXCEPT ![o] = TRUE]
    /\ everSeen' = [everSeen EXCEPT ![o] = TRUE]
    /\ UNCHANGED <<db, repl, flips, cuts, reach, report, view, approved, dbAtApproval, staleAtApproval, contradiction>>

HonestReport(o) ==
    /\ o \in Honest
    /\ report' = [report EXCEPT ![o] =
                    IF seenNow[o] THEN [kind |-> "A", fresh |-> TRUE] ELSE [kind |-> "C", fresh |-> TRUE]]
    /\ report' # report
    /\ UNCHANGED <<db, repl, flips, cuts, reach, signedNow, seenNow, everSeen, view, approved, dbAtApproval, staleAtApproval, contradiction>>

Lie(o) ==
    /\ o \in Popped
    /\ \E r \in (IF seenNow[o] THEN {[kind |-> "A", fresh |-> TRUE]} ELSE {})
              \cup (IF everSeen[o] THEN {[kind |-> "A", fresh |-> FALSE]} ELSE {})
              \cup {[kind |-> "C", fresh |-> TRUE], [kind |-> "B", fresh |-> TRUE]} :
         /\ r # report[o]
         /\ report' = [report EXCEPT ![o] = r]
    /\ UNCHANGED <<db, repl, flips, cuts, reach, signedNow, seenNow, everSeen, view, approved, dbAtApproval, staleAtApproval, contradiction>>

WitnessSees(w) ==
    /\ w # Broken
    /\ view[w] # Truth(w)
    /\ view' = [view EXCEPT ![w] = Truth(w)]
    /\ UNCHANGED <<db, repl, flips, cuts, reach, signedNow, seenNow, everSeen, report, approved, dbAtApproval, staleAtApproval, contradiction>>

WitnessGoesSilent(w) ==
    /\ w # Broken /\ view[w] # "silent" /\ cuts < MaxCuts
    /\ view' = [view EXCEPT ![w] = "silent"] /\ cuts' = cuts + 1
    /\ UNCHANGED <<db, repl, flips, reach, signedNow, seenNow, everSeen, report, approved, dbAtApproval, staleAtApproval, contradiction>>

BrokenWitnessSays ==
    /\ Broken \in Witnesses
    /\ \E a \in {"alive", "dead", "silent"} \ {view[Broken]} : view' = [view EXCEPT ![Broken] = a]
    /\ UNCHANGED <<db, repl, flips, cuts, reach, signedNow, seenNow, everSeen, report, approved, dbAtApproval, staleAtApproval, contradiction>>

FreshA == \E o \in Observers : report[o].kind = "A" /\ report[o].fresh
Cs == {o \in Observers : report[o].kind = "C" /\ report[o].fresh}
DeadSay == {w \in Witnesses : view[w] = "dead"}
ProvenDown == Cardinality(DeadSay) >= DeadNeeded

CanApprove ==
    /\ ~FreshA
    /\ Cardinality(Cs) >= Q
    /\ ProvenDown

EndRound ==
    /\ ~approved
    /\ \A o \in Honest : report[o].fresh
    /\ contradiction' = (FreshA /\ ProvenDown)
    /\ IF CanApprove
       THEN /\ approved' = TRUE
            /\ dbAtApproval' = db
            /\ staleAtApproval' = \E w \in DeadSay : Truth(w) # "dead"
            /\ UNCHANGED <<signedNow, seenNow, report>>
       ELSE /\ signedNow' = FALSE
            /\ seenNow' = [o \in Observers |-> FALSE]
            /\ report' = [o \in Observers |-> [report[o] EXCEPT !.fresh = FALSE]]
            /\ UNCHANGED <<approved, dbAtApproval, staleAtApproval>>
    /\ UNCHANGED <<db, repl, flips, cuts, reach, everSeen, view>>

Next ==
    \/ DBChanges \/ ReplicationBreaks \/ ReplicationHeals \/ BrokenWitnessSays
    \/ EndRound
    \/ \E o \in Observers : Cut(o) \/ Heal(o) \/ Sign(o) \/ HonestReport(o) \/ Lie(o)
    \/ \E w \in Witnesses : WitnessSees(w) \/ WitnessGoesSilent(w)

Fairness ==
    /\ WF_vars(EndRound) /\ WF_vars(ReplicationHeals)
    /\ \A o \in Honest : WF_vars(HonestReport(o)) /\ WF_vars(Heal(o)) /\ WF_vars(Sign(o))
    /\ \A w \in Witnesses : WF_vars(WitnessSees(w))

Spec == Init /\ [][Next]_vars /\ Fairness

NeverFailsOverAHealthyDBOnFreshEvidence == (approved /\ dbAtApproval = "up") => staleAtApproval

NeverActsOnAContradiction == approved => ~contradiction

UpCannotBeForged == \A o \in Observers : (report[o].kind = "A" /\ report[o].fresh) => signedNow

CrashFailsOver == (db = "down" /\ flips = MaxFlips) ~> approved

HungFailsOver == (db = "hung" /\ flips = MaxFlips) ~> approved

====
