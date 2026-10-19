---
title: "Buckets and keys"
weight: 8
description: "The S3 buckets in Garage, the key that can use each one and the Secret that key is stored in."
---

All buckets are created by the `garage-init` Job in `storage/buckets.yaml`. Each bucket has its own access key, scoped to that bucket only, and no key has owner rights, so no workload can change bucket settings or delete a bucket. The Job writes each key into a Secret in the namespace that uses it, with the fields `AWS_ACCESS_KEY_ID` and `AWS_SECRET_ACCESS_KEY`.

## Buckets

| Bucket | Used for | Key permissions | Secret |
|---|---|---|---|
| `<cluster>-backups` | WAL archive and base backups of each database cluster | Read and write | `<cluster>-backups` in the cluster's namespace |
| The primary's `<cluster>-backups` | Restoring the replica clusters from the primary's archive | Read only | `<primary>-backups-ro` in each replica's namespace |
| `loki-data` | Loki chunks and index | Read and write | `loki-data` in `application` |
| `tempo-data` | Tempo trace blocks | Read and write | `tempo-data` in `application` |

With the clusters in the repository, that means one `-backups` bucket per database cluster, plus `cluster-1-backups-ro` handed to each replica cluster.

## Endpoint

| Setting | Value |
|---|---|
| Endpoint | `http://garage.garage.svc:3900` |
| Region | `garage` |

Clients built on recent AWS SDKs must set `AWS_REQUEST_CHECKSUM_CALCULATION` and `AWS_RESPONSE_CHECKSUM_VALIDATION` to `when_required`, because Garage does not accept the newer default checksums. The database clusters set both in their `env`.

## How the Job behaves

- It waits until every Garage node is up, then applies the cluster layout if none exists.
- It skips any bucket whose Secret already exists, so running it again never replaces a working key.
- If a bucket exists but its Secret is gone, it creates a new key for the existing bucket. The old key keeps working until it is removed with the Garage admin API.
- Its service account can create Secrets, but can only read the ones listed above, and only in the namespaces it is bound to.

## Who can reach Garage

Network policy limits S3 traffic to Loki, Tempo, the database namespaces, the blackbox exporter and Prometheus. Holding a key is not enough on its own: a pod in any other namespace cannot open a connection to port 3900.
