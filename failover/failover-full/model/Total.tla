---- MODULE Total ----
EXTENDS Naturals

CONSTANTS Nodes, MaxChanges, GOnlyWhenWhole

VARIABLES total, reach, cut, changes

vars == <<total, reach, cut, changes>>

Init ==
    /\ total = [n \in Nodes |-> 10]
    /\ reach = Nodes
    /\ cut = FALSE
    /\ changes = 0

StartCut ==
    /\ ~cut
    /\ \E S \in SUBSET Nodes :
         /\ S # {} /\ S # Nodes
         /\ reach' = S
    /\ cut' = TRUE
    /\ UNCHANGED <<total, changes>>

Heal ==
    /\ cut
    /\ cut' = FALSE
    /\ reach' = Nodes
    /\ UNCHANGED <<total, changes>>

Change ==
    /\ changes < MaxChanges
    /\ (GOnlyWhenWhole => ~cut)
    /\ \E v \in 8..12 :
         total' = [n \in Nodes |-> IF n \in reach THEN v ELSE total[n]]
    /\ changes' = changes + 1
    /\ UNCHANGED <<reach, cut>>

Next == StartCut \/ Heal \/ Change

Spec == Init /\ [][Next]_vars

EveryNodeSameTotal == \A a, b \in Nodes : total[a] = total[b]

CanaryNeverChanged == \A n \in Nodes : total[n] = 10
====
