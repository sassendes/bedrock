<p align="center">
  <img src="docs/static/images/logo.svg" alt="bedrock" width="96">
</p>

<h1 align="center">bedrock</h1>

<p align="center">
  A Kubernetes cluster engine for production workloads.<br><br>
  <a href="https://bedrockctl.com"><b>Documentation</b></a>
</p>

<p align="center">
  <a href="https://github.com/sassendes/bedrock/actions/workflows/failover-image.yml"><img src="https://github.com/sassendes/bedrock/actions/workflows/failover-image.yml/badge.svg" alt="ci"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/sassendes/bedrock" alt="license"></a>
  <a href="https://scorecard.dev/viewer/?uri=github.com/sassendes/bedrock"><img src="https://api.scorecard.dev/projects/github.com/sassendes/bedrock/badge" alt="openssf scorecard"></a>
  <a href="reports/cis-1.9.pdf"><img src="https://img.shields.io/badge/CIS_1.9-passing-brightgreen" alt="cis 1.9"></a>
</p>

## Why

Most hosting setups put many customers on one cluster and draw the lines with namespaces. Bedrock doesn't. A tenant is a full cluster with its own nodes, storage, databases, observability and policy, so one tenant's problem can never become another's. It targets regulated organisations that need to prove isolation, not promise it.

## The tenant cluster

```mermaid
flowchart TB
    OBS[Observability: Prometheus, Loki, Tempo, Grafana] ~~~ RT[Runtime: Falco, Reloader]
    DATA[Data: CloudNativePG, Garage S3, OpenBao]
    BLK[Block storage: Longhorn]
    ADM[Admission: Pod Security, Kyverno, ValidatingAdmissionPolicy]
    NET[Network: Cilium, default deny, layer 7 rules, WireGuard]
    K8S[RKE2 with CIS profile]
    OBS --> DATA
    RT --> DATA
    DATA --> BLK --> ADM --> NET --> K8S
```

| Layer | Component |
|---|---|
| Kubernetes | RKE2 with the CIS profile, secrets encryption, audit logging |
| Network | Cilium with kube-proxy replacement, layer 7 rules on every HTTP flow, WireGuard encryption between nodes, Hubble |
| Policy | Kyverno, Pod Security Admission, a native ValidatingAdmissionPolicy, registry allowlist |
| Block storage | Longhorn, every volume encrypted with LUKS |
| Object storage | Garage, encrypted at rest |
| Databases | CloudNativePG: a primary with synchronous replication, a live replica and a delayed replica, poolers, WAL and base backups to Garage |
| Secrets | OpenBao: three pods on raft, initialized and unsealed by the bootstrap. Pods get their secrets as files in memory through the Secrets Store CSI driver and the app database password rotates daily |
| Observability | Prometheus, Alertmanager, Loki, Tempo, Alloy, Grafana, OpenCost, blackbox |
| Failover | bedrock's own controller: two observers per node, a database that proves it is alive with a signature, a separate verifier, every action gated by admission. Model checked with TLA+, tested live: 66 s, 0 bytes lost |
| Runtime | Falco, Reloader |
| Event buses (optional) | NATS JetStream, Valkey with Sentinel, Kafka on Strimzi |

## Security posture

- Default deny on every pod in every namespace. Each flow is allowed explicitly and pinned to the exact pods on both sides.
- Layer 7 rules on every plain HTTP flow, written from each client's source code. No pod can silence an alert, delete logs or reach an admin API.
- Nothing reaches the internet. No NodePorts, no LoadBalancers, and cluster DNS has no path outside the cluster.
- Bedrock's own services run under the `restricted` Pod Security level. Only the node agents that need the host run privileged, in their own namespace.
- Least-privilege RBAC: no wildcards, no component reading secrets it does not own, replicas read the primary's backups with a read-only key.
- Postgres superuser login is off and every client connection must use TLS.
- Every Longhorn volume is encrypted with LUKS and Kubernetes Secrets are encrypted in etcd.
- Every image is pinned by digest and every upstream bundle is checksum-verified before it is applied.
- kube-bench (`rke2-cis-1.9`): every check passes except one accepted by design (operators need API tokens). Full report: [reports/cis-1.9.pdf](reports/cis-1.9.pdf)

## Repository layout

```
tenant-workload/bootstrap/       ansible playbooks, RKE2 and Cilium config, audit policy, proof key rotation
tenant-workload/platform/        reloader, namespaces, priority classes
tenant-workload/netpols/         Cilium network policies, one file per namespace plus the cluster-wide default deny
tenant-workload/kyverno/         admission policies and the database CA syncs
tenant-workload/database/        CloudNativePG clusters, poolers, backups, the add-replica templates
tenant-workload/failover/        the failover controller: deployment, admission, network policy, alerts
tenant-workload/storage/         Garage, bucket initialisation, Longhorn settings
tenant-workload/observability/   metrics, logs, traces, dashboards, alerting, falco
tenant-workload/event-buses/     NATS, Valkey, Kafka
tenant-workload/openbao/         OpenBao, three pods on raft
tenant-workload/secrets/         the placeholder Longhorn encryption key
failover-controller/             the failover controller: Go code, Dockerfile, TLA+ models
.github/workflows/               builds the failover controller image
docs/                            the documentation site
reports/                         the CIS benchmark report
Makefile                         deploy, teardown, add-replica, rotate-proof-key
```

## Running it

Requirements: 3 virtual machines minimum (4 vCPU, 12 GB RAM and 200 GB disk each, more RAM with event buses), SSH key access, and a controller with Ansible.

```sh
sudo ./tenant-workload/bootstrap/dependencies/ansible_deps.sh
cp tenant-workload/bootstrap/hosts.example.ini tenant-workload/bootstrap/hosts.ini
# fill in hosts.ini, which git ignores
make deploy
```

`make teardown` wipes RKE2, etcd and every Longhorn volume on all nodes. It asks you to type `destroy` first.

Dashboards are ClusterIP only. Reach them with `kubectl port-forward`.

The full walkthrough is in the [documentation](https://bedrockctl.com/docs/tutorials/first-tenant/).

## Status

The tenant layer runs end to end on a lab cluster. The failover controller is built, model checked and tested live: a whole PostgreSQL cluster killed, cluster-2 promoted in 66 s with 0 bytes lost.

The Longhorn encryption key in `tenant-workload/secrets/` is a placeholder for a public repository. Change it before the first deploy since it cannot be rotated on a running tenant.
