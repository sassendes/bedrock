---
title: "Networking"
weight: 4
description: "How traffic moves inside a tenant cluster and how network policy decides what is allowed."
---

Inside a tenant, Cilium handles every packet between pods. It does three jobs: it routes pod traffic, it load balances Kubernetes services in place of kube-proxy, and it enforces network policy.

## Default deny

A cluster-wide policy selects every pod in every namespace. Once a pod is selected by any policy, Cilium drops all traffic to and from it that no policy allows. The cluster-wide policy allows only two things: health checks from the node and DNS lookups to the cluster DNS server.

Each namespace then has its own `CiliumNetworkPolicy` that adds the flows its components need. Policies only add allows, so a namespace without its own policy is closed, not open. The bootstrap checks this at the end of every deploy and fails if any namespace running pods has no policy.

{{< diagram "netpol-model" "Two layers of policy. The cluster-wide policy closes everything; namespace policies open named flows." >}}

## Flows are pinned on both sides

A flow between two components is allowed in two places: the sender's egress rule names the receiver, and the receiver's ingress rule names the sender. Both sides use labels on the exact pods, not whole namespaces. For example, Loki writes chunks to Garage on port 3900:

- the Loki policy in `application` allows egress to pods in `garage` on 3900
- the Garage policy allows ingress on 3900 only from the Loki and Tempo pods and the database clusters

A mistake on one side is not enough to open a path. Both sides have to agree.

{{< diagram "flows" "The main flows inside a tenant. Every arrow is allowed by name on both ends; anything without an arrow is dropped." >}}

## Per component inside `application`

The `application` namespace holds the whole monitoring stack. Allowing all traffic inside it would let any one of those pods reach the others, so each component has its own rules instead. Only Prometheus, Loki's ruler and the blackbox exporter can reach Alertmanager, which means no other pod can create a silence and hide an alert. Only Alloy, Grafana, Prometheus and the blackbox exporter can reach Loki.

Some namespaces do allow traffic between their own pods: `kube-system`, `longhorn-system` and the database clusters. Their internal ports are chosen by their operators and change between versions, and only those operators create pods there.

## Encryption between nodes

Pod traffic that crosses from one node to another goes through a WireGuard tunnel that Cilium manages. Traffic between two pods on the same node never leaves the machine and is not encrypted.

## DNS stays inside

The cluster DNS server answers cluster names and forwards nothing outside the cluster. A pod cannot use DNS to send data out or receive commands, because no query ever leaves the cluster.

## No exposed ports

Dashboards and APIs are `ClusterIP` services. Nothing listens on a NodePort or a LoadBalancer. Until the per-tenant edge exists, operators reach dashboards with `kubectl port-forward`, as described in [reach the dashboards]({{< relref "/tasks/dashboards" >}}).

The full list of allowed connections is in the [network flows reference]({{< relref "/reference/network-flows" >}}).
