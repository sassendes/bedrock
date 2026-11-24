---- MODULE Approval ----
EXTENDS Naturals

CONSTANTS Deciders, MaxRuns, MaxFlips, VerifierCanDie, OneObject, Revokes

VARIABLES run, provenUp, flips, approvals, verifierAlive, pc, epochOf, writes, badWrite

vars == <<run, provenUp, flips, approvals, verifierAlive, pc, epochOf, writes, badWrite>>

Steps == {"idle", "fenced", "promoted"}

Init ==
    /\ run = 1
    /\ provenUp = FALSE
    /\ flips = 0
    /\ approvals = [e \in 1..MaxRuns |-> "none"]
    /\ verifierAlive = TRUE
    /\ pc = [d \in Deciders |-> "idle"]
    /\ epochOf = [d \in Deciders |-> 1]
    /\ writes = 0
    /\ badWrite = FALSE

Admitted(e) ==
    IF OneObject
    THEN e = run /\ approvals[run] = "valid"
    ELSE approvals[e] = "valid"

EvidenceFlips ==
    /\ flips < MaxFlips
    /\ provenUp' = ~provenUp
    /\ flips' = flips + 1
    /\ UNCHANGED <<run, approvals, verifierAlive, pc, epochOf, writes, badWrite>>

Approve ==
    /\ verifierAlive /\ ~provenUp /\ approvals[run] = "none"
    /\ approvals' = [approvals EXCEPT ![run] = "valid"]
    /\ UNCHANGED <<run, provenUp, flips, verifierAlive, pc, epochOf, writes, badWrite>>

Revoke ==
    /\ Revokes /\ verifierAlive /\ provenUp /\ approvals[run] = "valid"
    /\ approvals' = [approvals EXCEPT ![run] = "revoked"]
    /\ UNCHANGED <<run, provenUp, flips, verifierAlive, pc, epochOf, writes, badWrite>>

VerifierDies ==
    /\ VerifierCanDie /\ verifierAlive
    /\ verifierAlive' = FALSE
    /\ UNCHANGED <<run, provenUp, flips, approvals, pc, epochOf, writes, badWrite>>

Write(d, step) ==
    /\ Admitted(epochOf[d])
    /\ pc' = [pc EXCEPT ![d] = step]
    /\ writes' = writes + 1
    /\ badWrite' = (badWrite \/ epochOf[d] # run)
    /\ UNCHANGED <<run, provenUp, flips, approvals, verifierAlive, epochOf>>

Fence(d) == pc[d] = "idle" /\ Write(d, "fenced")

Promote(d) == pc[d] = "fenced" /\ Write(d, "promoted")

CloseRun ==
    /\ run < MaxRuns
    /\ \E d \in Deciders : pc[d] = "promoted" /\ epochOf[d] = run
    /\ approvals' = [approvals EXCEPT ![run] = IF Revokes /\ verifierAlive THEN "closed" ELSE @]
    /\ run' = run + 1
    /\ provenUp' = FALSE
    /\ \E fresh \in Deciders :
         /\ pc' = [pc EXCEPT ![fresh] = "idle"]
         /\ epochOf' = [epochOf EXCEPT ![fresh] = run + 1]
    /\ UNCHANGED <<flips, verifierAlive, writes, badWrite>>

Next ==
    \/ EvidenceFlips \/ Approve \/ Revoke \/ VerifierDies \/ CloseRun
    \/ \E d \in Deciders : Fence(d) \/ Promote(d)

Spec == Init /\ [][Next]_vars

NoWriteFromAnOldRun == ~badWrite

RevokedMeansStopped == approvals[run] = "revoked" => ~ENABLED (\E d \in Deciders : Fence(d) \/ Promote(d))

====
