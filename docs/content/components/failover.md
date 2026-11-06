---
title: "Failover controller"
weight: 8
description: "bedrock's own controller that promotes cluster-2 when cluster-1 is lost, only on proof: how it decides, what it is allowed to do and what stops it."
---

The failover controller is written for bedrock. It watches the primary PostgreSQL cluster, and when cluster-1 is lost it fences it, promotes cluster-2, moves the other replicas to follow cluster-2 and points `postgres-rw` at cluster-2's pooler.

It does this only on proof. Postgres proves it is alive by signing a challenge. Death needs a majority of nodes and three independent witnesses. The part that acts holds no authority of its own: admission refuses every one of its writes unless a separate verifier approved that exact run.

| | |
|---|---|
| Namespace | `bedrock-failover` |
| Image | `ghcr.io/sassendes/bedrock-failover`, one image with four binaries, pinned by digest |
| Code | `failover-controller/`, Go, with TLA+ models in `failover-controller/model/` |
| Manifests | `tenant-workload/failover/` |
| Deployed by | The bootstrap, when `failover_enabled` is true (the default) |
| Priority class | `bedrock-data` |

{{< diagram "failover-parts" "Observers vote, the verifier judges and approves, the leading decider acts, the checker is the fallback. Each ConfigMap has one writer." >}}

## The parts

| Part | Runs as | Does | Never does |
|---|---|---|---|
| Observer | Two DaemonSets, `bedrock-observer-a` and `bedrock-observer-b`, so two per node | Writes a heartbeat to the primary, has postgres sign the round's challenge, votes up, down or abstain | Act on a database |
| Verifier | One Deployment, `Recreate` | Issues a challenge every round, checks every signature, asks the four witnesses, approves or revokes a run | Touch a database or a cluster object |
| Decider | One Deployment with two pods on different nodes, one holds the `bedrock-decider` Lease | Counts the votes, opens a run, fences, promotes, repoints, flips | Act without the verifier's approval |
| Checker | One Deployment | Wakes when the normal path is broken. Halts everything if the database is up, pages if it is down | Fail anything over |

Each part has its own service account. The observers, the verifier and the checker can read the clusters but change nothing in them. Only the decider has write permissions on clusters, pods and services, and admission gates every one of those writes.

## Proof of life

Every observer connects to the primary as `bedrock_observer` over TLS, verifying the cluster's CA, and runs two statements:

1. It writes its heartbeat row with a nonce and reads it back. The write must commit.
2. Only after the commit, it calls `bedrock_prove(challenge, observer)`. Postgres returns an HMAC-SHA256 over the key id, the challenge, the observer, the instance, the system identifier, the timeline, the WAL position, whether it is in recovery and the time.

The key sits in `bedrock_proof_key`. The observer login cannot read that table. The function runs as its owner, so postgres signs with a key the observer never sees. The verifier holds the same key in the `bedrock-proof-keys` Secret and checks every signature.

A signature is valid only if it covers this round's challenge, names the observer that carries it, verifies with a known key and says the instance is not in recovery. A valid signature is called an up, and one up outweighs every vote that says dead.

| Postgres state | Heartbeat write | Signed | Vote |
|---|---|---|---|
| Primary, healthy | Commits | Yes | Up |
| Standby | Refused, read only | No | Not primary |
| Primary, commit stuck (lock, synchronous standby gone, disk full) | Times out | No | Down, marked stuck |
| Primary with `default_transaction_read_only` | Refused while not in recovery | No | Down, marked stuck |
| Not reachable | No connection | No | Asks the other observers first |

The heartbeat table, the key table, `bedrock_prove` and `bedrock_receiver` are in `failover-controller/observe/heartbeat.sql`. The bootstrap loads them into the cluster-1 primary on every deploy. The replica clusters receive them through replication.

## Votes

An observer that cannot reach the primary does not vote down on its own. It asks the other observers over mutual TLS on port 8443, each of which checks the primary itself and signs its answer:

| What the others report | The observer votes |
|---|---|
| Down as well | Down |
| Up | Abstain, its own path is broken |
| No answers | Abstain, it is isolated |

Each observer writes its votes to its own ConfigMap, `vote-<node>-<slot>`. Admission reads the node and the pod name from the observer's service account token, so an observer can write only its own object.

The two observers on a node count as one vote:

| Observer a | Observer b | The node votes |
|---|---|---|
| Down | Down | Down |
| Up, signed | anything | Up |
| Down | Abstain or missing | Abstain |

The node that runs the primary does not vote on it. A vote counts only while it is fresh: the decider measures its age on its own clock from the moment its sequence number changed, and drops it after 6 seconds.

## Rounds and the verifier

The verifier closes a round every 4 seconds, whatever the evidence shows, and opens the next one with a new random challenge in `bedrock-round`. A signature belongs to one round, so one taken before a crash cannot be replayed later.

For each primary, the verifier counts the node votes and asks four witnesses:

| Witness | Says dead when | Says alive when | Silent when |
|---|---|---|---|
| Replicas | Every replica streaming from the primary reports its stream gone or older than 10 seconds | Any of them streams | None can be reached |
| Instance manager | `/readyz` on port 8000 answers anything but 200, over TLS checked against the cluster's CA | `/readyz` answers 200 | It cannot be reached |
| Kubelet | The postgres container is terminated, waiting or not Ready, read from the pod status in the API | The container is running and Ready | The node is not Ready, so its report is stale |
| API | CloudNativePG recorded the instance as not primary | It recorded it as primary | Nothing is recorded |

The kubelet is read through the API and never through the kubelet proxy, because the permission to use that proxy also allows running commands in pods.

| Outcome | When |
|---|---|
| Up | At least one valid signature |
| Proven dead | No valid signature, a majority of eligible nodes vote down and 3 of the 4 witnesses say dead |
| Contradiction | A valid signature while 3 witnesses say dead. Nothing acts, a person is paged |
| Unknown | Anything else. Nothing acts |

A silent witness never counts as dead. A dead node therefore leaves the kubelet and the instance manager silent, and the outcome stays unknown: a dead node is handled by CloudNativePG inside cluster-1, not by this controller.

If two different instances sign as primary in two consecutive rounds, the verifier pages a split brain. One round of it is normal during a switchover.

## Deciding to act

Every database cluster declares its role with the label `bedrock.io/db-role`: `primary`, `live` or `delayed`. The controller works the role out from the spec as well (`spec.replica.enabled` false is a primary, a replica with `minApplyDelay` is delayed, any other replica is live) and the two must agree. A cluster with no label, or one whose label contradicts its spec, is refused and paged, and a refused primary takes its whole family out of every decision. A relabel by hand can therefore never get a delayed replica promoted. A promotion sets the label to `primary` in the same write that ends replica mode.

The leading decider ticks every second. A primary becomes suspect when a majority of eligible nodes vote down and no node votes up. It is declared dead after it stays suspect for:

| Cluster-1 has | Wait |
|---|---|
| A Ready standby instance | 60 seconds, so CloudNativePG can promote it first |
| No Ready standby | 30 seconds |

CloudNativePG itself starts a promotion inside the cluster within about 10 to 30 seconds. While its phase says it is failing over, switching over, upgrading or restarting the primary, the decider waits and clears its suspicion.

Even when a primary is dead, the decider holds and pages instead of acting when:

| Hold | Reason |
|---|---|
| The cluster has the annotation `bedrock.io/failover-pause: "true"` | A person paused it |
| `dataDurability: required` | A dead standby looks like a dead primary when commits wait for it |
| More than one observer node abstains or is silent | The vote is too thin |
| With more than two primaries watched, two or more are suspect at once | Something wider is wrong, such as the network |
| The last failover of this primary was less than 10 minutes ago | Cooldown |
| One failover already ran in the last hour, across all primaries | Cap |
| No live replica has a Ready instance | There is nothing safe to promote |

The delayed replica, cluster-3, is never promoted automatically. The verifier does not take the API's word for which replica is delayed: it asks the candidate's postgres for `recovery_min_apply_delay` and refuses anything but 0.

A disaster recovery cluster in another datacenter is not part of this. It pulls from the primary from outside, and the controller never sees it.

## A failover run

The decider opens a run by writing `claim-<namespace>.<name>`. Writing it is a compare and swap on the object's version, so two deciders can never open the same run. The claim records an epoch, the instance that died, the candidate, the followers and the nodes that ran an instance. Every step after that reads only the claim, so a decider that takes over the lease resumes the same run.

{{< diagram "failover-run" "Each step is safe to repeat. Before the fence, any proof of life ends the run." >}}

| Step | The decider | It moves on when |
|---|---|---|
| Claimed | Waits for the verifier's approval | The approval is valid for this epoch and instance, and the verifier is still closing rounds. The step is saved as Fencing before cluster-1 is touched |
| Fencing | Sets `cnpg.io/reconciliationLoop: disabled` and `cnpg.io/hibernation: "on"` on cluster-1, deletes its instance pods | No instance pod exists and every node that ran one has been Ready without a break since before the claim |
| Fenced | Checks the fence again, then sets `spec.replica.enabled: false` on cluster-2 | The fence still holds and the patch is applied |
| Promoting | Waits for cluster-2 | cluster-2 is healthy, or after 5 minutes with a page, since cluster-1 is fenced |
| Promoted | Points every follower's `spec.replica.source` at cluster-2 | Done for every follower, each one that cannot be moved is paged |
| Repointed | Points every `ExternalName` Service labelled `bedrock.io/points-at` at cluster-2's pooler | Done |
| Flipped | Closes the claim | |

A fence is verified on node readiness, not only on the pod list. A pod that was force deleted while its node was away may still run there when the node returns, so any Ready transition after the claim fails the fence. A fence that does not verify within a minute halts the run and pages. Because the step is saved before the fence starts, a run never closes with cluster-1 half fenced. A cluster that carries a fence annotation with no open or halted run behind it pages.

The verifier approves a run only when all of these hold:

- the outcome for this round is proven dead
- the instance proven dead is the one the claim names, and it is still the current primary
- if cluster-1 has a Ready standby, the same instance was proven dead in every round for the last 60 seconds
- the checker is not halting

Then the verifier checks the candidate itself. It refuses and pages when the candidate is not a live replica of cluster-1, has no Ready instance, cannot be read, or runs with a `recovery_min_apply_delay` other than 0. It then compares how far the candidate has replayed, read with `pg_last_wal_replay_lsn()`, with the highest WAL position postgres ever signed for cluster-1:

| Candidate | The verifier |
|---|---|
| At or past the signed position | Approves at once. No signed write is lost |
| Behind, and its replay still moves | Waits. The candidate is still replaying WAL cluster-1 archived to Garage, so every second of waiting saves writes. This is also the case of a live replica bootstrapped again from Garage after an earlier failover: CloudNativePG reports it Ready while it replays, so the wait is what keeps those writes |
| Behind, and its replay has not moved for 12 seconds | Approves, so the database is never left down waiting for a person, and reports the gap in bytes. Whatever is missing was never archived and cannot be replayed. `BedrockFailoverLostWrites` pages with it |
| No signed position for cluster-1 to compare with | Follows the same rule: waits while the replay moves, approves once it stops, and reports the loss as unknown (-1). `BedrockFailoverLostWrites` pages with it |

The 12 seconds assume Garage hands over each WAL segment faster than that. If it is slower, raise `-stall`.

The approval names the candidate and the host `postgres-rw` may point at. A revoked or closed approval names neither.

Until the fence starts, the run ends without a fence when a fresh up arrives, the verifier revokes, the checker halts, or CloudNativePG has moved the primary to another instance. After the fence starts, the old primary cannot sign, so the approval stands and the run is finished.

The decider also refuses to act on an approval once the verifier has stopped closing rounds for 15 seconds, measured on its own clock and from the round's issue time.

Writes made in the last seconds before cluster-1 died, after the last signature, are not covered by any proof. Like any asynchronous replica, cluster-2 can miss them.

## Admission

The decider has the permissions to fence and promote. Admission decides when it may use them. The policies are ValidatingAdmissionPolicies in `tenant-workload/failover/admission.yaml` and run inside the API server.

| Policy | Rule |
|---|---|
| `bedrock-failover-params-stay` | Nobody deletes the Lease, `bedrock-approvals` or `bedrock-checker`, because the policies below read them |
| `bedrock-vote-owner` | An observer writes only `vote-<its node>-<its slot>`, with matching `node` and `slot` inside, and never deletes |
| `bedrock-court-writers` | Only the verifier writes `bedrock-round` and `bedrock-approvals`, only the checker writes `bedrock-checker`, only an observer writes `vote-*`, only the decider writes `claim-*`, only Kyverno's background controller writes `db-ca-*`, and none of them writes anything else here |
| `bedrock-decider-holds-lease` | A decider write comes from the pod that holds the Lease, and its only ConfigMaps are claims |
| `bedrock-decider-approved` | A cluster or Service write carries an epoch with a valid approval. A cluster write changes `spec.replica.enabled` and `spec.replica.source` and nothing else, and never turns a cluster into a replica. Only the cluster an approval names in `.candidate` is promoted. Only the approved target whose current primary is the approved instance is fenced. Only a follower is repointed. The only label a decider changes is `bedrock.io/db-role`, only to `primary`, and only in the write that promotes; a promotion that does not set it is refused. `postgres-rw` may only point at the approval's `.host`, which must end in `.svc.cluster.local`. A missing key denies. A pod delete targets an instance pod of a cluster with a valid approval |
| `bedrock-failover-pods` | A pod in `bedrock-failover` runs only the `bedrock-failover` image pinned by digest, with cpu and memory limits. This lives in the API server and not in Kyverno, so the controller's pods can always be recreated even while Kyverno is down |
| `bedrock-checker-halt` | While the checker holds any primary, every decider write to clusters, pods and Services is refused |

The policies that read an object deny everything they match when the object is missing. That is why `admission.yaml` creates those objects first, why their deletion is refused, and why the policies only match namespaces labelled `bedrock.io/failover-scope: "true"`: `cluster-1`, `cluster-2`, `cluster-3`, `application` and `bedrock-failover`. To remove the controller, delete the policies and their bindings before the namespace.

## The checker

The checker sleeps while the normal path works. It wakes for a primary when any of these is true:

- the verifier has not closed a round for 15 seconds
- the `bedrock-decider` Lease has expired
- fewer than a majority of eligible nodes have both observers voting in the current round

Awake, it calls the primary's `/readyz` itself:

| The database | The checker |
|---|---|
| Up | Writes `<target>.halt: "true"` to `bedrock-checker`. Admission blocks every decider action, the verifier revokes, the decider closes its claim. No page, a healthy database needs nobody |
| Down | Pages a person, because the normal path cannot act |

It never halts once a fence has started, because finishing a run is safer than stopping half way.

## A primary that stops writing

A primary that answers but cannot commit is not proven dead: the replicas, the instance manager, the kubelet and the API still see it alive. The controller never fails it over. Every observer reports how long its writes have been stuck in `bedrock_observer_write_stuck_seconds`, and `BedrockPrimaryNotWriting` pages when every observer has seen no commit for two minutes.

## Objects it keeps

| Object | Kind | Written by | Holds |
|---|---|---|---|
| `vote-<node>-<slot>` | ConfigMap | That observer | Its votes per primary, its public exchange key, a sequence number |
| `bedrock-round` | ConfigMap | Verifier | The round number and challenge, last round's valid ups, signers, outcomes, the highest signed WAL position, when each primary was first proven dead |
| `bedrock-approvals` | ConfigMap | Verifier | One slot per primary: state, epoch, round and the approved instance |
| `bedrock-checker` | ConfigMap | Checker | One halt flag per primary |
| `claim-<namespace>.<name>` | ConfigMap | Leading decider | One run: epoch, step, instance, candidate, followers, nodes, timings, notes |
| `db-ca-<cluster>` | ConfigMap | Kyverno | The `ca.crt` of every database cluster, copied out of each namespace labelled `bedrock.io/database: "true"`. Observers, the verifier and the checker list them by the label `bedrock.io/db-ca` every 30 seconds, so a cluster added later is trusted without editing any volume. The CA keys are never copied |
| `bedrock-decider` | Lease | Deciders | Which decider leads |

## Secrets

| Secret | Used by | Source |
|---|---|---|
| `bedrock-observer` | Observers, verifier | Copied from `cluster-1` by Kyverno (`tenant-workload/kyverno/generate/failover-secret-sync.yaml`) |
| `bedrock-proof-keys` | Verifier | Created by the bootstrap from `~/.bedrock/failover/proof-keys` on the controller machine |
| `bedrock-observer-exchange` | Observers | Created by the bootstrap from a CA and certificate generated once in `~/.bedrock/failover/exchange` |

The proof key is generated once on the controller machine and never leaves it except into postgres and the verifier. To replace it, see [rotate the proof key]({{< relref "/tasks/rotate-proof-key" >}}).

## Network

`tenant-workload/failover/netpol.yaml` covers the namespace. The database namespaces allow the matching ingress.

| From | To | Port |
|---|---|---|
| Observers | Observers | 8443 |
| Observers | Database instances | 5432 |
| Verifier | Database instances | 5432, 8000 |
| Checker | Database instances | 8000 |
| All four | API server | 6443 |
| Prometheus | All four | 9090, `/metrics` only |
| Kubelet | All four | 9090, `/healthz` |

## Settings

Every value is a flag on the binary. The defaults are the values in use.

| Flag | Binary | Default | Effect |
|---|---|---|---|
| `-probe-interval` | observer | 2s | Time between checks |
| `-suspect-probe-interval` | observer | 500ms | Time between checks while a primary looks down |
| `-check-timeout` | observer, verifier | 1s | Deadline of one check, also the `statement_timeout` |
| `-ask-timeout` | observer | 1.5s | How long to wait for the other observers |
| `-round` | verifier | 4s | Length of a round |
| `-dead-witnesses` | verifier | 3 | Witnesses that must say dead |
| `-cnpg-grace` | verifier, decider | 1m | CloudNativePG's time to promote inside the cluster |
| `-max-stream-lag` | verifier | 10s | A replica stream older than this counts as gone |
| `-stall` | verifier | 12s | A candidate behind the last signed position is approved once its replay has not moved for this long |
| `-dead-after` | decider | 30s | Time suspect before dead, without a Ready standby |
| `-freshness` | decider | 6s | How long a vote counts |
| `-cooldown` | decider | 10m | No second failover of the same primary within this |
| `-cap-count`, `-cap-window` | decider | 1, 1h | Failovers allowed per window, all primaries together |
| `-brake-count` | decider | 2 | Primaries suspect at once that stop every decision |
| `-max-abstain` | decider | 1 | Abstaining or silent nodes tolerated |
| `-claim-timeout` | decider | 5m | A run not fenced within this halts |
| `-fence-verify-timeout` | decider | 1m | A fence not verified within this halts |
| `-promote-timeout` | decider | 5m | How long to wait for the promoted cluster to be healthy |
| `-round-stale` | decider, checker | 15s | The verifier counts as stalled after this |
| `-observers-per-node` | decider, verifier, checker | 2 | Observers folded into one node vote |
| `-flip-namespaces` | decider | application | The only namespaces whose `ExternalName` Services are repointed |

## Pausing failover

Set the annotation on the primary cluster:

```sh
kubectl -n cluster-1 annotate cluster cluster-1 bedrock.io/failover-pause=true
```

The controller keeps watching and paging but never acts on that cluster. Remove the annotation to resume:

```sh
kubectl -n cluster-1 annotate cluster cluster-1 bedrock.io/failover-pause-
```

## Monitoring

All four parts serve `/metrics` and `/healthz` on port 9090. `/healthz` returns 503 when the last good round is older than about three intervals, so a part that hangs is restarted. A PodMonitor in `tenant-workload/failover/monitoring.yaml` scrapes them, and the same file holds the alerts, listed in [alert rules]({{< relref "/reference/alerts" >}}).

| Metric | Meaning |
|---|---|
| `bedrock_observer_ballot` | Each observer's current vote per primary |
| `bedrock_observer_write_stuck_seconds` | How long the primary has answered without committing |
| `bedrock_observer_peers` | Other observers this one reaches |
| `bedrock_verifier_outcome` | Up, proven dead, contradiction or unknown, per primary |
| `bedrock_verifier_dead_witnesses` | Witnesses that say dead |
| `bedrock_verifier_round` | The current round number |
| `bedrock_verifier_promoted_behind_bytes` | Signed WAL the new primary did not have at approval, per run; 0 when nothing was lost, -1 when unknown |
| `bedrock_failover_state` | Alive, suspect or dead, per primary |
| `bedrock_failover_votes` | Up, down and abstain counts, per primary |
| `bedrock_failover_claim_open` | Open runs and their step |
| `bedrock_failover_leader` | 1 on the decider that leads |
| `bedrock_checker_active`, `bedrock_checker_halt` | Whether the checker is awake and halting, per primary |

## Model checking

Each rule was written as a TLA+ model and checked with TLC before it was coded. The models are in `failover-controller/model/`. Every rule has a variant with the rule removed that TLC must break, and canaries that prove the good paths are reached.

| Model | Checks |
|---|---|
| `Proof` | Grades, rounds on a timer, split brain over two rounds |
| `Evidence` | Witnesses and votes never prove a live database dead |
| `Approval` | One approval slot per primary, no write from an old run |
| `Instances` | A force deleted pod never leaves two primaries |
| `Pairs` | Two observers per node: two liars anywhere cannot vote a live database down |
| `Halt` | A stalled verifier with a recovered database never leads to a fence |
| `Promotion` | Sign after commit, the CloudNativePG minute, the catch-up window, the instance pin, the stuck write page. No run ever waits for a person |
| `Candidate` | Only a ready live replica with no apply delay is promoted, and only what the verifier checked. Fence, promote and flip are bound to the approval. A candidate is never promoted while its replay could still move |
| `HalfFence` | The run is saved as fencing before the fence starts, so a half done fence is never orphaned |
| `Signer` | A signature counts for the cluster it names, never a sibling sharing the system identifier |
| `Decider`, `Exchange`, `Fallback` | The claim steps, the observer exchange, the earlier fallback design |

## What it does not do

- It does not handle a bad `DROP`, `DELETE` or `UPDATE`. Pausing cluster-3's replay and rolling it forward is done by a person.
- It does not handle a dead node. CloudNativePG promotes a standby inside cluster-1.
- It does not add a live replica after a failover. cluster-1 stays fenced and there is no live replica left, so `BedrockNoLiveReplica` pages and a person runs `make add-replica REPLICA=<new name>`. The new replica streams from the current primary, and every other replica is prepared to follow it if it is promoted later.
- It does not fail over a primary that is alive but not writing. It pages.
