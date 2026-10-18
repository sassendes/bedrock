---
title: "Namespaces"
weight: 2
description: "Every namespace in a tenant, what it holds and its Pod Security level."
---

| Namespace | Holds | Pod Security | Same-namespace traffic |
|---|---|---|---|
| `application` | Prometheus, Alertmanager, Grafana, Loki, Tempo, OpenCost, kube-state-metrics, blackbox exporter | restricted | Per component only |
| `node-agents` | Alloy, node-exporter | privileged | None |
| `cluster-1` | Primary PostgreSQL cluster and its pooler | restricted | Allowed |
| `cluster-2` | Live replica cluster and its pooler | restricted | Allowed |
| `cluster-3` | Delayed replica cluster and its pooler | restricted | Allowed |
| `cnpg-system` | CloudNativePG operator | set by the upstream bundle | Allowed |
| `garage` | Garage and its init job | restricted | Per component only |
| `longhorn-system` | Longhorn | privileged | Allowed |
| `kyverno` | Kyverno controllers | set by the upstream bundle | Allowed |
| `prometheus-operator` | prometheus-operator | restricted | Allowed |
| `falco` | Falco | privileged | Allowed |
| `reloader` | Reloader | restricted | None |
| `nats` | NATS JetStream, when enabled | restricted | Allowed |
| `redis` | Redis and Sentinel, when enabled | restricted | Allowed |
| `kafka` | Strimzi and Kafka, when enabled | restricted | Allowed |
| `kube-system` | RKE2 components, Cilium, CoreDNS | set by RKE2 | Allowed |

Default service accounts in every namespace have `automountServiceAccountToken: false`. Tenant applications get their own `restricted` namespaces and never run in `application`.

## Priority classes

| Class | Value | Allowed in |
|---|---|---|
| `bedrock-storage` | 90000000 | `garage` |
| `bedrock-data` | 80000000 | `cluster-1`, `cluster-2`, `cluster-3`, `falco` |
| `bedrock-workload` | 50000000 | any |
| `bedrock-default` | 10000000 | any, the global default |
| `system-cluster-critical`, `system-node-critical` | Kubernetes defaults | `kube-system`, `kyverno` |
