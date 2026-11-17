---
title: "Roadmap"
weight: 10
description: "What is built, what comes next and in what order."
---

bedrock is built from the bottom up. Each layer is finished and tested before the one above it starts.

{{< diagram "roadmap" "Built work on the left, planned work to the right." >}}

## Built

The tenant cluster: RKE2 with CIS, Cilium with default deny and WireGuard, Kyverno and Pod Security, Longhorn, encrypted Garage, PostgreSQL clusters with backups, the full observability stack with alerting, Falco, Reloader, digest-pinned images and a registry allowlist. It deploys from empty machines with one command and passes the CIS benchmark.

Layer 7 rules on every plain HTTP flow, so each client can only call the endpoints it needs.

OpenBao, one per tenant: three pods on raft, initialized and unsealed by the bootstrap.

The failover controller: when cluster-1 is lost, it promotes cluster-2 and points `postgres-rw` at it, only on proof. Postgres signs its own proof of life, two observers per node vote, a separate verifier needs three kinds of witness to call it dead, CloudNativePG gets the first minute to fix it inside the cluster, and every action the controller takes is gated by admission. Its logic is checked with TLA+ before it is coded. It was tested live on a real cluster: cluster-1 was killed and cluster-2 took over in 73.6 seconds with no signed write lost. After a failover, `make add-replica` puts a new live replica behind the new primary.

Secrets from OpenBao: pods get their secrets through the Secrets Store CSI driver as files in memory and log in to OpenBao as themselves. Grafana, NATS, Valkey, Garage and the failover observers read theirs this way. OpenBao rotates the database application user's password every day and checks every database's certificate. The S3 keys stay Kubernetes Secrets readable only in their own namespace, and the disk encryption key stays the one static secret Longhorn reads.

## Next, inside the tenant

- **InfluxDB.** Network metrics from the tenant's firewall, once the edge sends them.

## Then, around the tenant

- **The edge.** A pair of OPNsense firewalls per tenant, managed by an operator, with VLANs, WireGuard access, BGP for service addresses and an egress allowlist.
- **Disaster recovery.** An offsite backup machine, timed restore drills and a restore mode for the bootstrap.

## Then, above the tenant

- **Provisioners.** Creating tenants from a description, a pool of standby machines, rolling upgrades and fleet-wide alerting.
- **Fleet.** Placing provisioners across providers and sites, failure quorum for the fleet and central observability.
- **Genesis.** A sealed mirror, signed artifacts and a key ceremony for the root of trust.
