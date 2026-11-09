---
title: "Failure model"
weight: 8
description: "What a tenant survives on its own, what needs a person and the rules that decide between the two."
---

The failure model starts from the number of nodes in a tenant and works out what can be lost without losing data or service.

## Quorum

etcd, Loki and Garage all depend on a majority agreeing. With N members, a majority is more than half of N, so fewer than half can be lost. Once the majority is gone, the system stops accepting writes rather than risk two halves disagreeing. An even number of members tolerates no more failures than the odd number below it, which is why these run on odd counts. bedrock always prefers stopping to guessing.

## What survives what

| Failure | What happens | Needs a person |
|---|---|---|
| A minority of nodes lost | etcd, Loki and Garage keep a majority, and the remaining Prometheus replicas keep scraping. Longhorn serves volumes from the remaining replicas. Pods on the dead node are deleted and rescheduled. | No |
| The cluster-1 primary instance lost | CloudNativePG promotes a standby instance inside cluster-1. Synchronous replication means no acknowledged write is lost. | No |
| cluster-1 lost entirely | The failover controller proves it dead, fences it, promotes cluster-2 and points `postgres-rw` at it. | No |
| A bad `DROP`, `DELETE` or `UPDATE` | An alert fires. cluster-3 is five minutes behind, so its replay can be paused before the change reaches it. | Yes |
| Kyverno down | Application pods cannot be admitted, because those policies fail closed. Databases, storage and the operators keep working, because policies on their namespaces fail open or do not apply. | No, but it pages |
| One Garage pod lost | Two full copies keep serving S3. | No |
| A majority of nodes lost | etcd loses its majority and the cluster stops accepting changes. | Yes |

## Fail open or fail closed

An admission policy has to decide what happens when its engine is unreachable. bedrock decides per namespace:

- **Application namespaces fail closed.** If Kyverno is down, an application cannot deploy. That is the policy doing its job.
- **Storage and database namespaces fail open, or are not covered at all.** If Kyverno is down, CloudNativePG must still be able to recreate a database pod and Longhorn must still be able to replace an instance manager. A policy engine should never be the reason a cluster cannot heal.

The priority class rule is enforced twice for the same reason: once by Kyverno, which fails open, and once by a ValidatingAdmissionPolicy inside the API server, which needs no external service at all.

## Kept alive on purpose

Because admission sits in front of every pod ever created, Kyverno's admission controller runs one replica per server node, with a disruption budget that allows only one down at a time, and the highest priority class. Falco runs at the data priority class so it is never the first thing evicted from a full node.

## The failover controller

A process should not be the only judge of whether it is healthy, so the decision to fail over is split between parts that do not trust each other. The full description is on the [failover controller]({{< relref "/components/failover" >}}) page.

- **Postgres proves it is alive.** Every round the verifier issues a fresh challenge. Postgres signs it with a key no observer can read, and only after a write has committed. One valid signature outweighs every vote that says dead.
- **Death needs evidence.** Two observers run on every node and count as one vote: the node votes down only when both say down. A majority of nodes must vote down, and three of four witnesses must answer dead: the replicas streaming from the primary, its instance manager, its container state, and what CloudNativePG last recorded. A witness that cannot be reached is silent, never dead.
- **CloudNativePG goes first.** While cluster-1 still has a Ready standby, nothing happens for a minute so CloudNativePG can promote inside the cluster. If it does, the claim is closed and nothing is fenced.
- **Availability first, with the least loss.** If cluster-2 is behind the last position postgres signed, it gets up to 30 seconds to replay what cluster-1 archived. Then it is promoted either way, and any gap is reported in bytes and paged. The database is never left down waiting for a person.
- **The decider holds nothing.** It has the permissions to fence and promote, and admission refuses every one of its writes unless the verifier approved that exact instance and epoch. Approvals are written only by the verifier.
- **A fallback that only stops things.** When the normal path is broken, a simple checker asks the database itself. If it is up, every failover action is blocked. If it is down, a person is paged.
- **Stuck writes page, never fail over.** A primary that answers but cannot commit for two minutes pages a person.

{{< note >}}
For bad data, pausing cluster-3's replay and rolling it forward to just before the mistake is done by a person. After a failover, a new live replica is added with one command, as described in [add a live replica]({{< relref "/tasks/add-replica" >}}).
{{< /note >}}
