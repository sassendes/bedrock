---
title: "Observability"
weight: 7
description: "How metrics, logs, traces and alerts are collected inside a tenant."
---

Every tenant carries its own observability stack, so a tenant can detect its own problems without any control plane above it.

{{< diagram "observability" "Logs and traces flow through Alloy into Loki and Tempo; metrics are scraped by Prometheus; Grafana reads all three." >}}

## Metrics

Prometheus runs as several replicas that scrape the same targets independently, so losing one loses nothing. It keeps 15 days of data and caps itself at 16 GB so its volume never fills.

It scrapes the nodes, the kubelet and its container metrics, kube-state-metrics, the database clusters, Falco, OpenCost, Kyverno, Longhorn, Garage, Loki, Tempo, the CloudNativePG operator and etcd. A blackbox exporter probes the HTTP health endpoints of the services, the database endpoints over TCP and the TLS certificates of the API server and the admission webhooks.

## Logs

Alloy runs on every node and ships pod logs and the Kubernetes audit log to Loki. Loki runs several replicas and writes every log line to each of them, so it survives the loss of a node while a majority is up. Its chunks and index are stored in Garage and kept for 30 days. Loki's delete API is turned off, so nobody can erase logs through it.

## Traces

Applications send OpenTelemetry traces to Alloy, which forwards them to Tempo. Tempo stores them in Garage.

## Alerts

A set of alert rules ships with every tenant. They cover nodes, pods and jobs, the database clusters and their backups, storage, certificates, etcd, Kyverno, Falco and the monitoring pipeline itself. A Watchdog alert fires all the time on purpose: if it ever stops arriving upstream, the alerting path is broken. The full list is in the [alerts reference]({{< relref "/reference/alerts" >}}).

{{< note "planned" >}}
Alertmanager will forward alerts to the v2 provisioner, which groups them across tenants and delivers them. Until v2 exists, alerts are visible in Prometheus and Grafana only.
{{< /note >}}

## Dashboards and cost

Grafana shows metrics, logs and traces side by side, with dashboards for the nodes and the databases. OpenCost estimates what each workload costs from its resource use.

## Configuration changes

Alloy, Loki, Tempo and the blackbox exporter read their configuration only at startup. Reloader watches their ConfigMaps and restarts them when the configuration changes. Prometheus and Alertmanager are reloaded by their operator.
