---
aliases: ["/reference/components/"]
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
| Failover controller | 0.4, built in this repository | `bedrock-failover` |

## Secrets

| Component | Version | Namespace |
|---|---|---|
| OpenBao | 2.7.2 | `openbao` |
| Secrets Store CSI driver | v1.6.2 | `secrets-store` |
| OpenBao CSI provider | 2.0.3 | `secrets-store` |

## Observability

| Component | Version | Namespace |
|---|---|---|
| prometheus-operator | v0.93.1 | `prometheus-operator` |
| Prometheus | v3.13.1 | `observability` |
| Alertmanager | v0.33.1 | `observability` |
| Grafana | 13.2.4 | `observability` |
| k8s-sidecar | 2.14.1 | `observability` |
| Loki | 3.7.8 | `observability` |
| Tempo | 3.1.0 | `observability` |
| OpenCost | 1.121.3 | `observability` |
| kube-state-metrics | v2.20.0 | `observability` |
| blackbox exporter | v0.29.0 | `observability` |
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
| Valkey | 9.1.3 | `valkey` |
| Strimzi | 1.2.0 | `kafka` |
| Kafka | 4.3.1 | `kafka` |
