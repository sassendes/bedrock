---- MODULE Votes ----
EXTENDS Naturals, FiniteSets

CONSTANTS Observers, MaxRound, MaxRestarts, MaxRestores,
          GNonce, GRound, GOwner, GUniqueNonce

VARIABLES round, nonce, nextNonce, inc, rows, snapshot, restarts, restores

vars == <<round, nonce, nextNonce, inc, rows, snapshot, restarts, restores>>

Init ==
    /\ round = 1
    /\ nonce = [o \in Observers |-> 0]
    /\ nextNonce = 1
    /\ inc = [o \in Observers |-> 0]
    /\ rows = {}
    /\ snapshot = {}
    /\ restarts = 0
    /\ restores = 0

NewRound ==
    /\ round < MaxRound
    /\ round' = round + 1
    /\ UNCHANGED <<nonce, nextNonce, inc, rows, snapshot, restarts, restores>>

Vote ==
    /\ \E o \in Observers, w \in Observers :
         /\ (GOwner => w = o)
         /\ ~\E r \in rows : r.o = o /\ r.n = nonce[o] /\ r.r = round
         /\ rows' = rows \cup {[o |-> o, n |-> nonce[o], i |-> inc[o], r |-> round, by |-> w]}
    /\ UNCHANGED <<round, nonce, nextNonce, inc, snapshot, restarts, restores>>

Restart ==
    /\ restarts < MaxRestarts
    /\ \E o \in Observers :
         /\ nonce' = [nonce EXCEPT ![o] = IF GUniqueNonce THEN nextNonce ELSE 0]
         /\ inc' = [inc EXCEPT ![o] = @ + 1]
    /\ nextNonce' = nextNonce + 1
    /\ restarts' = restarts + 1
    /\ UNCHANGED <<round, rows, snapshot, restores>>

Snapshot ==
    /\ snapshot' = rows
    /\ UNCHANGED <<round, nonce, nextNonce, inc, rows, restarts, restores>>

Retention ==
    /\ \E r \in rows :
         /\ r.r + 2 < round
         /\ rows' = rows \ {r}
    /\ UNCHANGED <<round, nonce, nextNonce, inc, snapshot, restarts, restores>>

Restore ==
    /\ restores < MaxRestores
    /\ rows' = rows \cup snapshot
    /\ restores' = restores + 1
    /\ UNCHANGED <<round, nonce, nextNonce, inc, snapshot, restarts>>

Fresh(r) == IF GRound THEN r.r + 1 >= round ELSE TRUE

Counted == { r \in rows : Fresh(r) /\ (GNonce => r.n = nonce[r.o]) }

Next == NewRound \/ Vote \/ Restart \/ Snapshot \/ Retention \/ Restore

Spec == Init /\ [][Next]_vars

OneVotePerObserverPerRound ==
    \A o \in Observers : \A k \in 1..MaxRound :
        Cardinality({ r \in Counted : r.o = o /\ r.r = k }) <= 1

OnlyFreshCounted == \A r \in Counted : r.r + 1 >= round

OnlyOwnersCounted == \A r \in Counted : r.by = r.o

OnlyCurrentIncarnationCounted == \A r \in Counted : r.i = inc[r.o]

CanaryNothingCounted == Counted = {}
====
