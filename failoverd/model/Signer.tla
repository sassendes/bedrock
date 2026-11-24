---- MODULE Signer ----
EXTENDS Naturals

CONSTANTS BindName, BindIP, MaxSteps

IPs == {"ip1", "ip2", "ip3"}
Observers == {"o1", "o2"}
NoReport == [kind |-> "none", name |-> "", ip |-> ""]

VARIABLES tAlive, tIP, apiIP, sAlive, sIP, obsIP, report, approved, aliveAtApproval, steps

vars == <<tAlive, tIP, apiIP, sAlive, sIP, obsIP, report, approved, aliveAtApproval, steps>>

Init ==
    /\ tAlive = TRUE /\ tIP = "ip1" /\ apiIP = "ip1"
    /\ sAlive \in BOOLEAN /\ sIP = "ip3"
    /\ obsIP = [o \in Observers |-> "ip1"]
    /\ report = [o \in Observers |-> NoReport]
    /\ approved = FALSE /\ aliveAtApproval = FALSE
    /\ steps = 0

Tick == steps < MaxSteps /\ steps' = steps + 1

TDies ==
    /\ Tick /\ tAlive
    /\ tAlive' = FALSE
    /\ UNCHANGED <<tIP, apiIP, sAlive, sIP, obsIP, report, approved, aliveAtApproval>>

TMovesInside ==
    /\ Tick /\ tAlive
    /\ \E ip \in IPs \ {tIP, sIP} : tIP' = ip
    /\ UNCHANGED <<tAlive, apiIP, sAlive, sIP, obsIP, report, approved, aliveAtApproval>>

ApiCatchesUp ==
    /\ Tick /\ apiIP # tIP
    /\ apiIP' = tIP
    /\ UNCHANGED <<tAlive, tIP, sAlive, sIP, obsIP, report, approved, aliveAtApproval>>

SiblingTakesTheOldIP ==
    /\ Tick /\ ~tAlive /\ sAlive /\ sIP # tIP
    /\ sIP' = tIP
    /\ UNCHANGED <<tAlive, tIP, apiIP, sAlive, obsIP, report, approved, aliveAtApproval>>

Refresh(o) ==
    /\ Tick /\ obsIP[o] # apiIP
    /\ obsIP' = [obsIP EXCEPT ![o] = apiIP]
    /\ UNCHANGED <<tAlive, tIP, apiIP, sAlive, sIP, report, approved, aliveAtApproval>>

Probe(o) ==
    /\ Tick
    /\ report' = [report EXCEPT ![o] =
         IF tAlive /\ obsIP[o] = tIP THEN [kind |-> "A", name |-> "T", ip |-> tIP]
         ELSE IF sAlive /\ obsIP[o] = sIP THEN [kind |-> "A", name |-> "S", ip |-> sIP]
         ELSE [kind |-> "down", name |-> "", ip |-> ""]]
    /\ UNCHANGED <<tAlive, tIP, apiIP, sAlive, sIP, obsIP, approved, aliveAtApproval>>

ValidUp(r) ==
    /\ r.kind = "A"
    /\ (~BindName \/ r.name = "T")
    /\ (~BindIP \/ r.ip = apiIP)

Approve ==
    /\ ~approved
    /\ \A o \in Observers : ~ValidUp(report[o])
    /\ \E o \in Observers : report[o].kind = "down"
    /\ approved' = TRUE
    /\ aliveAtApproval' = tAlive
    /\ UNCHANGED <<tAlive, tIP, apiIP, sAlive, sIP, obsIP, report, steps>>

Next == TDies \/ TMovesInside \/ ApiCatchesUp \/ SiblingTakesTheOldIP \/ Approve
        \/ \E o \in Observers : Refresh(o) \/ Probe(o)

Spec == Init /\ [][Next]_vars

SiblingNeverCountsAsUp == \A o \in Observers : ValidUp(report[o]) => report[o].name = "T"
RealUpNeverDiscarded == \A o \in Observers : (report[o].kind = "A" /\ report[o].name = "T") => ValidUp(report[o])

CanaryNeverApproves == ~approved
CanaryNeverSeesTheSibling == \A o \in Observers : report[o].name # "S"
====
