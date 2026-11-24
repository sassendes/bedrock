---- MODULE Pick ----
EXTENDS Naturals, FiniteSets

CONSTANTS Replicas, MaxLsn, GSysId, GTimeline, GStreaming, GReady, GPooler, GDisk, GDelayed, GHighest, GStreamLsn

VARIABLES r, picked, paged

vars == <<r, picked, paged>>

None == "none"

Rec == [sysOk : BOOLEAN, tlOk : BOOLEAN, streaming : BOOLEAN, ready : BOOLEAN,
        pooler : BOOLEAN, disk : BOOLEAN, delayed : BOOLEAN,
        lsn : 0..MaxLsn, statusLsn : 0..MaxLsn]

Init ==
    /\ r \in [Replicas -> Rec]
    /\ picked = None
    /\ paged = FALSE

Passes(c) ==
    /\ (GSysId => r[c].sysOk)
    /\ (GTimeline => r[c].tlOk)
    /\ (GStreaming => r[c].streaming)
    /\ (GReady => r[c].ready)
    /\ (GPooler => r[c].pooler)
    /\ (GDisk => r[c].disk)
    /\ (GDelayed => ~r[c].delayed)

Lsn(c) == IF GStreamLsn THEN r[c].lsn ELSE r[c].statusLsn

Good == { c \in Replicas : Passes(c) }

Pick ==
    /\ picked = None
    /\ ~paged
    /\ \E c \in Good :
         /\ (GHighest => \A o \in Good : Lsn(o) <= Lsn(c))
         /\ picked' = c
    /\ UNCHANGED <<r, paged>>

Page ==
    /\ picked = None
    /\ ~paged
    /\ Good = {}
    /\ paged' = TRUE
    /\ UNCHANGED <<r, picked>>

Next == Pick \/ Page

Spec == Init /\ [][Next]_vars

TrulyPasses(c) ==
    /\ r[c].sysOk /\ r[c].tlOk /\ r[c].streaming /\ r[c].ready
    /\ r[c].pooler /\ r[c].disk /\ ~r[c].delayed

PickedTrulyPasses == picked # None => TrulyPasses(picked)
PickedHasHighestRealLsn ==
    picked # None => \A o \in Replicas : TrulyPasses(o) => r[o].lsn <= r[picked].lsn
NeverPagesWithAGoodOne == paged => \A c \in Replicas : ~TrulyPasses(c)

CanaryNeverPicks == picked = None
CanaryNeverPages == ~paged
====
