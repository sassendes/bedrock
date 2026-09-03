# last_chance — cross-cluster promotion (break-glass)

This folder is for one thing: manually promoting a replica cluster to a standalone primary when the source cluster is **confirmed dead**.

Read this before you run anything in here.

---

## what bedrock does automatically vs not

- **Instance failover (inside one cluster): automatic.** cnpg runs this. If a primary instance dies, cnpg promotes a replica instance in the same cluster. This is safe because one operator arbitrates it — no split-brain possible.
- **Cross-cluster promotion (cluster-1 dead, promote cluster-2): manual.** Bedrock does NOT automate this, on purpose.

---

## why cross-cluster promotion is manual

A single automated process cannot safely tell these two situations apart:

- the source cluster is actually **dead**
- the source cluster is **fine**, but this process just can't **see** it (network partition, API blip)

If a controller promotes a replica on a blip while the source is actually alive, you get **two primaries accepting writes** — split-brain. That is not an outage, it's **corruption**: two divergent histories that can never be merged. For clinical/financial data that's the worst possible outcome.

Promoting a replica is **irreversible**. Once promoted, the cluster forks its own WAL timeline and can never cleanly follow the old source again without a full rebuild. An irreversible, corrupting-on-mistake decision must be made by a human who has confirmed reality — not by a loop guessing from one viewpoint.

This is the CAP theorem in practice: during a partition you cannot have both consistency and availability. Bedrock chooses **consistency** — it waits for a human rather than risk a wrong automatic promotion.

---

## before you run the script: confirm the source is REALLY dead

Not "unreachable from my laptop." **Dead.** Check from more than one angle:

1. Cluster + pods state:

       kubectl get cluster <source> -n <source-ns>
       kubectl get pods -n <source-ns>

2. Independent witness — has the source stopped archiving WAL? If it were alive it would still be writing WAL to its own minio. A quiet bucket is the source confessing it's dead through a channel that doesn't depend on your network path to it:

       kubectl exec -n <source-ns> statefulset/minio -- sh -c \
         'mc alias set local http://localhost:9000 $MINIO_ROOT_USER $MINIO_ROOT_PASSWORD >/dev/null 2>&1; \
          mc ls --recursive local/<source>-backups/wals/ | tail'

   If new WAL is still landing, **the source is alive. Do not promote.**

When in doubt, do nothing. A few minutes of downtime is recoverable. Split-brain is not.

---

## running it

    ./promote-cluster.sh <target-cluster> <target-ns> <dead-source-cluster> <dead-source-ns>

Example — cluster-1 is dead, promote cluster-2:

    ./promote-cluster.sh cluster-2 cluster-2 cluster-1 cluster-1

What it does, in order:

1. Verifies the target is actually a replica cluster (won't double-promote).
2. Shows you the source state and makes you type `yes` that it's confirmed dead, then type the target name.
3. **Fences** the old source — scales it to `instances: 0` so a zombie source cannot keep serving writes after the new primary is live (fence-then-promote, never the reverse).
4. Promotes the target (`spec.replica.enabled: false`).

---

## after promotion

1. Confirm the target has a healthy primary:

       kubectl get cluster <target> -n <target-ns>

2. Confirm WAL forked to a new timeline (the number after `0000000` bumps).
3. Repoint apps' `DATABASE_URL` to `<target>-rw.<target-ns>.svc`.
4. The old source is fenced at 0 instances. **Do not just scale it back up.** It now holds a divergent history. If you want it back, **rebuild it fresh** as a replica of the new primary.

---

## scaling note

The one knob operators can freely turn is `instances:` per cluster (2, 3, 5, whatever). cnpg arbitrates instance failover, so any count is safe — no odd number needed (that's an etcd/quorum rule, and this isn't quorum). More instances = more read replicas and more failure headroom inside a cluster.
