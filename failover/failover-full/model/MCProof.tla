---- MODULE MCProof ----
EXTENDS Proof, TLC
CONSTANTS o1, o2, o3
MCObservers == {o1, o2, o3}
MCPopped == {o3}
MCPoppedMajority == {o2, o3}
====
