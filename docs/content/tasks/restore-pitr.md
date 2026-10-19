---
title: "Restore a database to a point in time"
weight: 7
description: "Recover cluster-1's data as it was at a given moment, into a new cluster, from the backups in Garage."
---

Every database cluster archives its write-ahead log to Garage continuously and takes base backups on the schedule in `database/backups/`, kept for the `retentionPolicy` in its `backup` section. Together they let you recover the data as it was at any moment in that window. This guide restores cluster-1 into a separate, new cluster, so the running database is not touched.

## Before you begin

- Know the moment you want, in UTC, for example `2026-10-19 09:30:00+00`.
- Make sure Longhorn has room for one more volume the size of cluster-1's.

## Write the restore cluster

Create `cluster-1-restore.yaml`. It lives in the `cluster-1` namespace, so it can reach Garage and already has the backup credentials:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: cluster-1-restore
  namespace: cluster-1
spec:
  instances: <instances>
  priorityClassName: bedrock-data
  imageName: <imageName from cluster-1.yaml>
  enableSuperuserAccess: false
  postgresql:
    pg_hba:
      - hostnossl all all all reject
  storage:
    size: <storage size from cluster-1.yaml>
    storageClass: longhorn
  env:
    - name: AWS_REQUEST_CHECKSUM_CALCULATION
      value: when_required
    - name: AWS_RESPONSE_CHECKSUM_VALIDATION
      value: when_required
    - name: AWS_DEFAULT_REGION
      value: garage
  bootstrap:
    recovery:
      source: cluster-1-backup
      recoveryTarget:
        targetTime: "2026-10-19 09:30:00+00"
  externalClusters:
    - name: cluster-1-backup
      barmanObjectStore:
        serverName: cluster-1
        destinationPath: s3://cluster-1-backups/wals
        endpointURL: http://garage.garage.svc:3900
        s3Credentials:
          accessKeyId:
            name: cluster-1-backups
            key: AWS_ACCESS_KEY_ID
          secretAccessKey:
            name: cluster-1-backups
            key: AWS_SECRET_ACCESS_KEY
        wal:
          compression: gzip
        data:
          compression: gzip
```

Fill in the placeholders. Copy `imageName` and the storage size from `database/cluster-1.yaml`, since the restored data must fit and the PostgreSQL major version must match. A single instance is enough to inspect the data; use more if the restored cluster will serve traffic. Change `targetTime` to your moment. The file has no `backup` section on purpose: the restored cluster must never write into cluster-1's bucket.

## Run the restore

```sh
kubectl apply -f cluster-1-restore.yaml
kubectl -n cluster-1 get cluster cluster-1-restore -w
```

CloudNativePG fetches the base backup taken before your moment, replays the WAL up to it and starts the instance. When the status reads `Cluster in healthy state`, the data is ready.

## Check the data

```sh
kubectl -n cluster-1 exec cluster-1-restore-1 -c postgres -- psql -U postgres -d app -c "\dt"
```

Query the tables you care about and compare with what you expected at that moment.

## Use or discard it

The restored cluster is a normal CloudNativePG cluster. You can copy rows out of it with `pg_dump` and load them into cluster-1, or keep it while you investigate. When you are done, delete it:

```sh
kubectl delete -f cluster-1-restore.yaml
```

Its volume is deleted with it.
