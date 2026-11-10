---
title: "Restore a database to a point in time"
weight: 7
description: "Recover the primary's data as it was at a given moment, into a new cluster, from the backups in Garage."
---

Every database cluster archives its write-ahead log to Garage continuously and takes base backups on the schedule in `tenant-workload/database/backups/`, kept for the `retentionPolicy` in its `backup` section. Together they let you recover the data as it was at any moment in that window. This guide restores the current primary into a separate, new cluster, so the running database is not touched. The primary is cluster-1 until a failover, then whichever cluster carries `bedrock.io/db-role: primary`:

```sh
kubectl get clusters.postgresql.cnpg.io -A -l bedrock.io/db-role=primary
```

Below, `<primary>` is its name, which is also its namespace.

## Before you begin

- Know the moment you want, in UTC, for example `2026-10-19 09:30:00+00`.
- Make sure Longhorn has room for one more volume the size of the primary's.

## Write the restore cluster

Create `restore.yaml`. It lives in the primary's namespace, so it can reach Garage and already has the backup credentials:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: <primary>-restore
  namespace: <primary>
  labels:
    bedrock.io/db-role: unmanaged
spec:
  instances: <instances>
  priorityClassName: bedrock-data
  imageName: <imageName of the primary>
  enableSuperuserAccess: false
  postgresql:
    pg_hba:
      - hostnossl all all all reject
  storage:
    size: <storage size of the primary>
    storageClass: longhorn-encrypted
  env:
    - name: AWS_REQUEST_CHECKSUM_CALCULATION
      value: when_required
    - name: AWS_RESPONSE_CHECKSUM_VALIDATION
      value: when_required
    - name: AWS_DEFAULT_REGION
      value: garage
  bootstrap:
    recovery:
      source: primary-backup
      recoveryTarget:
        targetTime: "2026-10-19 09:30:00+00"
  externalClusters:
    - name: primary-backup
      barmanObjectStore:
        serverName: <primary>
        destinationPath: s3://<primary>-backups/wals
        endpointURL: http://garage.garage.svc:3900
        s3Credentials:
          accessKeyId:
            name: <primary>-backups
            key: AWS_ACCESS_KEY_ID
          secretAccessKey:
            name: <primary>-backups
            key: AWS_SECRET_ACCESS_KEY
        wal:
          compression: gzip
        data:
          compression: gzip
```

Fill in the placeholders. Copy `imageName` and the storage size from the primary's cluster (`kubectl -n <primary> get cluster <primary> -o yaml`), since the restored data must fit and the PostgreSQL major version must match. The `unmanaged` role label tells the failover controller to leave this cluster alone, so it is neither watched nor reported as a problem. A single instance is enough to inspect the data; use more if the restored cluster will serve traffic. Change `targetTime` to your moment. The file has no `backup` section on purpose: the restored cluster must never write into the primary's bucket.

## Run the restore

```sh
kubectl apply -f restore.yaml
kubectl -n <primary> get cluster <primary>-restore -w
```

CloudNativePG fetches the base backup taken before your moment, replays the WAL up to it and starts the instance. When the status reads `Cluster in healthy state`, the data is ready.

## Check the data

```sh
kubectl -n <primary> exec <primary>-restore-1 -c postgres -- psql -U postgres -d app -c "\dt"
```

Query the tables you care about and compare with what you expected at that moment.

## Use or discard it

The restored cluster is a normal CloudNativePG cluster. You can copy rows out of it with `pg_dump` and load them into the primary, or keep it while you investigate. When you are done, delete it:

```sh
kubectl delete -f restore.yaml
```

Its volume is deleted with it.
