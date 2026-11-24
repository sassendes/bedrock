---- MODULE Timing ----
EXTENDS Naturals

CONSTANTS MaxT, TSelf, Drift, MaxDelay, GSelfFence, GCountFromSend, GSidecarCrash

VARIABLES now, cut, pgUp, sidecar, sentAt, ackAt, skew

vars == <<now, cut, pgUp, sidecar, sentAt, ackAt, skew>>

Init ==
    /\ now = 0
    /\ cut = FALSE
    /\ pgUp = TRUE
    /\ sidecar = "alive"
    /\ sentAt = 0
    /\ ackAt = 0
    /\ skew \in 0..(2 * Drift)

Base == IF GCountFromSend THEN sentAt ELSE ackAt

Elapsed == IF now + Drift >= Base + skew THEN (now + Drift) - (Base + skew) ELSE 0

Beat == ~cut /\ pgUp /\ sidecar = "alive"

FenceDue == GSelfFence /\ cut /\ sidecar = "alive" /\ pgUp /\ Elapsed >= TSelf

Tick ==
    /\ now < MaxT
    /\ ~FenceDue
    /\ now' = now + 1
    /\ IF Beat
         THEN \E a \in 0..MaxDelay : sentAt' = now /\ ackAt' = now + a
         ELSE UNCHANGED <<sentAt, ackAt>>
    /\ UNCHANGED <<cut, pgUp, sidecar, skew>>

Cut ==
    /\ ~cut
    /\ cut' = TRUE
    /\ UNCHANGED <<now, pgUp, sidecar, sentAt, ackAt, skew>>

SelfFence ==
    /\ FenceDue
    /\ pgUp' = FALSE
    /\ UNCHANGED <<now, cut, sidecar, sentAt, ackAt, skew>>

SidecarDies ==
    /\ GSidecarCrash
    /\ sidecar = "alive"
    /\ sidecar' = "dead"
    /\ UNCHANGED <<now, cut, pgUp, sentAt, ackAt, skew>>

Next == Tick \/ Cut \/ SelfFence \/ SidecarDies

Spec == Init /\ [][Next]_vars

WritesStopInTime == (cut /\ pgUp /\ sidecar = "alive") => now <= sentAt + TSelf + Drift

CanaryNeverSelfFences == pgUp \/ sidecar = "dead"
CanaryDeadSidecarKeepsWriting == ~(cut /\ pgUp /\ sidecar = "dead" /\ now > sentAt + TSelf + Drift)
====
