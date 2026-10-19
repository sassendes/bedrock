---
title: "Disruption budgets"
weight: 11
description: "The PodDisruptionBudgets that limit how many pods a drain or upgrade can take down at once."
---

A PodDisruptionBudget tells Kubernetes how many pods of a workload must stay up during voluntary disruptions, such as `kubectl drain` before node maintenance. A drain waits rather than break a budget. Budgets do not protect against a node failing on its own.

Every budget in bedrock allows one pod down at a time. Each one is written as a `minAvailable` one below the workload's replica count, so if the replica count changes, the budget must change with it.

| Budget | Namespace | Selects | File |
|---|---|---|---|
| `kyverno-admission-controller` | `kyverno` | Kyverno's admission controller | `kyverno/admission-pdb.yaml` |
| `prometheus` | `application` | Prometheus | `observability/prometheus/pdb.yaml` |
| `loki` | `application` | Loki | `observability/loki/loki-pdb.yaml` |
| `<cluster>-pooler-rw` | each database namespace | That cluster's PgBouncer pooler | `database/pooler.yaml` |
| `nats` | `nats` | NATS | `event-buses/nats.yaml` |
| `redis` | `redis` | Redis | `event-buses/redis.yaml` |

## Workloads managed by an operator

Some workloads get their budgets from their operator instead of from the repository:

| Workload | Budget comes from |
|---|---|
| Database clusters | CloudNativePG creates one for the primary and one for the replicas |
| Longhorn volumes | Longhorn protects the last healthy replica of each volume during a drain |
| Kafka | Strimzi |

## Workloads with no budget

Tempo, Grafana, Alertmanager, OpenCost and Reloader run a single pod, so a budget would block every drain. They lose nothing important while they restart. The DaemonSets, Alloy, node-exporter and Falco, are not evicted by a drain.

## Check a budget

```sh
kubectl get pdb -A
```

`ALLOWED DISRUPTIONS` must be at least 1 before a node is drained. If it is 0, a pod of that workload is already down, and draining would wait until it is back.
