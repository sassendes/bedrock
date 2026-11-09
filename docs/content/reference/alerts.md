---
title: "Alert rules"
weight: 5
description: "Every alert rule that ships with a tenant, with its severity and how long the condition must hold."
---

Most rules live in `tenant-workload/observability/alerting/alert-rules.yaml` as one `PrometheusRule`. Falco's rules live next to Falco in `tenant-workload/observability/falco/falco.yaml`. Three more, `PostgresDropStatement`, `FalcoCriticalEventLog` and `AuditLogSilent`, are Loki rules in `tenant-workload/observability/loki/loki.yaml`. They read log lines instead of metrics.

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
| `RuleEvaluationFailing` | critical | 5m | Prometheus or Loki's ruler fails to evaluate a rule group. A rule that cannot evaluate can never fire |
| `AlertSendingFailing` | critical | 10m | Prometheus or Loki's ruler cannot deliver alerts to Alertmanager |
| `AlertmanagerNotificationsFailing` | critical | 10m | Alertmanager fails to deliver through one of its integrations |
| `PrometheusConfigReloadFailed` | warning | 10m | Prometheus rejected its last configuration and keeps the previous one |
| `AlertmanagerConfigReloadFailed` | warning | 10m | Alertmanager rejected its last configuration and keeps the previous one |
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
| `FalcoCriticalEventLog` | critical | immediately | A Falco event of priority Emergency, Alert or Critical, or a reverse shell, appears in Loki in the last 5 minutes. It reads the event itself, so it fires on the very first one |

## Failover

These live in `tenant-workload/failover/monitoring.yaml`.

| Alert | Severity | For | Fires when |
|---|---|---|---|
| `BedrockFailoverPage` | critical | immediately | The decider or the verifier needs a person, with the reason |
| `BedrockPrimaryProvenDead` | critical | immediately | A primary is proven dead |
| `BedrockPrimaryContradiction` | critical | 1m | A primary signed that it is alive while three witnesses say dead; nothing will act |
| `BedrockFallbackDatabaseDown` | critical | immediately | The normal path is broken and the checker sees the database down |
| `BedrockFailoverLostWrites` | critical | immediately | A failover promoted a replica that was behind the last signed write; the value is the gap in bytes, -1 if unknown |
| `BedrockPrimaryNotWriting` | critical | immediately | Every observer has seen the primary answer without committing a write for over two minutes |
| `BedrockFailoverClaimStuck` | critical | 10m | A failover has been open for ten minutes |
| `BedrockDeciderNoLeader` | critical | 1m | The deciders do not have exactly one leader |
| `BedrockVerifierStalled` | critical | 1m | The verifier has not closed a round for a minute |
| `BedrockCheckerSilent` | warning | 1m | The checker has not finished a round for a minute |
| `BedrockObserverBlind` | warning | 2m | An observer cannot discover the clusters or publish its vote |
| `BedrockObserverAlone` | warning | 5m | An observer reaches no peers |
| `BedrockFailoverBlind` | critical | 2m | The verifier has neither a signed up nor a proof of death for a primary, so a real failover cannot happen |
| `BedrockNoLiveReplica` | critical | 10m | A primary has no ready live replica to promote. Add one with `make add-replica` |
| `BedrockCheckerHoldsFailover` | critical | 10m | The checker has held every failover for ten minutes |

## Logs

| Alert | Severity | For | Fires when |
|---|---|---|---|
| `PostgresDropStatement` | critical | immediately | A database cluster logs a `DROP` statement |
| `AuditLogSilent` | critical | 5m | No API audit log line has reached Loki for 15 minutes. `PostgresDropStatement` cannot fire while this fires |
| `AlloyDroppingLogs` | critical | immediately | Alloy gave up on log lines after every retry, in the last 10 minutes |
| `AlloyPushFailing` | warning | 10m | Alloy's pushes to Loki get a non-2xx answer. A 403 means a layer 7 rule refuses the push |

