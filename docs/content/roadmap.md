---
title: "Roadmap"
weight: 10
description: "What is built, what comes next and in what order."
---

bedrock is built from the bottom up. Each layer is finished and tested before the one above it starts.

{{< diagram "roadmap" "Built work on the left, planned work to the right." >}}

## Built

The tenant cluster: RKE2 with CIS, Cilium with default deny and WireGuard, Kyverno and Pod Security, Longhorn, encrypted Garage, PostgreSQL clusters with backups, the full observability stack with alerting, Falco, Reloader, digest-pinned images and a registry allowlist. It deploys from empty machines with one command and passes the CIS benchmark.

The failover controller: when cluster-1 is lost, it promotes cluster-2 and points `postgres-rw` at it, only on proof. Postgres signs its own proof of life, two observers per node vote, a separate verifier needs three kinds of witness to call it dead, CloudNativePG gets the first minute to fix it inside the cluster, and every action the controller takes is gated by admission. Its logic is checked with TLA+ before it is coded.

## Next, inside the tenant

- **Adding a live replica after a failover.** A playbook that bootstraps a new live replica behind the new primary, so a second failover is possible.
- **OpenBao.** One secrets store per tenant, replacing every placeholder secret and holding the disk encryption key.
- **InfluxDB.** Network metrics from the tenant's firewall, once the edge sends them.
- **Layer 7 rules** on the flows into Alertmanager, Loki and the Prometheus API, so each client can only call the endpoints it needs.

## Then, around the tenant

- **The edge.** A pair of OPNsense firewalls per tenant, managed by an operator, with VLANs, WireGuard access, BGP for service addresses and an egress allowlist.
- **Disaster recovery.** An offsite backup machine, timed restore drills and a restore mode for the bootstrap.

## Then, above the tenant

- **Provisioners.** Creating tenants from a description, a pool of standby machines, rolling upgrades and fleet-wide alerting.
- **Fleet.** Placing provisioners across providers and sites, failure quorum for the fleet and central observability.
- **Genesis.** A sealed mirror, signed artifacts and a key ceremony for the root of trust.
