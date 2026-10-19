---
title: "Alert rules"
weight: 5
description: "Every alert rule that ships with a tenant, with its severity and how long the condition must hold."
---

Most rules live in `observability/alerting/alert-rules.yaml` as one `PrometheusRule`. Falco's rules live next to Falco in `observability/falco/falco.yaml`. One more rule, `PostgresDropStatement`, is a Loki rule in `observability/loki/loki.yaml` that fires as soon as a database logs a `DROP` statement.

## Nodes

| Alert | Severity | For | Fires when |
|---|---|---|---|
| `NodeHighCPU` | warning | 10m | CPU use is above 85% |
| `NodeHighMemory` | warning | 10m | Memory use is above 90% |
| `NodeMemoryCritical` | critical | 5m | Memory use is above 95% |
| `NodeDiskSpaceLow` | warning | 10m | A filesystem is above 85% |
| `NodeDiskSpaceCritical` | critical | 5m | A filesystem is above 92% |
| `NodeDown` | critical | 3m | node-exporter on a node cannot be scraped |

## Workloads

| Alert | Severity | For | Fires when |
|---|---|---|---|
| `PodCrashLooping` | warning | 5m | A container restarted more than 3 times in 15 minutes |
| `PodNotReady` | warning | 15m | A pod is Pending or Unknown |
| `ContainerNearMemoryLimit` | warning | 5m | A container uses more than 90% of its memory limit |
| `PVCAlmostFull` | warning | 10m | A volume is above 85% |
| `PVCCritical` | critical | 5m | A volume is above 92% |
| `PodFailed` | warning | 5m | A pod is in phase Failed |
| `JobFailed` | warning | 5m | A Job has failed pods |
| `DaemonSetIncomplete` | warning | 10m | A DaemonSet has nodes without a ready pod |
| `StatefulSetNotReady` | warning | 15m | A StatefulSet has fewer ready replicas than desired |

## Databases

| Alert | Severity | For | Fires when |
|---|---|---|---|
| `CNPGReplicationLagHigh` | warning | 5m | A replica is more than 30 seconds behind (cluster-3 excluded) |
| `CNPGReplicationLagCritical` | critical | 5m | A replica is more than 120 seconds behind (cluster-3 excluded) |
| `CNPGInstanceDown` | critical | 3m | An instance's metrics cannot be scraped |
| `CNPGDelayedReplicaStuck` | warning | 10m | cluster-3 is more than 600 seconds behind instead of about 300 |
| `WALArchivingFailing` | critical | immediately | WAL archiving to Garage failed in the last 10 minutes |
| `CNPGRowsDeletedSpike` | critical | 1m | Rows are deleted at more than 10 times the usual rate |
| `CNPGRowsUpdatedSpike` | critical | 1m | Rows are updated at more than 10 times the usual rate |
| `CNPGStackedInstances` | warning | 15m | More than one instance of a cluster runs on the same node |
| `CNPGBackupTooOld` | critical | 10m | The last base backup is more than 8 hours old |

## Platform

| Alert | Severity | For | Fires when |
|---|---|---|---|
| `ScrapeTargetDown` | warning | 5m | Prometheus cannot scrape a target |
| `EndpointDown` | critical | 2m | A blackbox probe fails |
| `KyvernoAdmissionDegraded` | critical | 2m | Fewer than a majority of Kyverno admission replicas are ready |
| `KyvernoAdmissionDown` | critical | 1m | No Kyverno admission replica is ready |
| `Watchdog` | none | always | Always. It proves the alerting pipeline works. |

## Storage, certificates and etcd

| Alert | Severity | For | Fires when |
|---|---|---|---|
| `LonghornVolumeDegraded` | warning | 10m | A Longhorn volume has lost replicas |
| `LonghornVolumeFaulted` | critical | 1m | A Longhorn volume has no healthy replica |
| `GarageDiskLow` | warning | 15m | Less than 15% of a Garage data disk is free |
| `GarageDiskCritical` | critical | 5m | Less than 5% of a Garage data disk is free |
| `CertificateExpiringSoon` | warning | 1h | A probed certificate expires within 30 days |
| `CertificateExpiryCritical` | critical | 10m | A probed certificate expires within 7 days |
| `EtcdNoLeader` | critical | 1m | An etcd member has no leader |
| `EtcdFrequentLeaderChanges` | warning | 5m | More than 3 leader changes in an hour |
| `EtcdDatabaseNearQuota` | warning | 10m | The etcd database uses more than 80% of its quota |
| `EtcdSlowDisk` | warning | 10m | The 99th percentile WAL fsync takes more than 500 ms |

## Runtime security

| Alert | Severity | For | Fires when |
|---|---|---|---|
| `FalcoCriticalEvent` | critical | immediately | A Falco rule of priority Emergency, Alert or Critical matched in the last 5 minutes |
| `FalcoWarningEvent` | warning | immediately | A Falco rule of priority Error or Warning matched in the last 15 minutes |
| `FalcoDown` | critical | 5m | Falco on a node cannot be scraped |

## Logs

| Alert | Severity | For | Fires when |
|---|---|---|---|
| `PostgresDropStatement` | critical | immediately | A database cluster logs a `DROP` statement |

