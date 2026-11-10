---
title: "Add a live replica"
weight: 19
description: "Put a new live replica behind the current primary with one command, for example after a failover left none."
---

After a failover the old primary stays fenced and the delayed replica is never promoted, so there is no live replica left and `BedrockNoLiveReplica` pages. A second failover needs a new one.

```sh
make add-replica REPLICA=cluster-4
```

Names are never reused: a name whose namespace or Garage bucket already exists is refused, because the old bucket holds another database's WAL.

## What it does

1. Reads every database cluster's `bedrock.io/db-role` label and checks it against its spec. It stops if any label disagrees, or if there is not exactly one healthy, unfenced primary.
2. Creates the namespace, labelled `bedrock.io/database: "true"`, so the priority class policies, the registry allowlist and the database CA sync cover it without any name list.
3. Applies the replica's own network policy. Garage, the operator, monitoring, the failover controller and the other database clusters already select database namespaces by label, so nothing outside the replica's namespace changes.
4. Runs a Garage job: a bucket and a read-write key for the replica, and one read-only key per pair below, each with its own name so no other replica's key is ever deleted.
5. Pairs the replica with every cluster that can become primary (the primary and every live replica), and every existing replica with the new one: Kyverno copies the peer's `ca.crt` and replication certificate across, and a network policy on each side allows the stream on 5432.
6. Takes a fresh base backup of the primary, then creates the cluster, its pooler and its scheduled backups, with an `externalClusters` entry for every peer.
7. Waits until it is healthy and streaming, then adds an `externalClusters` entry for it to every other replica, so the decider can repoint them to it if it is ever promoted.

## Options

| Variable | Default |
|---|---|
| `instances` | 2 |
| `storage` | 10Gi |
| `image` | the PostgreSQL image pinned in `database/cluster-1.yaml` |

Pass them with `-e`, for example `ansible-playbook tenant-workload/bootstrap/add-live-replica.yml -e replica=cluster-4 -e instances=3`.
