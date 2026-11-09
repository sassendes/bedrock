---
title: "Admission policies"
weight: 4
description: "Every admission policy in a tenant: what it checks, where it applies and what happens when its engine is down."
---

## Kyverno policies

Files are in `tenant-workload/kyverno/`.

| Policy | Checks | Namespaces | Action | If Kyverno is down |
|---|---|---|---|---|
| `disallow-latest-tag` | Every container, init container and ephemeral container has an explicit tag that is not `latest` | `application`, `node-agents` | Deny | Fail closed |
| `require-resource-limits` | CPU and memory limits on containers and init containers | `application`, `node-agents` | Deny | Fail closed |
| `harden-run-as-nonroot` | Fills in `runAsNonRoot`, `seccompProfile`, `allowPrivilegeEscalation: false` and `drop: ALL` | `application` | Mutate | Fail closed |
| `registry-allowlist-strict` | Images come from an allowed publisher | `application`, `node-agents`, `reloader` | Deny | Fail closed |
| `registry-allowlist-data` | Images come from an allowed publisher | `garage`, `openbao`, `falco`, `nats`, `redis`, `kafka`, `cluster-1`, `cluster-2`, `cluster-3`, `prometheus-operator`, `bedrock-failover` | Deny | Fail open |
| `registry-allowlist-database` | Images come from an allowed publisher | Any namespace labelled `bedrock.io/database: "true"`, such as a replica added with `make add-replica` | Deny | Fail open |
| `restrict-priority-classes` | Reserved priority classes are used only where allowed | all | Deny | Fail open |

`bedrock-replica-secret-sync` and `bedrock-replica-ca-sync` in `tenant-workload/kyverno/generate/replica-secret-sync.yaml` are generate policies. They copy the replication certificate and the CA's `ca.crt` from `cluster-1` into `cluster-2` and `cluster-3` and keep them in sync. `bedrock-peer-cluster-2-to-cluster-3`, in the same file, does the same from `cluster-2` into `cluster-3`, so cluster-3 can follow cluster-2 after a failover. `make add-replica` adds one more of these for each new replica.

`bedrock-failover-ca-sync` and `bedrock-failover-password-sync` in `tenant-workload/kyverno/generate/failover-secret-sync.yaml` copy each database cluster's `ca.crt` and the `bedrock_observer` password into `bedrock-failover`. The CA private keys are never copied.

## Allowed image publishers

| Registry | Allowed |
|---|---|
| Docker Hub | `grafana/*`, `falcosecurity/*`, `nats`, `redis`, `dxflrs/garage`, `alpine/k8s` |
| ghcr.io | `cloudnative-pg/*`, `opencost/*`, `stakater/reloader`, `sassendes/bedrock-failover` |
| quay.io | `prometheus/*`, `prometheus-operator/*`, `strimzi/*`, `kiwigrid/k8s-sidecar`, `openbao/openbao` |
| registry.k8s.io | `kube-state-metrics/kube-state-metrics` |

## ValidatingAdmissionPolicy

`bedrock-reserved-priority-classes` in `tenant-workload/platform/priorityclasses.yaml` runs inside the API server and needs no webhook. It denies pod creation when:

- `bedrock-storage` is used outside `garage`
- `bedrock-data` is used outside `cluster-1`, `cluster-2`, `cluster-3`, `falco`, `bedrock-failover` and namespaces labelled `bedrock.io/database: "true"`
- `system-cluster-critical` or `system-node-critical` is used outside `kube-system` and `kyverno`

The failover controller adds seven more in `tenant-workload/failover/admission.yaml`. They decide what the observers, the verifier, the checker and the decider may write, and they are described on the [failover controller]({{< relref "/components/failover" >}}) page.

## Pod Security Admission

The `restricted` level is enforced on every namespace except `node-agents`, `falco` and `longhorn-system`, which are `privileged`, and the namespaces created by RKE2 and upstream bundles. See [namespaces]({{< relref "namespaces" >}}).
