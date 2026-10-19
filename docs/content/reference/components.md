---
title: "Components and versions"
weight: 1
description: "Every component in a tenant cluster with its pinned version and namespace."
---

All images are pinned by digest in the repository. Upstream bundles are checked against a sha256 checksum before they are applied.

## Kubernetes and network

| Component | Version | Namespace |
|---|---|---|
| RKE2 | v1.36.4+rke2r1 | host |
| Cilium (bundled with RKE2) | RKE2 default | `kube-system` |
| Hubble relay and UI | Cilium default | `kube-system` |

## Policy

| Component | Version | Namespace |
|---|---|---|
| Kyverno | v1.19.1 | `kyverno` |
| Reloader | v1.4.22 | `reloader` |

## Storage and data

| Component | Version | Namespace |
|---|---|---|
| Longhorn | v1.13.0 | `longhorn-system` |
| Garage | v2.4.1 | `garage` |
| CloudNativePG operator | 1.30.1 | `cnpg-system` |
| PostgreSQL | 17.4 | `cluster-1`, `cluster-2`, `cluster-3` |

## Observability

| Component | Version | Namespace |
|---|---|---|
| prometheus-operator | v0.93.1 | `prometheus-operator` |
| Prometheus | v3.13.1 | `application` |
| Alertmanager | v0.33.1 | `application` |
| Grafana | 13.2.4 | `application` |
| k8s-sidecar | 2.14.1 | `application` |
| Loki | 3.7.8 | `application` |
| Tempo | 3.1.0 | `application` |
| OpenCost | 1.121.3 | `application` |
| kube-state-metrics | v2.20.0 | `application` |
| blackbox exporter | v0.29.0 | `application` |
| Alloy | v1.20.1 | `node-agents` |
| node-exporter | v1.12.1 | `node-agents` |

## Runtime security

| Component | Version | Namespace |
|---|---|---|
| Falco | 0.44.1 | `falco` |

## Event buses (off by default)

| Component | Version | Namespace |
|---|---|---|
| NATS | 2.14.7 | `nats` |
| Redis | 8.8.3 | `redis` |
| Strimzi | 1.2.0 | `kafka` |
| Kafka | 4.3.1 | `kafka` |
