---
title: "CloudNativePG"
weight: 5
description: "The PostgreSQL operator: clusters, poolers, backups and the settings that harden them."
---

CloudNativePG 1.30.1 runs PostgreSQL 17.4. The operator lives in `cnpg-system` and manages one `Cluster` resource per database namespace.

## The clusters

| Setting | cluster-1 | cluster-2 | cluster-3 |
|---|---|---|---|
| Role | Primary | Live replica | Delayed replica |
| Instances | 3 | 3 | 2 |
| Created by | `initdb` (database `app`, owner `appuser`) | Recovery from cluster-1's backups | Recovery from cluster-1's backups |
| Replication | Synchronous inside the cluster | Streams from cluster-1 | Streams from cluster-1, 5 minutes behind |
| Volume | 10 GiB on `longhorn-encrypted` | 10 GiB on `longhorn-encrypted` | 10 GiB on `longhorn-encrypted` |
| Priority class | `bedrock-data` | `bedrock-data` | `bedrock-data` |

Each instance requests 256 MiB and 100m of CPU, with limits of 1 GiB and 1 CPU.

## Hardening

| Setting | Effect |
|---|---|
| `enableSuperuserAccess: false` | The `postgres` user cannot log in over the network. Applications use their own users. |
| `pg_hba: hostnossl all all all reject` | Placed before CloudNativePG's default rules, it refuses every connection that is not TLS |
| Applications log in as `bedrock_app` | OpenBao owns that role's password and changes it every 24 hours. The pods read it as files through the Secrets Store CSI driver, from `database/static-creds/bedrock-app` |
| `log_statement: ddl` | Every schema change, including `DROP`, is written to the log and shipped to Loki |
| Replicas connect with `sslmode: verify-ca` | The replica checks it is talking to the real cluster-1 |
| Replicas read backups with a read-only key | A compromised replica cannot change or delete cluster-1's backups |

## Synchronous replication

Every cluster carries the same quorum-based synchronous replication block:

```yaml
synchronous:
  method: any
  number: 1
  dataDurability: preferred
```

A commit returns once at least one standby instance has written it. `preferred` means that if no standby is up, the primary carries on alone instead of blocking every write. cluster-2 and cluster-3 stream from cluster-1 asynchronously as separate clusters. The block does nothing on them while they are replicas, and takes effect between their own instances the moment one is promoted, so a new primary never runs without it. A replica added with `make add-replica` gets it too when it has more than one instance.

## Replica clusters

cluster-2 and cluster-3 start with `bootstrap.recovery` from cluster-1's backup bucket, then switch to streaming from `cluster-1-rw.cluster-1.svc` with `replica.enabled: true`. cluster-3 adds `minApplyDelay: 5m`. Because the delay is incompatible with CloudNativePG's promotion token, cluster-3 is promoted by setting `replica.enabled: false`.

The replicas need cluster-1's CA and replication certificate in their own namespaces. Kyverno copies them and keeps them current (see [Kyverno]({{< relref "kyverno" >}})).

## Poolers

Each cluster has a PgBouncer `Pooler` with more than one instance in transaction mode, `max_client_conn` 1000 and `default_pool_size` 25, and a disruption budget that keeps one running. Each pooler pod requests 50m and 64 MiB, with limits of 500m and 256 MiB, so it is never a best effort pod in the write path. The poolers on cluster-2 and cluster-3 stay ready but only accept writes once their cluster is promoted.

`postgres-rw.application.svc` is an `ExternalName` service pointing at the active pooler. Promotion changes only where it points.

## Backups

| Setting | Value |
|---|---|
| Destination | One Garage bucket per cluster, `s3://cluster-N-backups/wals` |
| WAL archiving | Continuous, gzip |
| Base backups | Every 6 hours (`0 0 */6 * * *`), gzip |
| Retention | 30 days |
| First backup | `cluster-1-initial`, which must complete before the replicas can be created |

The S3 client needs `AWS_REQUEST_CHECKSUM_CALCULATION` and `AWS_RESPONSE_CHECKSUM_VALIDATION` set to `when_required` to work with Garage, and `AWS_DEFAULT_REGION` set to `garage`.

To recover data from these backups, see [restore a database to a point in time]({{< relref "/tasks/restore-pitr" >}}).

## Spread

Pod anti-affinity is `preferred`, not required. If a node dies, its instance moves onto a surviving node instead of staying Pending, which keeps the cluster available at the cost of instances sharing a node. `CNPGStackedInstances` fires when that happens, because instances never move back on their own.

## Monitoring

Every cluster has `enablePodMonitor: true`, so Prometheus scrapes each instance on 9187. Alerts cover replication lag, instance health, WAL archiving, backup age and sudden spikes of deleted or updated rows.

## Database users from OpenBao

The bootstrap creates two roles on cluster-1 with `psql` as `postgres`. CloudNativePG leaves roles that are not in a Cluster spec or a DatabaseRole alone, so it never touches them.

| Role | Rights | Password |
|---|---|---|
| `openbao` | `LOGIN CREATEROLE`, and admin of `bedrock_app` without inheriting its rights | Set once by the bootstrap, then rotated by OpenBao so nobody else knows it |
| `bedrock_app` | `LOGIN`, member of `appuser`, so it has the rights of the database owner | Changed by OpenBao every 24 hours, a static role |

Roles live in PostgreSQL's catalog, so they reach cluster-2 and cluster-3 through replication and survive a failover. OpenBao's connection string lists the read-write services of all three clusters with `target_session_attrs=read-write`. It skips the read-only replicas and lands on whichever cluster is the primary, so after a failover it follows on its own.

A static role fits the CSI driver better than dynamic users. The driver reads the secret again every two minutes, and with dynamic users every read would create a new PostgreSQL user. A static role returns the current password every time.

OpenBao connects with `sslmode=verify-full`, so it checks both the certificate and the host name of the server it reaches. Every cluster has its own CA, which CloudNativePG rotates on its own. Kyverno copies each cluster's `ca.crt` into a `db-ca-<cluster>` ConfigMap in `openbao` and keeps it current. A small `db-ca-bundle` sidecar in each OpenBao pod joins them into one file every minute, because the PostgreSQL driver takes a single CA file. A ValidatingAdmissionPolicy lets no workload but Kyverno write a `db-ca-*` ConfigMap in `openbao`, so nobody can slip in a CA of their own.

The connection string is built from every namespace labelled `bedrock.io/database`. `make add-replica` waits for the new replica's CA in the bundle, then rewrites the connection string with the new cluster in it. OpenBao keeps the stored password when only the connection string changes.

An application mounts the `bedrock-app-db` SecretProviderClass in `application` with the `bedrock-app` service account, and reads `username` and `password` from the files. It should read the files again each time it opens a connection.

