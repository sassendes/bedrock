---
title: "Reach the dashboards"
weight: 2
description: "Open Grafana, Prometheus, Alertmanager, OpenCost, Hubble and Longhorn from the controller."
---

Every dashboard in a tenant is a `ClusterIP` service and none is exposed outside the cluster. You reach them with `kubectl port-forward`, which opens a tunnel through the API server for as long as the command runs.

## Before you begin

`kubectl` pointed at the tenant from the controller:

```sh
export KUBECONFIG=~/.bedrock/kubeconfig
```

## Forward a port

Run the command for the dashboard you want and open the address in a browser. Stop the forward with `Ctrl+C`.

| Dashboard | Command | Address |
|---|---|---|
| Grafana | `kubectl -n application port-forward svc/grafana 3000:3000` | `http://localhost:3000` |
| Prometheus | `kubectl -n application port-forward svc/prometheus-operated 9090:9090` | `http://localhost:9090` |
| Alertmanager | `kubectl -n application port-forward svc/alertmanager-operated 9093:9093` | `http://localhost:9093` |
| OpenCost | `kubectl -n application port-forward svc/opencost-ui 9091:9090` | `http://localhost:9091` |
| Hubble | `kubectl -n kube-system port-forward svc/hubble-ui 12000:80` | `http://localhost:12000` |
| Longhorn | `kubectl -n longhorn-system port-forward svc/longhorn-frontend 8080:80` | `http://localhost:8080` |

Grafana's login is in `secrets/grafana-admin.yaml`. Change it before the cluster holds anything real.

{{< note "warning" >}}
The Longhorn and Hubble interfaces have no login of their own, and the Longhorn interface can delete volumes. Only open them when you need them, and close the forward afterwards.
{{< /note >}}

## Run several at once

Each forward holds the terminal. To run them in the background and stop them together:

```sh
kubectl -n application port-forward svc/grafana 3000:3000 &
kubectl -n application port-forward svc/prometheus-operated 9090:9090 &
wait
```

`Ctrl+C` stops both.
