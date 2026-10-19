---
title: "Storage classes"
weight: 9
description: "The Longhorn storage classes, their settings and which components use each."
---

## Classes

| | `longhorn` | `longhorn-local` |
|---|---|---|
| Defined by | Longhorn's manifest | `storage/longhorn-local.yaml` |
| Default class | Yes | No |
| Replicas | Longhorn's default, spread across nodes | 1 |
| Data locality | Off | `strict-local`, the replica stays on the pod's node |
| Encrypted | No | Yes, LUKS with the key in `longhorn-crypto` |
| Binding | Immediate | `WaitForFirstConsumer` |
| Expansion | Allowed | Allowed |
| Reclaim policy | Delete | Delete |

`longhorn-local` binds only when the pod is scheduled, so the volume is created on the node the pod lands on. Garage runs one pod per node, so each Garage node owns the disk beside it.

## Who uses which

| Class | Used by |
|---|---|
| `longhorn` | Database clusters, Prometheus, Loki, Tempo, Grafana, NATS, Redis, Kafka |
| `longhorn-local` | Garage |

Garage replicates every object across its own nodes, so its volumes need no Longhorn replication. Everything else relies on Longhorn for its copies.

## Encryption key

The encrypted class passes the `longhorn-crypto` Secret to the CSI driver at stage, publish and expand time:

| Field | Value |
|---|---|
| `CRYPTO_KEY_PROVIDER` | `secret` |
| `CRYPTO_KEY_CIPHER` | `aes-xts-plain64` |
| `CRYPTO_KEY_HASH` | `sha256` |
| `CRYPTO_KEY_SIZE` | `256` |
| `CRYPTO_PBKDF` | `argon2i` |
| `CRYPTO_KEY_VALUE` | The passphrase. Set it before the first deploy. |

The class has no provisioner secret parameters on purpose. Encryption happens on the node, so only the node stages need the key.

## Longhorn settings

`storage/longhorn-settings.yaml` changes these Longhorn defaults:

| Setting | Value | Why |
|---|---|---|
| `node-down-pod-deletion-policy` | `delete-both-statefulset-and-deployment-pod` | Pods on a dead node are deleted so they can start elsewhere with their volume |
| `storage-over-provisioning-percentage` | `300` | Volumes can promise more than the disk holds, since most are not full |
| `upgrade-checker` | `false` | No calls out to check for new versions |
| `allow-collecting-longhorn-usage-metrics` | `false` | No usage data sent out |
