---
title: "Policy labels"
weight: 12
description: "The pod labels the network policies select on. Changing one of these labels cuts a pod off."
---

Cilium policies pick pods by label. If a pod loses one of the labels below, or a new pod is deployed without it, the policies stop recognising it and its traffic is dropped. Before relabelling a platform workload, find it here and update the policies in `netpols/` at the same time.

Cilium writes the namespace as the label `k8s:io.kubernetes.pod.namespace`, so a peer is often a namespace and a label together.

## Labels selected inside a namespace

These labels pick which pods a rule applies to.

| Namespace | Label | Pods | Policy file |
|---|---|---|---|
| `application` | `app.kubernetes.io/name: prometheus` | Prometheus | `application.yaml` |
| `application` | `app.kubernetes.io/name: alertmanager` | Alertmanager | `application.yaml` |
| `application` | `app: grafana` | Grafana | `application.yaml` |
| `application` | `app: loki` | Loki | `application.yaml` |
| `application` | `app: tempo` | Tempo | `application.yaml` |
| `application` | `app: opencost` | OpenCost | `application.yaml` |
| `application` | `app: kube-state-metrics` | kube-state-metrics | `application.yaml` |
| `application` | `app: blackbox-exporter` | Blackbox exporter | `application.yaml` |
| `garage` | `app: garage` | Garage nodes | `garage.yaml` |
| `garage` | `app: garage-init` | The bucket setup Job | `garage.yaml` |
| `kube-system` | `k8s-app: kube-dns` | CoreDNS | `kube-system.yaml` |

The other namespaces apply one policy to every pod in them, so their pods need no particular label.

## Labels other namespaces rely on

These labels are named as peers by policies in other namespaces.

| Namespace | Label | Allowed to reach it, or reached by it |
|---|---|---|
| `kube-system` | `k8s-app: kube-dns` | Every pod, for DNS |
| `application` | `app.kubernetes.io/name: prometheus` | Scrapes every namespace that exposes metrics |
| `application` | `app: blackbox-exporter` | Probes Garage, Kyverno, CloudNativePG and the database clusters |
| `application` | `app: loki` | Reaches Garage; receives logs from Alloy |
| `application` | `app: tempo` | Reaches Garage; receives traces from Alloy |
| `node-agents` | `app: alloy` | Sends to Loki and Tempo |
| `longhorn-system` | `app: longhorn-manager` | Scraped by Prometheus |
| `kyverno` | `app.kubernetes.io/component: admission-controller` | Probed by the blackbox exporter |
| `cnpg-system` | `app.kubernetes.io/name: cloudnative-pg` | Scraped and probed from `application` |
| `garage` | `app: garage` | Scraped by Prometheus |

## Labels set by operators

Some of these labels are set by an operator, not by the repository. Prometheus and Alertmanager get theirs from the Prometheus operator, the database pods from CloudNativePG and the Kyverno and CloudNativePG controllers from their release manifests. An operator upgrade that changed them would cut those pods off, so check the labels after upgrading one:

```sh
kubectl -n application get pods --show-labels
```
