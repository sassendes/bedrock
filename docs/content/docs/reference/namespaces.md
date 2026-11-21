---
aliases: ["/reference/namespaces/"]
title: "Namespaces"
weight: 2
description: "Every namespace in a tenant, what it holds and its Pod Security level."
---

| Namespace | Holds | Pod Security | Same-namespace traffic |
|---|---|---|---|
| `observability` | Prometheus, Alertmanager, Grafana, Loki, Tempo, OpenCost, kube-state-metrics, blackbox exporter | restricted | Per component only |
| `application` | Created with the first tenant app. Holds the apps, the `postgres-rw` service and the `bedrock-app` service account | restricted | None |
| `node-agents` | Alloy, node-exporter | privileged | None |
| `cluster-1` | Primary PostgreSQL cluster and its pooler | restricted | Allowed |
| `cluster-2` | Live replica cluster and its pooler | restricted | Allowed |
| `cluster-3` | Delayed replica cluster and its pooler | restricted | Allowed |
| `bedrock-failover` | The failover controller: observers, deciders, the verifier and the checker | restricted | Per component only |
| `cnpg-system` | CloudNativePG operator | set by the upstream bundle | Allowed |
| `garage` | Garage and its init job | restricted | Per component only |
| `openbao` | OpenBao, three pods on raft | restricted | OpenBao pods only |
| `secrets-store` | Secrets Store CSI driver and the OpenBao CSI provider, one pod of each per node | privileged | None |
| `longhorn-system` | Longhorn | privileged | Allowed |
| `kyverno` | Kyverno controllers | set by the upstream bundle | Allowed |
| `prometheus-operator` | prometheus-operator | restricted | Allowed |
| `falco` | Falco | privileged | Allowed |
| `reloader` | Reloader | restricted | None |
| `nats` | NATS JetStream, when enabled | restricted | Allowed |
| `valkey` | Valkey and Sentinel, when enabled | restricted | Allowed |
| `kafka` | Strimzi and Kafka, when enabled | restricted | Allowed |
| `kube-system` | RKE2 components, Cilium, CoreDNS | set by RKE2 | Allowed |

Default service accounts in every namespace have `automountServiceAccountToken: false`. Tenant apps run in `application` and never next to the monitoring stack.

## Namespace labels

Every namespace bedrock creates carries `bedrockctl.com/role` set to its own name. Network policies name a peer in another namespace by this label and the pod's own labels. Moving a component to a new namespace means moving the role label with it.

A namespace whose own policy limits the node to named ports carries `bedrockctl.com/host-limited: "true"`. The cluster-wide default deny leaves those namespaces out.

## Priority classes

| Class | Value | Allowed in |
|---|---|---|
| `bedrock-storage` | 90000000 | `garage` |
| `bedrock-data` | 80000000 | Every namespace labelled `bedrockctl.com/database: "true"`, `falco` and `bedrock-failover` |
| `bedrock-workload` | 50000000 | any |
| `bedrock-default` | 10000000 | any, the global default |
| `system-cluster-critical`, `system-node-critical` | Kubernetes defaults | `kube-system`, `kyverno` |
