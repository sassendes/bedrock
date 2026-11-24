---- MODULE Exchange ----
EXTENDS Naturals, Sequences, FiniteSets

CONSTANTS Nodes, Origin, MaxHops, MaxInjections, Sees

Peers == Nodes \ {Origin}
Pairs == {p \in Nodes \X Nodes : p[1] # p[2]}

VARIABLES links, sees, msgs, seen, probes, relayed, accepted, injected, sent

vars == <<links, sees, msgs, seen, probes, relayed, accepted, injected, sent>>

Link(x, y) == <<x, y>> \in links

Req(to, path, sig) == [type |-> "req", to |-> to, path |-> path, sig |-> sig]

Ans(to, resp, nonce, result, path, rel, sig) ==
    [type |-> "ans", to |-> to, responder |-> resp, nonce |-> nonce,
     result |-> result, path |-> path, relayed |-> rel, sig |-> sig]

InPath(path, n) == \E i \in 1..Len(path) : path[i] = n

PrevHop(path, n) ==
    IF InPath(path, n)
    THEN LET i == CHOOSE i \in 1..Len(path) : path[i] = n
         IN IF i > 1 THEN path[i - 1] ELSE "none"
    ELSE path[Len(path)]

Route(n, a) ==
    IF Link(n, Origin) THEN {[a EXCEPT !.to = Origin]}
    ELSE IF ~a.relayed
    THEN {[a EXCEPT !.to = p] : p \in {q \in Peers \ {n} : Link(n, q)}}
    ELSE LET prev == PrevHop(a.path, n) IN
         IF prev # "none" /\ prev # Origin /\ Link(n, prev) THEN {[a EXCEPT !.to = prev]} ELSE {}

SendRouted(n, a, consumed, extra) ==
    /\ msgs' = (msgs \ {consumed}) \cup extra \cup Route(n, a)
    /\ sent' = sent + Cardinality(extra) + Cardinality(Route(n, a))

Init ==
    /\ links \in SUBSET Pairs
    /\ sees = [p \in Peers |-> Sees]
    /\ msgs = {Req(p, <<Origin>>, TRUE) : p \in {q \in Peers : Link(Origin, q)}}
    /\ seen = [n \in Nodes |-> IF n = Origin THEN 0 ELSE MaxHops + 1]
    /\ probes = [n \in Nodes |-> 0]
    /\ relayed = [n \in Nodes |-> {}]
    /\ accepted = {}
    /\ injected = 0
    /\ sent = Cardinality(msgs)

Drop(m) ==
    /\ msgs' = msgs \ {m}
    /\ UNCHANGED <<links, sees, seen, probes, relayed, accepted, injected, sent>>

RecvReq(m) ==
    /\ m.type = "req"
    /\ LET n == m.to IN
       IF ~m.sig \/ n = Origin \/ InPath(m.path, n) \/ Len(m.path) >= seen[n]
       THEN Drop(m)
       ELSE LET fwdPath == Append(m.path, n)
                fwd == IF Len(m.path) < MaxHops
                       THEN {Req(p, fwdPath, TRUE) : p \in {q \in Peers : ~InPath(fwdPath, q) /\ Link(n, q)}}
                       ELSE {}
                ans == Ans("none", n, "N", sees[n], m.path, FALSE, TRUE)
                first == seen[n] = MaxHops + 1
            IN /\ seen' = [seen EXCEPT ![n] = Len(m.path)]
               /\ probes' = IF first THEN [probes EXCEPT ![n] = @ + 1] ELSE probes
               /\ IF first
                  THEN SendRouted(n, ans, m, fwd)
                  ELSE /\ msgs' = (msgs \ {m}) \cup fwd
                       /\ sent' = sent + Cardinality(fwd)
               /\ UNCHANGED <<links, sees, relayed, accepted, injected>>

RecvAns(m) ==
    /\ m.type = "ans"
    /\ LET n == m.to IN
       IF ~m.sig THEN Drop(m)
       ELSE IF n = Origin
       THEN /\ msgs' = msgs \ {m}
            /\ accepted' =
                 IF m.nonce = "N" /\ ~\E r \in accepted : r.responder = m.responder
                 THEN accepted \cup {[responder |-> m.responder, result |-> m.result, nonce |-> m.nonce]}
                 ELSE accepted
            /\ UNCHANGED <<links, sees, seen, probes, relayed, injected, sent>>
       ELSE IF m.responder \in relayed[n] THEN Drop(m)
       ELSE /\ relayed' = [relayed EXCEPT ![n] = @ \cup {m.responder}]
            /\ SendRouted(n, [m EXCEPT !.relayed = TRUE], m, {})
            /\ UNCHANGED <<links, sees, seen, probes, accepted, injected>>

Deliver == \E m \in msgs : RecvReq(m) \/ RecvAns(m)

Inject(m) ==
    /\ injected < MaxInjections
    /\ injected' = injected + 1
    /\ msgs' = msgs \cup {m}
    /\ UNCHANGED <<links, sees, seen, probes, relayed, accepted, sent>>

Attack ==
    \/ \E r \in Peers, res \in {"up", "down"} : Inject(Ans(Origin, r, "N", res, <<Origin>>, FALSE, FALSE))
    \/ \E r \in Peers, res \in {"up", "down"} : Inject(Ans(Origin, r, "OLD", res, <<Origin>>, FALSE, TRUE))
    \/ \E p \in Peers : Inject(Req(p, <<Origin>>, FALSE))

Next == Deliver \/ Attack

Spec == Init /\ [][Next]_vars /\ WF_vars(Deliver)

OnlyGenuineAccepted ==
    \A r \in accepted : r.nonce = "N" /\ r.result = sees[r.responder] /\ probes[r.responder] >= 1

OneProbePerObserver == \A n \in Nodes : probes[n] <= 1

OriginNeverProbes == probes[Origin] = 0

BoundedTraffic == sent <= Cardinality(Nodes) * Cardinality(Nodes) * Cardinality(Nodes)

AlwaysStops == <>(msgs = {})

Reachable(x) ==
    \/ Link(Origin, x)
    \/ (MaxHops >= 2 /\ \E p \in Peers \ {x} : Link(Origin, p) /\ Link(p, x))

CanReturn(x) == Link(x, Origin) \/ \E p \in Peers \ {x} : Link(x, p) /\ Link(p, Origin)

Heard(x) == \E r \in accepted : r.responder = x

EveryReachablePeerIsHeard == \A x \in Peers : (Reachable(x) /\ CanReturn(x)) ~> Heard(x)

====
