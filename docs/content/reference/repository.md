---
title: "Repository layout"
weight: 7
description: "Where each part of a tenant lives in the repository."
---

| Path | Contents |
|---|---|
| `tenant-workload/bootstrap/` | Ansible playbooks, the inventory example, the RKE2 configuration template, the RKE2 audit policy and the Cilium configuration |
| `tenant-workload/bootstrap/tasks/` | Task files shared by the playbooks, such as finding the primary from the role labels |
| `tenant-workload/platform/` | Namespaces, priority classes and the ValidatingAdmissionPolicy, Reloader |
| `tenant-workload/kyverno/` | Kyverno policies and the admission controller's disruption budget |
| `tenant-workload/netpols/` | Cilium network policies, one file per namespace, plus the cluster-wide default deny |
| `tenant-workload/storage/` | Garage, the bucket job, Longhorn settings and storage classes |
| `tenant-workload/database/` | The CloudNativePG clusters, poolers, scheduled backups and the certificate sync |
| `tenant-workload/database/replica/` | Templates `make add-replica` renders for a new live replica |
| `tenant-workload/observability/` | Prometheus and exporters, Loki and Alloy, Tempo, Grafana and dashboards, alerting, OpenCost, Falco |
| `tenant-workload/event-buses/` | NATS, Redis and Kafka |
| `tenant-workload/failover/` | The failover controller's deployment, admission policies, network policy and alerts |
| `tenant-workload/openbao/` | OpenBao: three pods on raft |
| `tenant-workload/secrets/` | Placeholder secrets |
| `tenant-workload/bootstrap/dependencies/` | Controller setup |
| `failover-controller/` | The failover controller's Go code, its Dockerfile and its TLA+ models |
| `.github/workflows/` | Builds and publishes the failover controller's image |
| `docs/` | This documentation site |
| `reports/` | The CIS 1.9 self-assessment |
| `Makefile` | The `make` commands listed in the [bootstrap reference]({{< relref "bootstrap" >}}) |

## Conventions

- Every manifest is plain YAML applied by the bootstrap. There is no Helm. The only templates are Ansible's: `tenant-workload/database/replica/*.j2`, rendered by `make add-replica`, and `tenant-workload/bootstrap/rke2-config.yaml.j2`, the RKE2 configuration for every server.
- YAML files carry no comments and Go code only a few. Explanations live in these docs.
- `tenant-workload/bootstrap/hosts.example.ini` holds placeholders only. The real `hosts.ini` is listed in `.gitignore`, so it stays out of the repository.
