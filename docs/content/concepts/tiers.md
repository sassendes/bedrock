---
title: "The tier model"
weight: 2
description: "Genesis, the fleet, provisioners and tenants: how the layers relate and which ones exist today."
---

bedrock is built as four layers. Each layer creates and looks after the layer below it, and nothing else.

{{< diagram "tiers" "Each layer reconciles only its own children. The tenant layer is built; the layers above it are designed." >}}

## The layers

**Genesis** is a one-time step run from an operator's workstation. It creates the root of trust and stands up the fleet, then stops. Nothing keeps running, so there is nothing to attack between runs. It installs only from a sealed, verified mirror and never reaches the internet.

**The fleet** is the control plane above everything. It decides where provisioners run, buys machines through provider integrations and holds the source of truth for every tenant. It runs on enough nodes that it can still agree something has failed while some of them are down.

**A provisioner** owns a pool of idle machines and turns them into tenants on request. It stamps the tenant's nodes, firewall pair, network, backup target and databases, then keeps the tenant matching its description over time. Each provisioner has a replica, and if one is lost another can take over its tenants.

**The tenant** is a single cluster on dedicated nodes. It is the part that runs a customer's workloads, and it is the part these docs cover.

## Recursion

The layers repeat one pattern: describe what you want, and a controller makes it true. A tenant's operators keep its pods running. A provisioner keeps its tenants matching their descriptions. The fleet keeps its provisioners alive. Genesis is where the recursion stops, which is why it holds no state and runs only when needed.

The same idea applies to watching for failure. A process should not be the only judge of whether it is healthy, so each layer is watched from outside:

- inside a tenant, separate observers decide whether the primary database is gone
- above a tenant, the provisioners and the fleet watch whole clusters
- above a site, offsite backups survive the loss of the building

## Why separate clusters

The cost of one cluster per tenant is real. Every tenant runs its own copy of the platform stack, which uses memory and CPU before any application starts. bedrock accepts that cost because the alternative, many tenants sharing one cluster, makes isolation a matter of configuration instead of a property of the design. For small tenants that do not need this level of separation, a lighter shared tier is an open design question on the roadmap.

## What is built

| Layer | Status |
|---|---|
| Tenant cluster | Built. Deploys from empty machines, passes CIS. |
| Provisioner | Designed |
| Fleet | Designed |
| Genesis | Designed |
