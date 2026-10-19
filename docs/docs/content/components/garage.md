---
title: "Garage"
weight: 4
description: "S3 object storage inside the tenant: layout, buckets, keys and access."
---

Garage v2.4.1 is a single binary that provides an S3 API. It replaces MinIO and Ceph RGW in bedrock, and the same tool is planned for the offsite backup machine.

## Shape

| Property | Value |
|---|---|
| Pods | 3, a StatefulSet with required anti-affinity, one per node |
| Replication factor | Equal to the node count, so every object is on every node |
| Consistency mode | `consistent` |
| Metadata engine | LMDB |
| Volume | 40 GiB per pod on `longhorn-local`, encrypted |
| Ports | 3900 S3, 3901 cluster RPC, 3903 admin |
| Service | `garage.garage.svc` |

Garage pods find each other through Kubernetes discovery. The `GarageNode` custom resource definition is shipped in `storage/garage.yaml` and Garage is started with `skip_crd = true`, so it never needs permission to create CRDs. Its service account has a Role in the `garage` namespace with exactly the four verbs its discovery code uses on `garagenodes`: get, list, create and update.

## The bucket job

`garage-init` runs once after Garage starts. It waits for every Garage node to join, applies the cluster layout, then creates the buckets and keys and writes each key into a Secret in the namespace that uses it:

- one bucket per consumer, with a key that can read and write but never own the bucket
- a separate read-only key on cluster-1's bucket for the replica clusters

Existing Secrets are skipped, so the job is safe to run again. Its RoleBindings are deleted by the bootstrap as soon as it succeeds, and the Job removes itself an hour later. The full list is in the [buckets and keys reference]({{< relref "/reference/buckets-and-keys" >}}).

## Access

Network policy limits who can reach each port:

| Port | Reachable from |
|---|---|
| 3900 S3 | Loki, Tempo, every database cluster, the nodes |
| 3901 RPC | Other Garage pods only |
| 3903 admin | `garage-init`, the blackbox exporter, Prometheus |

The admin API can create keys and delete buckets, which is why it is the most restricted. Its `/metrics` endpoint is open without a token because no `metrics_token` is set; the network policy is what limits it.

## Secrets

`rpc_secret` and `admin_token` come from the `garage-secrets` Secret, which holds placeholder values until OpenBao exists.

## Monitoring

Prometheus scrapes `/metrics` on 3903. `GarageDiskLow` and `GarageDiskCritical` fire when less than 15% or 5% of a node's data disk is free. The blackbox exporter probes `/health` on the admin port.
