---
title: "Repository layout"
weight: 7
description: "Where each part of a tenant lives in the repository."
---

| Path | Contents |
|---|---|
| `bootstrap/` | Ansible playbooks, the inventory template, the RKE2 audit policy and the Cilium configuration |
| `platform/` | Namespaces, priority classes and the ValidatingAdmissionPolicy, Reloader |
| `kyverno/` | Kyverno policies and the admission controller's disruption budget |
| `netpols/` | Cilium network policies, one file per namespace, plus the cluster-wide default deny |
| `storage/` | Garage, the bucket job, Longhorn settings and storage classes |
| `database/` | The CloudNativePG clusters, poolers, scheduled backups and the certificate sync |
| `observability/` | Prometheus and exporters, Loki and Alloy, Tempo, Grafana and dashboards, alerting, OpenCost, Falco |
| `event-buses/` | NATS, Redis and Kafka |
| `secrets/` | Placeholder secrets, to be replaced by OpenBao |
| `scripts/` | Controller setup |
| `docs/` | This documentation site |

## Conventions

- Every manifest is plain YAML applied by the bootstrap. There is no Helm and no templating.
- Files carry no comments. Explanations live in these docs.
- `bootstrap/hosts.ini` holds placeholders only. Real inventories are kept out of the repository.
