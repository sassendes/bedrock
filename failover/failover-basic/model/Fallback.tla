---- MODULE Fallback ----
EXTENDS Naturals, FiniteSets

CONSTANTS Observers, LockAfter, MaxResets, MaxEvents, MassBrake, Guarded

VARIABLES voters, lostFor, locked, provenUp, drunk, resets, promoted, paged, events, resetWhileMassDrunk

vars == <<voters, lostFor, locked, provenUp, drunk, resets, promoted, paged, events, resetWhileMassDrunk>>

Q == Cardinality(Observers) \div 2 + 1

Init ==
    /\ voters = Observers
    /\ lostFor = 0
    /\ locked = FALSE
    /\ provenUp = TRUE
    /\ drunk = {}
    /\ resets = 0
    /\ promoted = FALSE
    /\ paged = FALSE
    /\ events = 0
    /\ resetWhileMassDrunk = FALSE

QuorumLost == Cardinality(voters) < Q

ObserverDrops ==
    /\ events < MaxEvents
    /\ \E o \in voters : voters' = voters \ {o}
    /\ events' = events + 1
    /\ UNCHANGED <<lostFor, locked, provenUp, drunk, resets, promoted, paged, resetWhileMassDrunk>>

ObserverBack ==
    /\ \E o \in Observers \ voters : voters' = voters \cup {o}
    /\ UNCHANGED <<lostFor, locked, provenUp, drunk, resets, promoted, paged, events, resetWhileMassDrunk>>

GetsDrunk ==
    /\ events < MaxEvents
    /\ \E o \in voters \ drunk : drunk' = drunk \cup {o}
    /\ events' = events + 1
    /\ UNCHANGED <<voters, lostFor, locked, provenUp, resets, promoted, paged, resetWhileMassDrunk>>

DBChanges ==
    /\ events < MaxEvents
    /\ provenUp' = ~provenUp
    /\ events' = events + 1
    /\ UNCHANGED <<voters, lostFor, locked, drunk, resets, promoted, paged, resetWhileMassDrunk>>

Tick ==
    /\ lostFor' = IF QuorumLost THEN (IF lostFor < LockAfter THEN lostFor + 1 ELSE lostFor) ELSE 0
    /\ locked' = ((QuorumLost /\ lostFor + 1 >= LockAfter) \/ (locked /\ QuorumLost))
    /\ paged' = (paged \/ (QuorumLost /\ lostFor + 1 >= LockAfter))
    /\ UNCHANGED <<voters, provenUp, drunk, resets, promoted, events, resetWhileMassDrunk>>

Reset ==
    /\ provenUp /\ ~locked /\ drunk # {} /\ resets < MaxResets
    /\ (~MassBrake \/ Cardinality(drunk) < Q)
    /\ resetWhileMassDrunk' = (resetWhileMassDrunk \/ Cardinality(drunk) >= Q)
    /\ \E o \in drunk : drunk' = drunk \ {o}
    /\ resets' = resets + 1
    /\ UNCHANGED <<voters, lostFor, locked, provenUp, promoted, paged, events>>

MassDrunkPages ==
    /\ Cardinality(drunk) >= Q /\ ~paged
    /\ paged' = TRUE
    /\ UNCHANGED <<voters, lostFor, locked, provenUp, drunk, resets, promoted, events, resetWhileMassDrunk>>

Promote ==
    /\ (~Guarded \/ (~locked /\ ~QuorumLost)) /\ ~provenUp /\ ~promoted
    /\ (~Guarded \/ Cardinality(voters \ drunk) >= Q)
    /\ promoted' = TRUE
    /\ UNCHANGED <<voters, lostFor, locked, provenUp, drunk, resets, paged, events, resetWhileMassDrunk>>

Next == ObserverDrops \/ ObserverBack \/ GetsDrunk \/ DBChanges \/ Tick \/ Reset \/ MassDrunkPages \/ Promote

Spec == Init /\ [][Next]_vars

LockedNeverPromotes == [][promoted' /\ ~promoted => ~locked /\ ~QuorumLost]_vars

NoMassReset == ~resetWhileMassDrunk

ResetsBounded == resets <= MaxResets

====
