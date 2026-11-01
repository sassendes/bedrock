---- MODULE Pairs ----
EXTENDS Naturals, FiniteSets

CONSTANTS Nodes, Paired, MaxFaults

Slots == IF Paired THEN {"a", "b"} ELSE {"a"}
Observers == Nodes \X Slots
Q == Cardinality(Nodes) \div 2 + 1

VARIABLES db, faulty, vote

vars == <<db, faulty, vote>>

Honest(d) == IF d = "up" THEN {"signed", "abstain"} ELSE {"down", "abstain"}
Liar(d) == IF d = "up" THEN {"signed", "unsigned", "down", "abstain", "silent"} ELSE {"unsigned", "down", "abstain", "silent"}

Init ==
    /\ db \in {"up", "down"}
    /\ faulty \in {f \in SUBSET Observers : Cardinality(f) <= MaxFaults}
    /\ vote \in [Observers -> {"signed", "unsigned", "down", "abstain", "silent"}]
    /\ \A o \in Observers : vote[o] \in (IF o \in faulty THEN Liar(db) ELSE Honest(db))

Next == UNCHANGED vars

Spec == Init /\ [][Next]_vars

NodeVote(n) ==
    IF \E s \in Slots : vote[<<n, s>>] = "signed" THEN "up"
    ELSE IF \A s \in Slots : vote[<<n, s>>] = "down" THEN "down"
    ELSE "abstain"

Up == \E n \in Nodes : NodeVote(n) = "up"
DownNodes == {n \in Nodes : NodeVote(n) = "down"}
MajorityDown == ~Up /\ Cardinality(DownNodes) >= Q

VotesNeverLie == MajorityDown => db = "down"
SignedOnlyWhenUp == Up => db = "up"

CanaryFailoverReachable == ~(MajorityDown /\ db = "down" /\ faulty = {})
CanaryLiarsExist == ~(\E o \in faulty : vote[o] = "down" /\ db = "up")
====
