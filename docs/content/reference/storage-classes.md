---
title: "Storage classes"
weight: 9
description: "The Longhorn storage classes, their settings and which components use each."
---

## Classes

bedrock's two classes are defined in `tenant-workload/storage/longhorn/`.

| | `longhorn-encrypted` | `longhorn-local` | `longhorn` |
|---|---|---|---|
| Defined by | `longhorn-encrypted.yaml` | `longhorn-local.yaml` | Longhorn's manifest |
| Default class | No | No | Yes |
| Replicas | 3, spread across nodes | 1 | Longhorn's default, spread across nodes |
| Data locality | Off | `strict-local` | Off |
| Encrypted | Yes, LUKS with the key in `longhorn-crypto` | Yes, LUKS with the key in `longhorn-crypto` | No |
| Binding | Immediate | `WaitForFirstConsumer` | Immediate |
| Expansion | Allowed | Allowed | Allowed |
| Reclaim policy | Delete | Delete | Delete |

`longhorn-local` binds only when the pod is scheduled, so the volume is created on the node the pod lands on, and `strict-local` keeps its one replica on that node. Garage and OpenBao run one pod per node, so each pod owns the disk beside it.

## Who uses which

| Class | Used by |
|---|---|
| `longhorn-encrypted` | Database clusters, Prometheus, Loki, Tempo, Grafana, NATS, Redis, Kafka |
| `longhorn-local` | Garage, OpenBao |
| `longhorn` | Nothing in bedrock |

Garage replicates every object across its own nodes and OpenBao replicates its data over raft, so their volumes need no Longhorn replication. Everything else relies on Longhorn for its copies.

## Encryption key

Both encrypted classes pass the `longhorn-crypto` Secret to the CSI driver at stage, publish and expand time:

| Field | Value |
|---|---|
| `CRYPTO_KEY_PROVIDER` | `secret` |
| `CRYPTO_KEY_CIPHER` | `aes-xts-plain64` |
| `CRYPTO_KEY_HASH` | `sha256` |
| `CRYPTO_KEY_SIZE` | `512` |
| `CRYPTO_PBKDF` | `pbkdf2` |
| `CRYPTO_KEY_VALUE` | The passphrase. Set it before the first deploy. |

The classes have no provisioner secret parameters on purpose. Encryption happens on the node, so only the node stages need the key.

## Longhorn settings

`tenant-workload/storage/longhorn/longhorn-settings.yaml` changes these Longhorn defaults:

| Setting | Value | Why |
|---|---|---|
| `node-down-pod-deletion-policy` | `delete-both-statefulset-and-deployment-pod` | Pods on a dead node are deleted so they can start elsewhere with their volume |
| `storage-over-provisioning-percentage` | `300` | Volumes can promise more than the disk holds, since most are not full |
| `upgrade-checker` | `false` | No calls out to check for new versions |
| `allow-collecting-longhorn-usage-metrics` | `false` | No usage data sent out |
