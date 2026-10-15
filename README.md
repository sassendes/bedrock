# bedrock

Self-hosted Kubernetes platform for managed service providers. Every tenant gets its own cluster on dedicated nodes, hardened from the inside out, with nothing shared between tenants.

## Why

Most platforms put many customers on one cluster and draw the lines with namespaces. Bedrock doesn't. A tenant is a full cluster with its own nodes, storage, databases, observability, policy and network edge, so one tenant's problem can never become another's. It targets regulated organisations that need to prove isolation, not promise it.

## Architecture

```
genesis  →  v3 (control plane)  →  v2 (provisioners)  →  tenants
```

- **Tenant:** the workload cluster. This repository builds it today.
- **v2:** stamps tenants, owns their network edge, rolls upgrades across the fleet. Planned.
- **v3:** places v2s on providers and sites. Planned.
- **Genesis:** the sealed starting point the rest is rebuilt from. Planned.

## The tenant cluster

| Layer | Component |
|---|---|
| Kubernetes | RKE2 with the CIS profile, secrets encryption, audit logging |
| Network | Cilium with kube-proxy replacement, WireGuard encryption between nodes, Hubble |
| Policy | Kyverno, Pod Security Admission, a native ValidatingAdmissionPolicy |
| Block storage | Longhorn |
| Object storage | Garage, 3 nodes, replication factor 3 |
| Databases | CloudNativePG: a primary, a live replica and a 5-minute delayed replica, poolers, WAL and base backups to Garage |
| Observability | Prometheus, Alertmanager, Loki, Tempo, Alloy, Grafana, OpenCost, blackbox |
| Runtime security | Falco |
| Event buses (optional) | NATS JetStream, Redis with Sentinel, Kafka on Strimzi |

## Security posture

- Default deny on every pod in every namespace. Each flow is allowed explicitly and pinned to the exact pods on both sides.
- Nothing reaches the internet. No NodePorts, no LoadBalancers, and cluster DNS does not forward outside the cluster.
- Platform services run under the `restricted` Pod Security level. Only the two node agents that need the host run privileged, in their own namespace.
- Least-privilege RBAC: no wildcards, no component reading secrets it does not own, replicas read the primary's backups with a read-only key.
- Postgres superuser login is off and every client connection must use TLS.
- Every upstream bundle is checksum-verified before it is applied.
- kube-bench (`rke2-cis-1.9`): 85 pass, 1 fail accepted by design (operators need API tokens), 1 check not applicable (kube-proxy is replaced by Cilium).

## Repository layout

```
bootstrap/       ansible playbooks, RKE2 and Cilium config, audit policy
netpols/         Cilium network policies, one file per namespace plus the cluster-wide default deny
kyverno/         admission policies
database/        CloudNativePG clusters, poolers, backups
storage/         Garage, bucket initialisation, Longhorn settings
observability/   metrics, logs, traces, dashboards, alerting
event-buses/     NATS, Redis, Kafka
falco/           runtime security
secrets/         placeholder secrets, replaced by OpenBao
```

## Running it

Requirements: three Debian 12 nodes (4 vCPU, 9 GB RAM, 200 GB disk absolute minimium), SSH key access, and a controller with Ansible.

```sh
sudo ./scripts/ansible_deps.sh
# fill in bootstrap/hosts.ini
make deploy
```

`make teardown` wipes RKE2, etcd and every Longhorn volume on all nodes. It asks you to type `destroy` first.

Dashboards are ClusterIP only. Until the WireGuard edge exists, reach them with `kubectl port-forward`.

## Status

The tenant layer runs end to end on a three-node lab. Next: Reloader, the Go failover operator, OpenBao, then the network edge (OPNsense, WireGuard, nginx), disaster recovery, and the v2 and v3 tiers.

The secrets in `secrets/` are placeholders for a public repository and must never be used as they are.
