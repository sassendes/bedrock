---
title: "Restart a workload when its config changes"
weight: 4
description: "Use Reloader to roll a Deployment, StatefulSet or DaemonSet when a ConfigMap it reads is updated."
---

Many programs read their configuration file once at startup. Updating the ConfigMap changes the file on disk, but the running process keeps the old settings until it restarts. Reloader closes that gap by rolling the workload whenever the ConfigMap changes.

## Before you begin

Reloader watches the `application` and `node-agents` namespaces, and only ConfigMaps. It ignores Secrets on purpose until OpenBao manages them.

## Add the annotation

Add an annotation to the workload's own `metadata`, not to its pod template, naming the ConfigMap to watch:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: blackbox-exporter
  namespace: application
  annotations:
    configmap.reloader.stakater.com/reload: "blackbox-exporter"
```

To watch more than one ConfigMap, separate the names with commas.

Apply the workload as usual:

```sh
kubectl apply -f observability/prometheus/blackbox-exporter.yaml
```

## Test it

Change a value in the ConfigMap, then watch the pods:

```sh
kubectl -n application edit configmap blackbox-exporter
kubectl -n application get pods -l app=blackbox-exporter -w
```

A new pod starts within a few seconds and the old one is removed. Changing only the ConfigMap's metadata, such as a label, does not trigger a restart, because Reloader compares the data.

## Watch a new namespace

Reloader only has permissions in the namespaces it watches. To add one, add the namespace to the `--namespaces` argument in `platform/reloader.yaml` and add a Role and RoleBinding for it in the same file, copied from the existing ones.

## Workloads already annotated

| Workload | Namespace | ConfigMap |
|---|---|---|
| Alloy | `node-agents` | `alloy-config` |
| Loki | `application` | `loki-config` |
| Tempo | `application` | `tempo-config` |
| Blackbox exporter | `application` | `blackbox-exporter` |

Prometheus and Alertmanager are not annotated, because their operator already reloads them.
