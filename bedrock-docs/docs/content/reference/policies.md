---
title: "Admission policies"
weight: 4
description: "Every admission policy in a tenant: what it checks, where it applies and what happens when its engine is down."
---

## Kyverno policies

Files are in `kyverno/`.

| Policy | Checks | Namespaces | Action | If Kyverno is down |
|---|---|---|---|---|
| `disallow-latest-tag` | Every container, init container and ephemeral container has an explicit tag that is not `latest` | `application`, `node-agents` | Deny | Fail closed |
| `require-resource-limits` | CPU and memory limits on containers and init containers | `application`, `node-agents` | Deny | Fail closed |
| `harden-run-as-nonroot` | Fills in `runAsNonRoot`, `seccompProfile`, `allowPrivilegeEscalation: false` and `drop: ALL` | `application` | Mutate | Fail closed |
| `registry-allowlist-strict` | Images come from an allowed publisher | `application`, `node-agents`, `reloader` | Deny | Fail closed |
| `registry-allowlist-data` | Images come from an allowed publisher | `garage`, `falco`, `nats`, `redis`, `kafka`, `cluster-1`, `cluster-2`, `cluster-3`, `prometheus-operator` | Deny | Fail open |
| `restrict-priority-classes` | Reserved priority classes are used only where allowed | all | Deny | Fail open |

`bedrock-replica-secret-sync` in `database/replica-secret-sync.yaml` is a generate policy. It copies the replication certificates from `cluster-1` into `cluster-2` and `cluster-3` and keeps them in sync.

## Allowed image publishers

| Registry | Allowed |
|---|---|
| Docker Hub | `grafana/*`, `falcosecurity/*`, `nats`, `redis`, `dxflrs/garage`, `alpine/k8s` |
| ghcr.io | `cloudnative-pg/*`, `opencost/*`, `stakater/reloader` |
| quay.io | `prometheus/*`, `prometheus-operator/*`, `strimzi/*`, `kiwigrid/k8s-sidecar` |
| registry.k8s.io | `kube-state-metrics/kube-state-metrics` |

## ValidatingAdmissionPolicy

`bedrock-reserved-priority-classes` in `platform/priorityclasses.yaml` runs inside the API server and needs no webhook. It denies pod creation when:

- `bedrock-storage` is used outside `garage`
- `bedrock-data` is used outside `cluster-1`, `cluster-2`, `cluster-3` and `falco`
- `system-cluster-critical` or `system-node-critical` is used outside `kube-system` and `kyverno`

## Pod Security Admission

The `restricted` level is enforced on every namespace except `node-agents`, `falco` and `longhorn-system`, which are `privileged`, and the namespaces created by RKE2 and upstream bundles. See [namespaces]({{< relref "namespaces" >}}).
