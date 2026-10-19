---
title: "Resources and priorities"
weight: 10
description: "CPU and memory requests and limits for every platform workload, and the priority class each runs at."
---

Every platform container has requests and limits. Requests are what the scheduler reserves; limits are the most the container can use before it is throttled, or killed for memory. Replica counts are left out, because they follow the number of nodes.

## Priority classes

When a node runs out of room, Kubernetes evicts lower priority pods first. bedrock defines four classes in `platform/priorityclasses.yaml`, and the `restrict-priority-classes` policy stops anything outside their namespaces from claiming the higher ones.

| Class | Value | Reserved for |
|---|---|---|
| `bedrock-storage` | 90000000 | Garage |
| `bedrock-data` | 80000000 | Database clusters, their poolers and Falco |
| `bedrock-workload` | 50000000 | Event buses |
| `bedrock-default` | 10000000 | Everything else. This is the global default. |

Kubernetes components and Kyverno's admission controller run at the built-in system classes, above all of these.

## Storage and data

| Workload | Container | Requests | Limits | Priority |
|---|---|---|---|---|
| Garage | `garage` | 100m, 256Mi | 1 CPU, 1Gi | `bedrock-storage` |
| Database clusters | `postgres` | 100m, 256Mi | 1 CPU, 1Gi | `bedrock-data` |
| Falco | `falco` | 100m, 256Mi | 1 CPU, 1Gi | `bedrock-data` |

## Monitoring

| Workload | Container | Requests | Limits |
|---|---|---|---|
| Prometheus | `prometheus` | 100m, 400Mi | 1 CPU, 1Gi |
| Alertmanager | `alertmanager` | 25m, 64Mi | 250m, 128Mi |
| Loki | `loki` | 100m, 128Mi | 500m, 512Mi |
| Tempo | `tempo` | 250m, 512Mi | 1 CPU, 2Gi |
| Grafana | `grafana` | 50m, 128Mi | 500m, 256Mi |
| Grafana | `sc-dashboards` | 10m, 64Mi | 250m, 256Mi |
| kube-state-metrics | `kube-state-metrics` | 50m, 64Mi | 500m, 256Mi |
| Blackbox exporter | `blackbox-exporter` | 25m, 32Mi | 200m, 128Mi |
| OpenCost | `opencost` | 10m, 64Mi | 500m, 1Gi |
| OpenCost | `opencost-ui` | 10m, 32Mi | 100m, 128Mi |
| Alloy | `alloy` | 50m, 128Mi | 500m, 256Mi |
| node-exporter | `node-exporter` | 50m, 32Mi | 200m, 128Mi |

## Platform

| Workload | Container | Requests | Limits |
|---|---|---|---|
| Reloader | `reloader` | 10m, 64Mi | 200m, 128Mi |

## Event buses

These run only when turned on. All run at `bedrock-workload`.

| Workload | Container | Requests | Limits |
|---|---|---|---|
| NATS | `nats` | 500m, 1Gi | 2 CPU, 2Gi |
| Redis | `redis` | 250m, 512Mi | 1 CPU, 1Gi |
| Redis | `sentinel` | 50m, 64Mi | 200m, 128Mi |
| Kafka | broker | 500m, 2Gi | 2 CPU, 3Gi |

## Changing a value

Edit the `resources` block in the component's file and apply it. In `application` and `node-agents`, the `require-resource-limits` policy rejects any container without CPU and memory limits, so a limit can be changed but never removed.
