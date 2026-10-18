---
title: "Failure model"
weight: 8
description: "What a tenant survives on its own, what needs a person and the rules that decide between the two."
---

A tenant runs on three nodes. The failure model starts from that number and works out what can be lost without losing data or service.

## Quorum

etcd, Loki and Garage all depend on a majority agreeing. With three members, a majority is two, so one member can be lost. Losing two means the majority is gone, and the system stops accepting writes rather than risk two halves disagreeing. bedrock always prefers stopping to guessing.

## What survives what

| Failure | What happens | Needs a person |
|---|---|---|
| One node lost | etcd, Loki and Garage keep a majority, and the other Prometheus replica keeps scraping. Longhorn serves volumes from the remaining replicas. Pods on the dead node are deleted and rescheduled. | No |
| The cluster-1 primary instance lost | CloudNativePG promotes the second instance inside cluster-1. Synchronous replication means no acknowledged write is lost. | No |
| cluster-1 lost entirely | cluster-2 holds a live copy and is promoted. | Yes, today |
| A bad `DROP`, `DELETE` or `UPDATE` | An alert fires. cluster-3 is five minutes behind, so its replay can be paused before the change reaches it. | Yes |
| Kyverno down | Application pods cannot be admitted, because those policies fail closed. Databases, storage and the operators keep working, because policies on their namespaces fail open or do not apply. | No, but it pages |
| One Garage pod lost | Two full copies keep serving S3. | No |
| Two nodes lost | etcd loses its majority and the cluster stops accepting changes. | Yes |

## Fail open or fail closed

An admission policy has to decide what happens when its engine is unreachable. bedrock decides per namespace:

- **Application namespaces fail closed.** If Kyverno is down, an application cannot deploy. That is the policy doing its job.
- **Storage and database namespaces fail open, or are not covered at all.** If Kyverno is down, CloudNativePG must still be able to recreate a database pod and Longhorn must still be able to replace an instance manager. A policy engine should never be the reason a cluster cannot heal.

The priority class rule is enforced twice for the same reason: once by Kyverno, which fails open, and once by a ValidatingAdmissionPolicy inside the API server, which needs no external service at all.

## Kept alive on purpose

Because admission sits in front of every pod ever created, Kyverno's admission controller runs three replicas, one per node, with a disruption budget that keeps two, and the highest priority class. Falco runs at the data priority class so it is never the first thing evicted from a full node.

## Planned automation

{{< note "planned" >}}
A failover operator will handle the cases marked "yes, today". Three observers on separate failure domains must agree that cluster-1 is dead before anything happens. It then fences cluster-1, confirms no instance is running, promotes cluster-2 and points `postgres-rw` at it. For bad data, it pauses cluster-3's replay and rolls it forward to just before the mistake. If the observers disagree, it does nothing and pages a person.
{{< /note >}}
