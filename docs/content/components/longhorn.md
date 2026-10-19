---
title: "Longhorn"
weight: 3
description: "Block storage for every volume in the cluster, and how its permissions and settings are tightened."
---

Longhorn v1.13.0 provides block volumes from the local disks of the tenant's nodes. It is installed from the official manifest, checked against its sha256 checksum, and needs Kubernetes 1.34 or newer.

## Node requirements

The bootstrap installs `open-iscsi`, `nfs-common`, `lvm2` and `cryptsetup` on every node, starts `iscsid` and loads the `dm_crypt` kernel module now and at every boot. `cryptsetup` and `dm_crypt` are needed for encrypted volumes.

## Storage classes

| Class | Replicas | Placement | Encrypted | Used by |
|---|---|---|---|---|
| `longhorn` (default) | 3 | anywhere | no | Databases, Prometheus, Loki, Tempo, Grafana, the buses |
| `longhorn-local` | 1 | same node as the pod | yes | Garage |

`longhorn-local` exists because Garage replicates its own data across nodes. Replicating each of those copies again in Longhorn would multiply the space used for no extra safety. Details are in the [storage classes reference]({{< relref "/reference/storage-classes" >}}).

## Permissions

The upstream manifest grants Longhorn wildcard permissions. The bootstrap rewrites them before applying:

- the rule for `clusterroles` and `clusterrolebindings` keeps only the verbs Longhorn needs, including `bind` and `escalate`
- every other wildcard verb becomes an explicit list

This is what CIS check 5.1.3 asks for.

The bootstrap also deletes the `longhorn-csi-secret-bind` ClusterRoleBinding. It gives Longhorn's CSI sidecars read access to every Secret in the cluster, and Longhorn documents deleting it as the opt-out. Encrypted volumes still work without it, because the encryption key is read by the kubelet when it mounts a volume, not by the CSI provisioner.

## Settings

| Setting | Value | Why |
|---|---|---|
| `node-down-pod-deletion-policy` | `delete-both-statefulset-and-deployment-pod` | Pods stuck on a dead node are deleted and rescheduled instead of holding their volume |
| `storage-over-provisioning-percentage` | `300` | Longhorn books the full provisioned size times the replica count, not the space actually used |
| `upgrade-checker` | `false` | No calls to the internet |
| `allow-collecting-longhorn-usage-metrics` | `false` | No usage data sent upstream |

## Encryption

Volumes in `longhorn-local` are LUKS devices. The key lives in the `longhorn-crypto` Secret in `longhorn-system` and is referenced by the storage class for staging, publishing and expanding the volume. Changing a storage class's parameters requires recreating it, so turning encryption on for existing volumes means a fresh deploy.

## Monitoring

Prometheus scrapes the Longhorn manager on port 9500. `LonghornVolumeDegraded` fires when a volume has lost replicas and `LonghornVolumeFaulted` when it has none left.
