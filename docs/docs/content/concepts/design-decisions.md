---
title: "Design decisions"
weight: 9
description: "The main choices behind bedrock and the alternatives that were considered."
---

Most parts of bedrock replaced something else that was tried or considered first. This page lists the main decisions and the reasons for them.

## Platform

| Chosen | Instead of | Reason |
|---|---|---|
| RKE2 with the CIS profile | k3s | Audit logging, Pod Security, secrets encryption and CIS compliance out of the box |
| One cluster per tenant | Namespaces on a shared cluster | Isolation as a property of the design, not of configuration |
| Hand-written, pinned manifests | Helm | What is in the repository is exactly what runs |
| Bootstrap once, then reviewed `kubectl diff` and apply | Argo CD or another GitOps controller | Write access to the repository would equal cluster admin, and a git host becomes attack surface |

## Storage

| Chosen | Instead of | Reason |
|---|---|---|
| Garage | MinIO | MinIO's enterprise edition is paid and licence gated, and the community edition is no longer maintained |
| Garage | Ceph RGW | Ceph is heavy in memory and operations for a job that only needs S3 |
| Three Garage pods, each on a local volume | One Garage pod on a replicated volume | No single point of failure, and no data replicated twice |
| Longhorn | Ceph RBD and CephFS | Longhorn already provides block and shared volumes and runs on any node |

## Network

| Chosen | Instead of | Reason |
|---|---|---|
| `CiliumNetworkPolicy` | Kubernetes `NetworkPolicy` | Cluster-wide deny, naming the API server as an entity, FQDN rules and layer 7 rules |
| Cilium kube-proxy replacement | kube-proxy alongside Cilium | With both handling services, traffic from the node was rewritten and lost its identity, so host rules never matched |
| `ClusterIP` and port-forward | Dashboards on NodePorts | A datacenter has no trusted LAN |
| Cluster DNS with no egress | Letting CoreDNS reach an upstream resolver | A path out for DNS is a path out for data |
| Per-component policy in `application` | Allow-all inside the namespace | One compromised pod could otherwise silence alerts and delete logs |

## Policy and security

| Chosen | Instead of | Reason |
|---|---|---|
| Kyverno on application namespaces only | Kyverno on every namespace | Storage and database recovery must never depend on a policy engine |
| Priority classes set in each manifest | A Kyverno mutation | Priority is scheduling, not security, so it should not sit behind a webhook |
| A small privileged `node-agents` namespace | Host agents inside `application` | Two agents needing the host made every monitoring pod eligible to run as root |
| OpenBao, one per tenant | Sealed secrets, SOPS or a shared vault | An audit trail and dynamic credentials, and no shared component holding every tenant's keys |
| Falco to stdout, collected by Alloy | falcosidekick | One component fewer |

## Operations

| Chosen | Instead of | Reason |
|---|---|---|
| A single Alertmanager per tenant | A highly available Alertmanager | If it dies, the Watchdog stops reaching v2, which raises the alarm. High availability arrives with v2 delivery. |
| Local etcd snapshots | Snapshots to external S3 | The bootstrap must work with no outside services, and etcd can be rebuilt from the repository |
| Single replicas for Tempo, Grafana and OpenCost | High availability everywhere | They lose nothing on restart |
