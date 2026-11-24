---- MODULE MCDecider ----
EXTENDS Decider, TLC
CONSTANTS d1, d2
MCDeciders == {d1, d2}
DeciderSymmetry == Permutations(MCDeciders)
====
