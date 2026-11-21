---
aliases: ["/reference/netpol-labels/"]
title: "Policy labels"
weight: 12
description: "The pod labels the network policies select on. Changing one of these labels cuts a pod off."
---

Cilium policies pick pods by label. If a pod loses one of the labels below, or a new pod is deployed without it, the policies stop recognising it and its traffic is dropped. Before relabelling a bedrock workload, find it here and update the policies in `tenant-workload/netpols/` at the same time.

A peer in another namespace is a namespace label and a pod label together. Namespaces bedrock creates are named by `k8s:io.cilium.k8s.namespace.labels.bedrockctl.com/role`. Namespaces created by RKE2 or an upstream bundle (`kube-system`, `kyverno`, `cnpg-system`) are named by `k8s:io.kubernetes.pod.namespace`.

## Labels selected inside a namespace

These labels pick which pods a rule applies to.

| Namespace | Label | Pods | Policy file |
|---|---|---|---|
| `observability` | `app.kubernetes.io/name: prometheus` | Prometheus | `observability.yaml` |
| `observability` | `app.kubernetes.io/name: alertmanager` | Alertmanager | `observability.yaml` |
| `observability` | `app: grafana` | Grafana | `observability.yaml` |
| `observability` | `app: loki` | Loki | `observability.yaml` |
| `observability` | `app: tempo` | Tempo | `observability.yaml` |
| `observability` | `app: opencost` | OpenCost | `observability.yaml` |
| `observability` | `app: kube-state-metrics` | kube-state-metrics | `observability.yaml` |
| `observability` | `app: blackbox-exporter` | Blackbox exporter | `observability.yaml` |
| `garage` | `app: garage` | Garage nodes | `garage.yaml` |
| `garage` | `app: garage-init` | The bucket setup Job | `garage.yaml` |
| `openbao` | `app: openbao` | OpenBao, for its raft peers | `openbao.yaml` |
| `secrets-store` | `app: csi-secrets-store` | The CSI driver | `secrets-store.yaml` |
| `secrets-store` | `app.kubernetes.io/name: openbao-csi-provider` | The OpenBao CSI provider | `secrets-store.yaml` |
| `kube-system` | `k8s-app: kube-dns` | CoreDNS | `kube-system.yaml` |
| `bedrock-failover` | `app.kubernetes.io/name: bedrock-observer` | Both observer DaemonSets | `tenant-workload/failover/netpol.yaml` |
| `bedrock-failover` | `app.kubernetes.io/name: bedrock-verifier` | The verifier | `tenant-workload/failover/netpol.yaml` |
| `bedrock-failover` | `app.kubernetes.io/name: bedrock-checker` | The checker | `tenant-workload/failover/netpol.yaml` |

The other namespaces apply one policy to every pod in them, so their pods need no particular label.

## Labels other namespaces rely on

These labels are named as peers by policies in other namespaces.

| Namespace | Label | Allowed to reach it, or reached by it |
|---|---|---|
| `kube-system` | `k8s-app: kube-dns` | Every pod, for DNS |
| `observability` | `app.kubernetes.io/name: prometheus` | Scrapes every namespace that exposes metrics |
| `observability` | `app: blackbox-exporter` | Probes Garage, Kyverno, CloudNativePG and the database clusters |
| `observability` | `app: loki` | Reaches Garage; receives logs from Alloy |
| `observability` | `app: tempo` | Reaches Garage; receives traces from Alloy |
| `node-agents` | `app: alloy` | Sends to Loki and Tempo |
| `longhorn-system` | `app: longhorn-manager` | Scraped by Prometheus |
| `kyverno` | `app.kubernetes.io/component: admission-controller` | Probed by the blackbox exporter |
| `cnpg-system` | `app.kubernetes.io/name: cloudnative-pg` | Scraped and probed from `observability` |
| `garage` | `app: garage` | Scraped by Prometheus |
| `secrets-store` | `app.kubernetes.io/name: openbao-csi-provider` | Reaches OpenBao on 8200 |
| `openbao` | `app: openbao` | Reached by the OpenBao CSI provider, reaches the database clusters on 5432 |
| `bedrock-failover` | `app.kubernetes.io/name: bedrock-observer` | Reaches the database instances on 5432 |
| `bedrock-failover` | `app.kubernetes.io/name: bedrock-verifier` | Reaches the database instances on 5432 and 8000 |
| `bedrock-failover` | `app.kubernetes.io/name: bedrock-checker` | Reaches the database instances on 8000 |

The failover controller's pods also carry `bedrockctl.com/observer-slot` (`a` or `b`) on the observers, which admission reads to tell the two observers on a node apart.

## Labels set by operators

Some of these labels are set by an operator, not by the repository. Prometheus and Alertmanager get theirs from the Prometheus operator, the database pods from CloudNativePG and the Kyverno and CloudNativePG controllers from their release manifests. An operator upgrade that changed them would cut those pods off, so check the labels after upgrading one:

```sh
kubectl -n observability get pods --show-labels
```
