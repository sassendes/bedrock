---
title: "What bedrock is"
linkTitle: "What bedrock is"
weight: 1
description: "The problem bedrock solves, who it is for and what it does today."
---

bedrock is a self-hosted Kubernetes platform for managed service providers (MSPs). An MSP runs IT for other organisations: clinics, law firms, small banks, local government. bedrock gives each of those organisations, called a **tenant**, a complete Kubernetes cluster of its own, already hardened, monitored and backed up.

## The problem

Most hosting platforms put many customers on one shared cluster and separate them with namespaces. That is cheap, but the separation is a set of rules inside one system. A mistake in those rules, a noisy neighbour or a compromised control plane affects every customer at once.

Regulated organisations often need more than that. A clinic or a bank has to show an auditor where its data lives, who can reach it and what happens when something fails. "We trust the namespace rules" is a weak answer.

## The approach

bedrock gives every tenant its own cluster on dedicated nodes. Nothing is shared between tenants: not the nodes, not the storage, not the databases, not the monitoring stack and, once the edge layer exists, not the firewall or the network either. One tenant's failure or breach stays inside that tenant.

Inside each cluster, bedrock installs the same tested stack:

| Need | What bedrock uses |
|---|---|
| Kubernetes | RKE2 with the CIS hardening profile |
| Networking and network policy | Cilium, with WireGuard between nodes |
| Admission control | Kyverno, Pod Security Admission and a native ValidatingAdmissionPolicy |
| Block storage | Longhorn |
| Object storage (S3) | Garage |
| PostgreSQL | CloudNativePG, a primary cluster and replica clusters per tenant |
| Metrics, logs, traces, dashboards | Prometheus, Loki, Tempo, Alloy, Grafana |
| Runtime threat detection | Falco |
| Event buses, when a tenant needs them | NATS, Redis, Kafka |

The tenant chooses **what** it needs: which buses, how big, which compliance regime. bedrock decides **how** each part is built. Fewer free choices means fewer combinations to test, so every tenant runs a configuration that has been tested as a whole.

## Who it is for

bedrock is written for the people who operate it: the engineers at an MSP. A tenant's own developers see a normal Kubernetes cluster and deploy their applications into their own namespaces. They do not need to know how the platform underneath is wired.

## What exists today

bedrock is designed as a set of layers. The **tenant layer**, the cluster each tenant receives, is built, deploys from empty machines with one command and passes the CIS Kubernetes benchmark. The layers above it, which create tenants on demand and manage them as a fleet, are designed but not built yet. The [tier model]({{< relref "tiers" >}}) explains the full design and the [roadmap]({{< relref "/roadmap" >}}) shows the order of work.

Everything in these docs describes the tenant layer unless a page says otherwise.
