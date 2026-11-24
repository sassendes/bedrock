<div align="center">

<img src="https://raw.githubusercontent.com/sassendes/bedrock/main/docs/static/images/logo.svg" width="96" alt="bedrock">

# failover

**Cross cluster failover for CloudNativePG**

[![TLA+](https://img.shields.io/badge/TLA%2B-23_models-2b2a27?style=flat-square&labelColor=151413)](failover-full/model)
[![CloudNativePG](https://img.shields.io/badge/CloudNativePG-1.30-2b2a27?style=flat-square&labelColor=151413)](https://cloudnative-pg.io)
[![License](https://img.shields.io/badge/license-Apache_2.0-2b2a27?style=flat-square&labelColor=151413)](https://github.com/sassendes/bedrock/blob/main/LICENSE)

</div>

<br>

CNPG handles failover inside a cluster. This moves a tenant's database to another CNPG cluster when the whole primary cluster can no longer write.

## [failover-basic](failover-basic)

Postgres commits a signed proof of life every round. Observers on every node check for it and vote. When a majority agrees the primary is gone, the decider fences it and promotes the live replica. Admission checks every write the decider makes. Tested live with RPO 0 and RTO 66 s.

## [failover-full](failover-full)

A superset of failover-basic with more complexity and more assurance. Evidence is graded from many independent viewpoints. A cut off primary fences itself. Every verdict passes through a clerk, a judge and a bailiff before the decider can act. Each part of the design is a TLA+ model checked with TLC.

```sh
./failover-full/model/run.sh
```
