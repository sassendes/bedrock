---
title: "Repository layout"
weight: 7
description: "Where each part of a tenant lives in the repository."
---

| Path | Contents |
|---|---|
| `tenant-workload/bootstrap/` | Ansible playbooks, the inventory template, the RKE2 audit policy and the Cilium configuration |
| `tenant-workload/platform/` | Namespaces, priority classes and the ValidatingAdmissionPolicy, Reloader |
| `tenant-workload/kyverno/` | Kyverno policies and the admission controller's disruption budget |
| `tenant-workload/netpols/` | Cilium network policies, one file per namespace, plus the cluster-wide default deny |
| `tenant-workload/storage/` | Garage, the bucket job, Longhorn settings and storage classes |
| `tenant-workload/database/` | The CloudNativePG clusters, poolers, scheduled backups and the certificate sync |
| `tenant-workload/observability/` | Prometheus and exporters, Loki and Alloy, Tempo, Grafana and dashboards, alerting, OpenCost, Falco |
| `tenant-workload/event-buses/` | NATS, Redis and Kafka |
| `tenant-workload/failover/` | The failover controller's deployment, admission policies, network policy and alerts |
| `tenant-workload/openbao/` | OpenBao |
| `tenant-workload/secrets/` | Placeholder secrets, to be replaced by OpenBao |
| `tenant-workload/bootstrap/dependencies/` | Controller setup |
| `failover-controller/` | The failover controller's Go code, its Dockerfile and its TLA+ models |
| `.github/workflows/` | Builds and publishes the failover controller's image |
| `docs/` | This documentation site |

## Conventions

- Every manifest is plain YAML applied by the bootstrap. There is no Helm and no templating.
- YAML files carry no comments and Go code only a few. Explanations live in these docs.
- `tenant-workload/bootstrap/hosts.ini` holds placeholders only. Real inventories are kept out of the repository.
