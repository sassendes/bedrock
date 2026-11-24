---- MODULE Partition ----
EXTENDS Naturals, FiniteSets

CONSTANTS Nodes, Weight, P, D, MaxCuts,
          GExclusive, GStaticTotal, GStrictMajority, GMissingIsLoss

VARIABLES sees, grantP, grantD, serves, promoted, cuts, unreadable

vars == <<sees, grantP, grantD, serves, promoted, cuts, unreadable>>

RECURSIVE SumW(_)
SumW(S) == IF S = {} THEN 0 ELSE LET n == CHOOSE x \in S : TRUE IN Weight[n] + SumW(S \ {n})

Total == SumW(Nodes)

Init ==
    /\ sees = [a \in Nodes |-> [b \in Nodes |-> TRUE]]
    /\ grantP = Nodes
    /\ grantD = {}
    /\ serves = TRUE
    /\ promoted = FALSE
    /\ cuts = 0
    /\ unreadable = FALSE

CutLink ==
    /\ cuts < MaxCuts
    /\ \E a, b \in Nodes :
         /\ a # b
         /\ sees[a][b]
         /\ sees' = [sees EXCEPT ![a][b] = FALSE]
    /\ cuts' = cuts + 1
    /\ UNCHANGED <<grantP, grantD, serves, promoted, unreadable>>

LoseTable ==
    /\ ~unreadable
    /\ unreadable' = TRUE
    /\ UNCHANGED <<sees, grantP, grantD, serves, promoted, cuts>>

PTotal == IF GStaticTotal THEN Total ELSE SumW({n \in Nodes : sees[P][n]})
DTotal == IF GStaticTotal THEN Total ELSE SumW({n \in Nodes : sees[D][n]})

PKeeps(w) == 2 * w >= PTotal
DWins(w) == IF GStrictMajority THEN 2 * w > DTotal ELSE 2 * w >= DTotal

ExpireP ==
    /\ \E n \in grantP :
         /\ n # P
         /\ ~sees[n][P] \/ ~sees[P][n]
         /\ grantP' = grantP \ {n}
         /\ serves' = IF unreadable
                         THEN (serves /\ ~GMissingIsLoss)
                         ELSE (serves /\ PKeeps(SumW(grantP \ {n})))
    /\ UNCHANGED <<sees, grantD, promoted, cuts, unreadable>>

CheckP ==
    /\ serves
    /\ GMissingIsLoss
    /\ unreadable
    /\ serves' = FALSE
    /\ UNCHANGED <<sees, grantP, grantD, promoted, cuts, unreadable>>

GrantD ==
    /\ \E n \in Nodes \ grantD :
         /\ sees[n][D]
         /\ sees[D][n]
         /\ (GExclusive => n \notin grantP)
         /\ n # P
         /\ grantD' = grantD \cup {n}
    /\ UNCHANGED <<sees, grantP, serves, promoted, cuts, unreadable>>

Promote ==
    /\ ~promoted
    /\ ~sees[D][P]
    /\ DWins(SumW(grantD))
    /\ promoted' = TRUE
    /\ UNCHANGED <<sees, grantP, grantD, serves, cuts, unreadable>>

Next == CutLink \/ LoseTable \/ ExpireP \/ CheckP \/ GrantD \/ Promote

Spec == Init /\ [][Next]_vars

NeverTwoWriters == ~(serves /\ promoted)

CanaryNeverPromoted == ~promoted
CanaryNeverFences == serves
====
