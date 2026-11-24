<div align="center">

<img src="https://raw.githubusercontent.com/sassendes/bedrock/main/docs/static/images/logo.svg" width="96" alt="bedrock">

# failoverd

**Cross cluster failover for CloudNativePG**

[![TLA+](https://img.shields.io/badge/TLA%2B-23_models-2b2a27?style=flat-square&labelColor=151413)](model)
[![TLC](https://img.shields.io/badge/TLC-135_configurations-2b2a27?style=flat-square&labelColor=151413)](model)
[![CloudNativePG](https://img.shields.io/badge/CloudNativePG-1.30-2b2a27?style=flat-square&labelColor=151413)](https://cloudnative-pg.io)
[![License](https://img.shields.io/badge/license-Apache_2.0-2b2a27?style=flat-square&labelColor=151413)](https://github.com/sassendes/bedrock/blob/main/LICENSE)

</div>

<br>

failoverd moves a tenant's database to another CNPG cluster when the primary cluster can no longer write. CNPG handles failover inside a cluster. failoverd acts only when the whole cluster is gone and CNPG already had its chance.

It is the second version of [`failover-basic`](../failover-basic) and keeps everything the first one proved.

## How it decides

Observers on every node collect evidence. Each observer has a clerk in its pod that checks the evidence is real. The judge weighs the evidence and reaches a verdict. The bailiff checks the verdict and grants the decider its permissions for that one run. The decider fences the old primary and promotes the live replica with the highest LSN.

## Rules it never breaks

- It never promotes past a node it cannot reach.
- It never fences a database that sent a fresh proof of life.
- One run at a time.
- No failback.

## Models

The design is model checked in TLA+ in [`model/`](model). Run them with `./model/run.sh`.
