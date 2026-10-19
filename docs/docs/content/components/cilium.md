---
title: "Cilium"
weight: 1
description: "The network plugin: pod routing, service load balancing, network policy, encryption and flow visibility."
---

Cilium ships with RKE2 and is configured through a `HelmChartConfig` that the bootstrap writes to the RKE2 manifests directory before the first server starts.

## Configuration

| Setting | Value | Why |
|---|---|---|
| `kubeProxyReplacement` | `true` | One component handles all service traffic, so packet identities stay correct |
| `k8sServiceHost` / `k8sServicePort` | `localhost` / `6443` | Without kube-proxy, Cilium needs a fixed API address at startup. Every node is a server, so the local API server is always there. |
| `encryption.type` | `wireguard` | Pod traffic between nodes is encrypted |
| `hubble.enabled`, `relay`, `ui` | `true` | Flow logs, a cluster-wide relay and the web interface |

RKE2 itself runs with `disable-kube-proxy: true` and `cni: cilium`.

## Why kube-proxy is replaced

With kube-proxy and Cilium both handling services, traffic arriving through a node port was rewritten by kube-proxy before Cilium saw it. Cilium then classified it as coming from outside the cluster, so rules that allowed the node never matched. With Cilium alone on the path, a packet keeps its real identity from the first hop.

The `localhost` API address works because every node runs the API server. A cluster with worker-only nodes would need a different address there.

## Network policy

Policies are written as `CiliumNetworkPolicy` and `CiliumClusterwideNetworkPolicy`, not the plain Kubernetes `NetworkPolicy`. The Cilium types add what bedrock needs:

- a cluster-wide policy that selects every pod at once
- **entities** such as `kube-apiserver`, `host` and `remote-node`, so the API server and the nodes can be named without knowing their IP addresses
- room for FQDN egress and layer 7 rules later

Each namespace has one file in `netpols/`. The rules and the reasoning are in [networking]({{< relref "/concepts/networking" >}}) and every allowed flow is listed in the [network flows reference]({{< relref "/reference/network-flows" >}}).

## Entities used

| Entity | Means | Used for |
|---|---|---|
| `host` | The node the pod runs on | Kubelet health checks, scraping the node |
| `remote-node` | Any other node in the cluster | Scraping other nodes, Longhorn traffic |
| `kube-apiserver` | The Kubernetes API server | Operators and controllers that watch the cluster |
| `cluster` | Anything inside the cluster | DNS queries to CoreDNS |

## Hubble

Hubble records every flow Cilium handles, including the ones it drops. It is the first place to look when a connection fails after a policy change. See [find a dropped connection]({{< relref "/tasks/dropped-connections" >}}).
