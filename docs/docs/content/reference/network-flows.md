---
title: "Network flows"
weight: 3
description: "Every connection allowed between components in a tenant, by source, destination and port."
---

Everything not listed here is dropped. Each row is allowed in both the source's and the destination's policy. Policies live in `netpols/`, one file per namespace, plus `cluster-default-deny.yaml`.

## Allowed for every pod

| From | To | Port |
|---|---|---|
| Any pod | CoreDNS in `kube-system` | 53 TCP and UDP |
| The node (kubelet probes) | Any pod | any |

## Metrics collection

| From | To | Port |
|---|---|---|
| Prometheus | kube-state-metrics | 8080 |
| Prometheus | OpenCost | 9003 |
| Prometheus | blackbox exporter | 9115 |
| Prometheus | Alertmanager | 9093 |
| Prometheus | Loki | 3100 |
| Prometheus | Tempo | 3200 |
| Prometheus | Falco | 8765 |
| Prometheus | Database clusters | 9187, 9127 |
| Prometheus | Kyverno | 8000 |
| Prometheus | Longhorn manager | 9500 |
| Prometheus | Garage admin | 3903 |
| Prometheus | CloudNativePG operator | 8080 |
| Prometheus | Nodes: kubelet, node-exporter, etcd | 10250, 9100, 2381 |
| Prometheus | Kubernetes API | any |

## Logs, traces and alerts

| From | To | Port |
|---|---|---|
| Alloy | Loki | 3100 |
| Alloy | Tempo (OTLP) | 4317 |
| Loki | Loki (ring and gossip) | 3100, 9095, 7946 TCP and UDP |
| Loki | Alertmanager | 9093 |
| Loki | Garage S3 | 3900 |
| Tempo | Garage S3 | 3900 |

## Dashboards

| From | To | Port |
|---|---|---|
| Grafana | Prometheus | 9090 |
| Grafana | Loki | 3100 |
| Grafana | Tempo | 3200 |
| Grafana sidecar | Kubernetes API | any |
| OpenCost | Prometheus | 9090 |
| OpenCost | Kubernetes API | any |

## Probes

| From | To | Port |
|---|---|---|
| blackbox exporter | Grafana | 3000 |
| blackbox exporter | Prometheus | 9090 |
| blackbox exporter | Alertmanager | 9093 |
| blackbox exporter | Loki | 3100 |
| blackbox exporter | Tempo | 3200 |
| blackbox exporter | Garage admin | 3903 |
| blackbox exporter | Database clusters | 5432 |
| blackbox exporter | Kyverno and CloudNativePG webhooks | 9443 |
| blackbox exporter | Kubernetes API (certificate check) | any |

## Databases

| From | To | Port |
|---|---|---|
| cluster-2 and cluster-3 | cluster-1 (streaming replication) | 5432 |
| cluster-1, cluster-2, cluster-3 | Garage S3 (WAL and base backups) | 3900 |
| CloudNativePG operator | Database instances | 8000 |
| Database instances | Kubernetes API | any |
| The nodes | Database instances | 8000, 5432 |

## Storage

| From | To | Port |
|---|---|---|
| Garage | Garage (cluster RPC) | 3901 |
| garage-init | Garage admin | 3903 |
| Garage | Kubernetes API (node discovery) | any |
| The nodes | Garage S3 | 3900 |

## Admission webhooks

| From | To | Port |
|---|---|---|
| Kubernetes API | Kyverno | 9443 |
| Kubernetes API | CloudNativePG operator | 9443 |

## Operators and controllers

| From | To | Port |
|---|---|---|
| Kyverno, prometheus-operator, Longhorn, Strimzi, Reloader, Alloy, kube-state-metrics | Kubernetes API | any |
