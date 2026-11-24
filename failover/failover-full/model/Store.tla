---- MODULE Store ----
EXTENDS Naturals, FiniteSets

CONSTANTS Nodes, Ring, Writers, Home, Sidecar, MaxCh, MaxDeaths, MaxPgFlips, Keep,
          GOwner, GCommitFirst, GChallenge, GAnyAlive, GRetainFresh, GCopyFirst

Beaters == Writers \cup {"pg"}
HomeOf(w) == IF w = "pg" THEN Home[Sidecar] ELSE Home[w]

VARIABLES ch, alive, ready, board, pgUp, deaths, pgFlips, held

vars == <<ch, alive, ready, board, pgUp, deaths, pgFlips, held>>

Init ==
    /\ ch = 1
    /\ alive = [n \in Nodes |-> TRUE]
    /\ ready = [n \in Nodes |-> TRUE]
    /\ board = [n \in Nodes |-> {}]
    /\ pgUp = TRUE
    /\ deaths = 0
    /\ pgFlips = 0
    /\ held = {}

Fresh(b) == b.ch + 1 >= ch
Recent(b) == b.ch + Keep >= ch

Tick ==
    /\ ch < MaxCh
    /\ ch' = ch + 1
    /\ UNCHANGED <<alive, ready, board, pgUp, deaths, pgFlips, held>>

Write(n, b) ==
    /\ board' = [board EXCEPT ![n] = @ \cup {b}]
    /\ UNCHANGED <<ch, alive, ready, pgUp, deaths, pgFlips, held>>

Beat(w) ==
    /\ w \in Writers
    /\ alive[Home[w]] /\ ready[Home[w]]
    /\ \E t \in Writers :
         /\ (GOwner => t = w)
         /\ ~\E b \in board[Home[w]] : b.w = t /\ b.ch = ch
         /\ Write(Home[w], [w |-> t, ch |-> ch, by |-> w, real |-> TRUE])

PgBeat ==
    /\ alive[Home[Sidecar]] /\ ready[Home[Sidecar]]
    /\ (GCommitFirst => pgUp)
    /\ ~\E b \in board[Home[Sidecar]] : b.w = "pg" /\ b.ch = ch
    /\ Write(Home[Sidecar], [w |-> "pg", ch |-> ch, by |-> Sidecar, real |-> pgUp])

PgFlips ==
    /\ pgFlips < MaxPgFlips
    /\ pgUp' = ~pgUp
    /\ pgFlips' = pgFlips + 1
    /\ UNCHANGED <<ch, alive, ready, board, deaths, held>>

Targets(n) == IF GAnyAlive THEN {m \in Nodes \ {n} : alive[m]}
              ELSE {m \in {Ring[n]} : alive[m]}

Replicate(n) ==
    /\ alive[n] /\ ready[n]
    /\ \E m \in Targets(n) :
         /\ \E b \in board[n] : Recent(b) /\ b \notin board[m]
         /\ board' = [board EXCEPT ![m] = @ \cup {b \in board[n] : Recent(b)}]
         /\ held' = held \cup {b \in board[n] : Recent(b)}
    /\ UNCHANGED <<ch, alive, ready, pgUp, deaths, pgFlips>>

Die(n) ==
    /\ deaths < MaxDeaths
    /\ alive[n]
    /\ alive' = [alive EXCEPT ![n] = FALSE]
    /\ ready' = [ready EXCEPT ![n] = FALSE]
    /\ deaths' = deaths + 1
    /\ UNCHANGED <<ch, board, pgUp, pgFlips, held>>

Return(n) ==
    /\ ~alive[n]
    /\ alive' = [alive EXCEPT ![n] = TRUE]
    /\ ready' = [ready EXCEPT ![n] = ~GCopyFirst]
    /\ UNCHANGED <<ch, board, pgUp, deaths, pgFlips, held>>

CopyIn(n) ==
    /\ alive[n] /\ ~ready[n]
    /\ \E m \in Nodes \ {n} :
         /\ alive[m] /\ ready[m]
         /\ board' = [board EXCEPT ![n] = @ \cup board[m]]
    /\ ready' = [ready EXCEPT ![n] = TRUE]
    /\ UNCHANGED <<ch, alive, pgUp, deaths, pgFlips, held>>

Retain(n) ==
    /\ alive[n]
    /\ \E b \in board[n] :
         /\ (GRetainFresh => ~Recent(b))
         /\ board' = [board EXCEPT ![n] = @ \ {b}]
    /\ UNCHANGED <<ch, alive, ready, pgUp, deaths, pgFlips, held>>

Next ==
    \/ Tick \/ PgBeat \/ PgFlips
    \/ \E w \in Writers : Beat(w)
    \/ \E n \in Nodes : Replicate(n) \/ Die(n) \/ Return(n) \/ CopyIn(n) \/ Retain(n)

Fairness ==
    /\ \A n \in Nodes : WF_vars(Replicate(n))
    /\ \A n \in Nodes : WF_vars(CopyIn(n))

Spec == Init /\ [][Next]_vars
LiveSpec == Spec /\ Fairness

ClerkSees(w) ==
    IF alive[HomeOf(w)] /\ ready[HomeOf(w)]
    THEN {b \in board[HomeOf(w)] : b.w = w /\ (GChallenge => Fresh(b))}
    ELSE {}

OnlyOwnersWrite == \A n \in Nodes : \A b \in board[n] :
    IF b.w = "pg" THEN b.by = Sidecar ELSE b.by = b.w

PgBeatMeansCommitted == \A b \in ClerkSees("pg") : b.real

ClerkNeverTakesStale == \A w \in Beaters : \A b \in ClerkSees(w) : Fresh(b)

ReplicatedFreshSurvives ==
    \A b \in held : Recent(b) => \E n \in Nodes : alive[n] /\ b \in board[n]

ReadyNodesCaughtUp ==
    <>[](\A n, m \in Nodes :
           (alive[n] /\ ready[n] /\ alive[m] /\ ready[m]) =>
             {b \in board[n] : Recent(b)} = {b \in board[m] : Recent(b)})

CaughtUpWhenServing ==
    [][\A n \in Nodes :
         (~ready[n] /\ ready'[n]) =>
           \/ ~\E m \in Nodes \ {n} : alive[m] /\ ready[m]
           \/ \E m \in Nodes \ {n} : alive[m] /\ ready[m] /\ board[m] \subseteq board'[n]]_vars

CanaryNothingReplicated == held = {}
CanaryPgNeverDown == pgUp
====
