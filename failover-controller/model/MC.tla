---- MODULE MC ----
EXTENDS Exchange, TLC
CONSTANTS a, b, c, d
MCNodes == {a, b, c, d}
MCOrigin == a
PeerSymmetry == Permutations({b, c, d})
====
