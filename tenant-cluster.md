---
title: "Inside a tenant cluster"
weight: 3
description: "The layers of a tenant cluster, from the nodes to the services, and how they depend on each other."
---

A tenant cluster is three nodes running a fixed stack. Each layer builds on the one below it, and the order matters both when the cluster is installed and when something fails.

{{< diagram "tenant-layers" "A tenant cluster from the machine upwards. Applications run in their own namespaces on top of the platform services." >}}

## Nodes and Kubernetes

The nodes are plain Linux machines (Debian 12 in the reference setup). bedrock does not assume a hypervisor or a cloud. It needs SSH access with keys and passwordless sudo, nothing more.

Kubernetes is RKE2 with its CIS profile turned on, secrets encrypted at rest and an audit policy that records who did what at the API. All three nodes are servers, so etcd keeps a majority when one node is lost.

## Network

Cilium is the network plugin. It replaces kube-proxy, so one component handles all service traffic, and it encrypts pod traffic between nodes with WireGuard. Every pod in every namespace starts with all traffic denied, and each flow the platform needs is allowed by name. Hubble records flows so that a blocked connection is visible. See [networking]({{< relref "networking" >}}).

## Admission

Before a pod runs, three checks look at it:

- Pod Security Admission enforces the `restricted` level on almost every namespace, so root, privileged and host-path pods are refused
- Kyverno checks images, tags and resource limits
- a native ValidatingAdmissionPolicy keeps the high priority classes for the components that need them

See the [security model]({{< relref "security-model" >}}) for how these fit together.

## Storage

Longhorn provides block volumes for everything that needs a disk. Garage runs on top of Longhorn and provides S3 for backups, logs and traces. Garage keeps three copies of every object across the three nodes, and its disks are encrypted. See [data and storage]({{< relref "data-and-storage" >}}).

## Databases

CloudNativePG runs three PostgreSQL clusters: a primary, a live replica and a replica that stays five minutes behind. Applications connect through a connection pooler and a stable service name, so they do not need to know which cluster is active.

## Observability and runtime security

Prometheus collects metrics, Loki stores logs and audit events, Tempo stores traces and Grafana shows all three. Prometheus evaluates 39 alert rules and Alertmanager routes the ones that fire. Falco watches system calls on every node and reports suspicious behaviour. See [observability]({{< relref "observability" >}}).

## Namespaces

Components are grouped by what they need, not by product. The rule is that a namespace is only as privileged as its most privileged pod, so the few components that need the host live apart from everything else.

| Namespace | Holds | Pod Security |
|---|---|---|
| `application` | Prometheus, Alertmanager, Grafana, Loki, Tempo, OpenCost, exporters | restricted |
| `node-agents` | Alloy and node-exporter, which read from the host | privileged |
| `cluster-1`, `cluster-2`, `cluster-3` | The three PostgreSQL clusters and their poolers | restricted |
| `garage` | S3 object storage | restricted |
| `falco` | Runtime detection | privileged |
| `longhorn-system` | Block storage | privileged |
| `reloader` | Restarts workloads when their configuration changes | restricted |

The full list is in the [namespaces reference]({{< relref "/reference/namespaces" >}}).
