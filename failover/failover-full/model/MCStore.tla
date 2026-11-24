---- MODULE MCStore ----
EXTENDS Store, TLC

MCNodes == {"n1", "n2", "n3"}
MCRing == [n \in MCNodes |-> CASE n = "n1" -> "n2" [] n = "n2" -> "n3" [] OTHER -> "n1"]
MCWriters == {"o1", "sc"}
MCHome == [w \in MCWriters |-> CASE w = "o1" -> "n1" [] w = "o2" -> "n2" [] OTHER -> "n3"]
====
