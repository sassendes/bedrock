---
title: "Data and storage"
weight: 6
description: "Block storage, object storage and the PostgreSQL clusters, and how data moves between them."
---

A tenant keeps two kinds of irreplaceable data: what is in PostgreSQL and what is in S3. Everything else can be rebuilt from the repository. The storage design follows from that.

## Block storage: Longhorn

Longhorn turns the disks of the tenant's nodes into replicated block volumes. Most volumes keep a replica on several nodes, so a volume survives the loss of a node. When a node dies, pods stuck on it are deleted and rescheduled instead of holding their volume forever.

## Object storage: Garage

Garage provides S3 inside the tenant. It runs one pod per node and copies every object to several of them, so losing a node leaves full copies serving requests.

Each Garage pod stores its data on a Longhorn volume with a single replica that stays on the same node. Garage already keeps its own copies, so Longhorn copying each of them again would waste disk for nothing.

Those volumes are encrypted with LUKS. A disk pulled from a node, or the raw replica files on the host, are unreadable without the key.

Every consumer gets its own bucket and its own key with read and write rights, never owner rights. The replicas get a separate read-only key on the primary's bucket.

## PostgreSQL: a primary and its replicas

{{< diagram "data-path" "Writes go to cluster-1. cluster-2 follows it live, cluster-3 follows it five minutes behind, and every cluster archives to Garage." >}}

| Cluster | Role | Covers |
|---|---|---|
| `cluster-1` | Primary, several instances with synchronous replication | Normal operation and the loss of one instance |
| `cluster-2` | Live replica, streaming from cluster-1 | The loss of cluster-1 as a whole |
| `cluster-3` | Delayed replica, five minutes behind | Bad data: a dropped table or a `DELETE` without a `WHERE` |

Inside cluster-1, a commit returns only once at least one standby instance has it too. If no standby is available, the primary keeps accepting writes instead of stopping, so failed standbys never freeze the database.

cluster-2 and cluster-3 are separate clusters, not extra instances. A problem that corrupts cluster-1 does not automatically reach them, and cluster-3's delay gives an operator five minutes to stop replay before a mistake is applied there too. The delay is only useful if someone notices quickly, so a dropped table in the logs and a sudden spike of deleted or updated rows both raise a critical alert.

## How applications connect

Applications never connect to a database pod directly. They use `postgres-rw.application.svc:5432`, which points at cluster-1's PgBouncer pooler. If cluster-2 is promoted, that name is pointed at cluster-2's pooler and applications reconnect without a configuration change.

## Backups

Each cluster archives its write-ahead log (WAL) to its own Garage bucket continuously and takes a full base backup every six hours. Backups are kept for 30 days. Together they allow point-in-time recovery to any moment in that window.

The replicas are created from cluster-1's backups. That means every fresh deploy restores a backup for real, once per replica cluster, before the cluster is reported healthy.
