---- MODULE MCPartition ----
EXTENDS Partition, TLC

MCNodes == {"n1", "n2", "n3", "n4"}
MCWeight == [n \in MCNodes |-> IF n = "n2" THEN 3 ELSE 1]
MCP == "n1"
MCD == "n4"
====
