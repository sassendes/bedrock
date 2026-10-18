---
title: "Security model"
weight: 5
description: "The layers of defence in a tenant, what each one stops and the exceptions that are accepted on purpose."
---

bedrock assumes that any single control can fail. Security comes from several independent layers, so that getting past one still leaves the others in place.

{{< diagram "security-layers" "From the outside in: what has to fail before an attacker reaches tenant data." >}}

## Supply chain

Every upstream bundle (Longhorn, Kyverno, CloudNativePG, prometheus-operator, Strimzi) and the RKE2 release itself is downloaded with a sha256 checksum and refused if it does not match. Every container image in the repository is pinned by digest, so the content cannot change behind a tag. A Kyverno policy allows images only from a short list of publishers.

## Admission

- **Pod Security Admission** enforces the `restricted` level on almost every namespace. Kubernetes itself refuses root, privileged and host-path pods there, without depending on Kyverno.
- **Kyverno** requires pinned tags, CPU and memory limits and allowed publishers on application pods, and fills in non-root defaults.
- **A ValidatingAdmissionPolicy** runs inside the API server and keeps the high priority classes for storage, databases and system components. It works even when Kyverno is down.

## Network

Default deny everywhere, flows pinned on both sides and no exposed ports. See [networking]({{< relref "networking" >}}).

## Identity and access

No component has wildcard RBAC. Default service accounts do not mount API tokens. kube-state-metrics, Grafana's sidecar and Reloader have no access to Secrets. The database replicas read the primary's backups with a read-only key, so a compromised replica cannot delete them. PostgreSQL's superuser cannot log in over the network, and every database client must use TLS.

## Runtime

Falco watches system calls on every node: a shell opening in a database pod, an unexpected binary, an attempt to break out of a container. Events go to Loki and to Prometheus, and critical ones raise an alert.

## Data at rest

Kubernetes Secrets are encrypted in etcd. Garage's volumes, which hold every backup, log and trace, are encrypted with LUKS by Longhorn.

## Threat model

The table shows what an attacker who gains a shell in one pod can reach.

| Foothold | Can reach | Cannot reach |
|---|---|---|
| An application pod in its own namespace | Only the services its own policy names, such as its database pooler | Other tenants, the monitoring stack, Garage, other databases |
| Grafana | Prometheus, Loki, Tempo and the Kubernetes API | Alertmanager, Garage, databases |
| A pod in `node-agents` | Loki on 3100, Tempo on 4317 and the Kubernetes API | Each other, Alertmanager, Garage, databases |
| A database replica | Its own cluster, Garage on 3900, the primary on 5432, the Kubernetes API | The primary's backups for writing, other tenants |

A breach in one tenant does not reach another, because tenants share no cluster, node or network.

## Accepted exceptions

Some findings are left in place on purpose. Each has a reason.

| Finding | Why it stays |
|---|---|
| CIS 5.1.6: some pods mount API tokens | They are operators and controllers that need the Kubernetes API to work. The exception is documented, not hidden by editing the benchmark. |
| Kyverno can write Secrets cluster-wide | It copies two replication certificates into the replica namespaces and keeps them current when they rotate. The plan is to have cert-manager issue them directly. |
| Pod traffic on the same node is not encrypted | It never leaves the machine. |
| Placeholder secrets in the repository | They exist so a fresh deploy works. OpenBao replaces them before anything is exposed. |

## Compliance

The cluster is checked with kube-bench against the RKE2 CIS 1.9 benchmark. The current result is 85 checks passed and 1 failed, the failure being 5.1.6 above. Check 4.3.1 is skipped because it tests kube-proxy, which Cilium replaces.
