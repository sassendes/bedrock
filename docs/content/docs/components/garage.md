---
aliases: ["/components/garage/"]
title: "Garage"
weight: 4
description: "S3 object storage inside the tenant: layout, buckets, keys and access."
---

Garage v2.4.1 is a single binary that provides an S3 API.

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

Garage pods find each other through Kubernetes discovery. The `GarageNode` custom resource definition is shipped in `tenant-workload/storage/garage/garage.yaml` and Garage is started with `skip_crd = true`, so it never needs permission to create CRDs. Its service account has a Role in the `garage` namespace with exactly the four verbs its discovery code uses on `garagenodes`: get, list, create and update.

## The bucket job

`garage-init` runs once after Garage starts. It waits for every Garage node to join and applies the cluster layout, with each node's capacity set to 90% of the size of its data volume, read from the volume claim. Then it creates the buckets and keys and writes each key into a Secret in the namespace that uses it:

- one bucket per consumer, with a key that can read and write but never own the bucket
- a separate read-only key on cluster-1's bucket for the replica clusters

Existing Secrets are skipped, so the job is safe to run again. Its RoleBindings are deleted by the bootstrap as soon as it succeeds, and the Job removes itself an hour later. The full list is in the [buckets and keys reference]({{< relref "/docs/reference/buckets-and-keys" >}}).

## Access

Network policy limits who can reach each port:

| Port | Reachable from |
|---|---|
| 3900 S3 | Loki, Tempo, every database cluster, the nodes |
| 3901 RPC | Other Garage pods only |
| 3903 admin | `garage-init`, the blackbox exporter, Prometheus |

The admin API can create keys and delete buckets, which is why it is the most restricted. Its `/metrics` endpoint is open without a token because no `metrics_token` is set; the network policy is what limits it.

## Secrets

The RPC secret and the admin token live in OpenBao at `apps/garage/rpc` and `apps/garage/admin`. The bootstrap makes them once and keeps a copy on the controller in `~/.bedrock/garage-rpc-secret` and `~/.bedrock/garage-admin-token`. They reach the Garage pods as files through the Secrets Store CSI driver, and Garage reads them with `GARAGE_RPC_SECRET_FILE` and `GARAGE_ADMIN_TOKEN_FILE`. The `garage-init` job gets only the admin token, through its own OpenBao role.

The CSI driver writes the files with mode 0644 and Garage refuses secret files that others can read, so `GARAGE_ALLOW_WORLD_READABLE_SECRETS` is set. The files sit in memory inside the pod and the only process there is Garage.

## Monitoring

Prometheus scrapes `/metrics` on 3903. `GarageDiskLow` and `GarageDiskCritical` fire when less than 15% or 5% of a node's data disk is free. The blackbox exporter probes `/health` on the admin port.
