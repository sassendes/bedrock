---
title: "Check Falco events"
weight: 10
description: "Read the runtime security events Falco has raised, in Loki or straight from the pods."
---

Falco writes each event as one line of JSON. They end up in Loki with the rest of the pod logs, and the counts per rule end up in Prometheus.

## In Grafana

Forward Grafana, open **Explore** and pick **Loki**:

```sh
kubectl -n application port-forward svc/grafana 3000:3000
```

All Falco events:

```text
{namespace="falco"} | json | rule != ""
```

Only critical ones:

```text
{namespace="falco"} | json | priority="Critical"
```

Events from one namespace's pods:

```text
{namespace="falco"} | json | output_fields_k8s_ns_name="cluster-1"
```

Each event includes the rule name, the priority, the command line, the container and the Kubernetes pod and namespace.

## From the pods

To read the latest events on every node without Grafana:

```sh
kubectl -n falco logs -l app=falco --tail=50 --prefix
```

## Which rules fire most

In Prometheus, at `http://localhost:9090` after forwarding `svc/prometheus-operated`:

```text
sort_desc(sum by (rule_name) (increase(falcosecurity_falco_rules_matches_total[24h])))
```

This shows which rules matched in the last day and how often. A rule that fires constantly on a known component, such as Longhorn or CloudNativePG doing normal work, is a candidate for an exception in Falco's rules.
