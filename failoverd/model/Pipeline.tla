---- MODULE Pipeline ----
EXTENDS Naturals

CONSTANTS Judges, Deciders, MaxTerm, MaxRound,
          GApproval, GJudgeTerm, GDeciderTerm, GFreshVerdict, GLastLook

VARIABLES round, approved, alive, jHolder, jTerm, dHolder, dTerm, verdict, annotated, acted, actLog

vars == <<round, approved, alive, jHolder, jTerm, dHolder, dTerm, verdict, annotated, acted, actLog>>

None == "none"

Init ==
    /\ round = 1
    /\ approved = FALSE
    /\ alive = FALSE
    /\ jHolder \in Judges
    /\ jTerm = 1
    /\ dHolder \in Deciders
    /\ dTerm = 1
    /\ verdict = [by |-> None, term |-> 0, round |-> 0, dead |-> FALSE, fromApproved |-> FALSE]
    /\ annotated = FALSE
    /\ acted = FALSE
    /\ actLog = {}

NewRound ==
    /\ round < MaxRound
    /\ round' = round + 1
    /\ approved' = FALSE
    /\ UNCHANGED <<alive, jHolder, jTerm, dHolder, dTerm, verdict, annotated, acted, actLog>>

Approve ==
    /\ ~approved
    /\ approved' = TRUE
    /\ UNCHANGED <<round, alive, jHolder, jTerm, dHolder, dTerm, verdict, annotated, acted, actLog>>

ProofOfLife ==
    /\ ~annotated
    /\ alive' = ~alive
    /\ UNCHANGED <<round, approved, jHolder, jTerm, dHolder, dTerm, verdict, annotated, acted, actLog>>

JudgeLeaseMoves ==
    /\ jTerm < MaxTerm
    /\ \E j \in Judges : jHolder' = j
    /\ jTerm' = jTerm + 1
    /\ UNCHANGED <<round, approved, alive, dHolder, dTerm, verdict, annotated, acted, actLog>>

DeciderLeaseMoves ==
    /\ dTerm < MaxTerm
    /\ \E d \in Deciders : dHolder' = d
    /\ dTerm' = dTerm + 1
    /\ UNCHANGED <<round, approved, alive, jHolder, jTerm, verdict, annotated, acted, actLog>>

WriteVerdict ==
    /\ \E j \in Judges, t \in 1..jTerm :
         /\ (GJudgeTerm => (j = jHolder /\ t = jTerm))
         /\ (GApproval => approved)
         /\ verdict' = [by |-> j, term |-> t, round |-> round, dead |-> ~alive, fromApproved |-> approved]
    /\ UNCHANGED <<round, approved, alive, jHolder, jTerm, dHolder, dTerm, annotated, acted, actLog>>

VerdictOk ==
    /\ verdict.dead
    /\ (GJudgeTerm => verdict.term = jTerm)
    /\ (GFreshVerdict => verdict.round = round)

Annotate ==
    /\ ~annotated
    /\ \E d \in Deciders, t \in 1..dTerm :
         /\ (GDeciderTerm => (d = dHolder /\ t = dTerm))
         /\ VerdictOk
         /\ (GLastLook => ~alive)
         /\ annotated' = TRUE
         /\ actLog' = actLog \cup {[d |-> d, t |-> t, current |-> (d = dHolder /\ t = dTerm),
                                     vterm |-> verdict.term, vround |-> verdict.round,
                                     okTerm |-> verdict.term = jTerm, okRound |-> verdict.round = round,
                                     approvedVerdict |-> verdict.fromApproved, liveAtAct |-> alive]}
    /\ UNCHANGED <<round, approved, alive, jHolder, jTerm, dHolder, dTerm, verdict, acted>>

FinishFence ==
    /\ annotated
    /\ ~acted
    /\ acted' = TRUE
    /\ UNCHANGED <<round, approved, alive, jHolder, jTerm, dHolder, dTerm, verdict, annotated, actLog>>

Next == NewRound \/ Approve \/ ProofOfLife \/ JudgeLeaseMoves \/ DeciderLeaseMoves \/ WriteVerdict \/ Annotate \/ FinishFence

Spec == Init /\ [][Next]_vars

OnlyCurrentDeciderActs == \A e \in actLog : e.current
OnlyCurrentVerdictUsed == \A e \in actLog : e.okTerm /\ e.okRound
OnlyApprovedVotesUsed == \A e \in actLog : e.approvedVerdict
NeverFenceAProvenLife == \A e \in actLog : ~e.liveAtAct
StartedFenceFinishes == acted => annotated

CanaryNeverActs == actLog = {}
====
