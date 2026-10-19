---
title: "Monitoring stack"
weight: 6
description: "Prometheus, Alertmanager, Loki, Alloy, Tempo, Grafana and OpenCost, and how each is configured."
---

All monitoring components except the node agents run in the `application` namespace under the `restricted` Pod Security level.

## Prometheus

Managed by prometheus-operator v0.93.1, which runs in its own `prometheus-operator` namespace.

| Setting | Value |
|---|---|
| Version | v3.13.1, set explicitly with its digest |
| Replicas | 2, scraping the same targets independently |
| Retention | 15 days or 16 GB, whichever comes first |
| Volume | 20 GiB on `longhorn` |
| Resources | 400 MiB and 100m requested, 1 GiB and 1 CPU limit |
| Disruption budget | Keeps 1 |
| Selectors | All ServiceMonitors, PodMonitors, Probes and rules in every namespace |

Its ClusterRole can read nodes, services, endpoints, endpoint slices and pods, and `/metrics` on the API. It cannot read Secrets.

## Alertmanager

v0.33.1, also run by the operator. Only Prometheus, Loki's ruler and the blackbox exporter can reach it, so no other pod can create a silence. Running it without replicas is enough because the Watchdog alert reveals a dead Alertmanager as soon as delivery to v2 exists.

## Loki

| Setting | Value |
|---|---|
| Version | 3.7.8 |
| Replicas | 3, ring over memberlist |
| Replication factor | Equal to the replica count |
| Storage | Chunks and index in the `loki-data` Garage bucket, schema v13 |
| Retention | 720 hours |
| Deletion API | Turned off, so logs cannot be erased over the API |
| Disruption budget | Keeps 2 |

A write needs a majority of copies, so Loki runs an odd number of replicas spread across nodes and keeps serving as long as a majority is up. Loki's ruler sends `PostgresDropStatement` to Alertmanager when a database logs a `DROP`.

Loki 3.6 moved to version 2 of the AWS SDK, which sends checksums Garage does not accept by default. The `AWS_REQUEST_CHECKSUM_CALCULATION` and `AWS_RESPONSE_CHECKSUM_VALIDATION` variables set to `when_required` fix that.

## Alloy

A DaemonSet in `node-agents`. It collects:

- pod logs, labelled with `namespace`, `pod` and `container`
- the Kubernetes audit log from `/var/lib/rancher/rke2/server/logs/audit.log`, labelled `job="kube-audit"`, `node` and `verb`
- OpenTelemetry traces, forwarded to Tempo

Alloy runs as root to read the root-owned audit log, but drops every Linux capability and cannot escalate. It keeps its read positions on the node, so a restart neither resends nor skips log lines.

## Tempo

Tempo 3.1.0, a single instance, storing traces in the `tempo-data` Garage bucket and keeping them for 14 days.

## Grafana

Grafana 13.2.4, a single instance. Datasources for Prometheus, Loki and Tempo are provisioned from a ConfigMap, and dashboards are loaded by the k8s-sidecar from ConfigMaps in `application` only. The admin login comes from the `grafana-admin` Secret. Update checks, plugin checks and the news feed are turned off.

## OpenCost

OpenCost 1.121.3 estimates cost from resource use with fixed prices per core hour, per GB hour of memory and per GB of storage. Its API listens on 9003 for Prometheus and its interface on 9090.

## Exporters

| Exporter | Version | Runs in |
|---|---|---|
| node-exporter | v1.12.1 | `node-agents`, as nobody with a read-only filesystem |
| kube-state-metrics | v2.20.0 | `application`, with no access to Secrets |
| blackbox exporter | v0.29.0 | `application` |
