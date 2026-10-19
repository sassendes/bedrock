---
title: "Roadmap"
weight: 10
description: "What is built, what comes next and in what order."
---

bedrock is built from the bottom up. Each layer is finished and tested before the one above it starts.

{{< diagram "roadmap" "Built work on the left, planned work to the right." >}}

## Built

The tenant cluster: RKE2 with CIS, Cilium with default deny and WireGuard, Kyverno and Pod Security, Longhorn, encrypted Garage, PostgreSQL clusters with backups, the full observability stack with alerting, Falco, Reloader, digest-pinned images and a registry allowlist. It deploys from empty machines with one command and passes the CIS benchmark.

## Next, inside the tenant

- **Failover operator.** A Go controller that promotes cluster-2 when cluster-1 is lost, and pauses and promotes cluster-3 when bad data is written, only after independent observers agree.
- **OpenBao.** One secrets store per tenant, replacing every placeholder secret and holding the disk encryption key.
- **InfluxDB.** Network metrics from the tenant's firewall, once the edge sends them.
- **Layer 7 rules** on the flows into Alertmanager, Loki and the Prometheus API, so each client can only call the endpoints it needs.

## Then, around the tenant

- **The edge.** A pair of OPNsense firewalls per tenant, managed by an operator, with VLANs, WireGuard access, BGP for service addresses and an egress allowlist.
- **Disaster recovery.** An offsite backup machine, timed restore drills and a restore mode for the bootstrap.

## Then, above the tenant

- **v2.** Creating tenants from a description, a pool of standby machines, rolling upgrades and fleet-wide alerting.
- **v3.** Placing provisioners across providers and sites, failure quorum for the fleet and central observability.
- **Genesis.** A sealed mirror, signed artifacts and a key ceremony for the root of trust.
