---
title: "Kyverno"
weight: 2
description: "The policy engine: image, tag and resource checks on pods, plus the replication certificate sync."
---

Kyverno v1.19.1 is installed from the release's `install.yaml`, checked against its sha256 checksum. It is the first component after the namespaces, so every pod created later passes through it.

## What the bootstrap changes

| Change | Upstream default | Why |
|---|---|---|
| Admission controller replicas | 1 | One per server node, so losing a node never loses admission |
| Admission controller spread | none | Required anti-affinity, at most one replica per node |
| Admission controller priority | default | `system-cluster-critical`, so it is never evicted to make room |
| Background controller priority | default | `system-cluster-critical`, because it keeps the replication certificates in sync |
| Disruption budget | none | `minAvailable: 2` in `kyverno/admission-pdb.yaml` |

The bootstrap waits for the rollout to finish before it applies the policies.

## Scoped with match conditions

Each policy has a `webhookConfiguration` with a CEL `matchConditions` expression listing the namespaces it covers. The API server evaluates that expression before calling Kyverno at all, so a request for a namespace outside the list never reaches the webhook. This is what lets a policy fail closed for applications without putting storage and databases behind Kyverno.

## Policies

| Policy | Purpose |
|---|---|
| `disallow-latest-tag` | Explicit tags, never `latest` |
| `require-resource-limits` | CPU and memory limits on every container |
| `harden-run-as-nonroot` | Fills in non-root and seccomp defaults |
| `registry-allowlist-strict` and `registry-allowlist-data` | Images only from allowed publishers |
| `restrict-priority-classes` | Reserved priority classes only where allowed |
| `bedrock-replica-secret-sync` | Copies replication certificates into the replica namespaces |

Where each one applies and how it fails are in the [policies reference]({{< relref "/reference/policies" >}}).

## The certificate sync

`bedrock-replica-secret-sync` clones `cluster-1-replication` into both replica namespaces with `synchronize` on. The CA goes through its own policy, `bedrock-replica-ca-sync`: it fires on `cluster-1-ca` itself and writes only `ca.crt` into `cluster-1-ca-crt` in cluster-2 and cluster-3, so the CA private key never leaves cluster-1. Because the source secret is the trigger, every CloudNativePG rotation updates the copies. The policy fails open, so a dead Kyverno never blocks CloudNativePG from writing its own secrets.

## Alerts

`KyvernoAdmissionDegraded` fires when fewer than a majority of admission replicas are ready and `KyvernoAdmissionDown` when none are. Both are critical, because the application policies fail closed.
