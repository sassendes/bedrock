---
title: "Falco"
weight: 7
description: "Runtime threat detection on every node, and where its events go."
---

Falco 0.44.1 watches system calls on every node and matches them against rules: a shell opening inside a container, a write to a sensitive path, an unexpected binary, an attempt to reach the host. It is hand written in `observability/falco/falco.yaml`, without Helm and without falcosidekick.

## How it runs

| Property | Value |
|---|---|
| Kind | DaemonSet in the privileged `falco` namespace |
| Driver | `modern_ebpf`, built into the image |
| Container runtime | Reads RKE2's containerd socket at `/run/k3s/containerd` |
| Resources | 100m and 256 MiB requested, 1 CPU and 1 GiB limit |
| Priority class | `bedrock-data` |
| Tolerations | All, so it runs on every node |

The modern eBPF driver needs a kernel with BTF, which Debian 12's 6.1 kernel has, so nothing is compiled on the nodes.

Falco runs at the data priority class because its events are evidence. On a full node, a lower class would be among the first pods evicted, leaving that node without detection exactly when it matters.

## Where events go

Falco writes events as JSON to standard output at `notice` priority and above. Alloy ships them to Loki like any other pod log. Falco also exposes Prometheus metrics on port 8765, which Prometheus scrapes.

| Alert | Fires when |
|---|---|
| `FalcoCriticalEvent` | A rule at emergency, alert or critical priority matched in the last 5 minutes |
| `FalcoWarningEvent` | A rule at error or warning priority matched in the last 15 minutes |
| `FalcoDown` | Falco cannot be scraped on a node for 5 minutes |

To read the events themselves, see [check Falco events]({{< relref "/tasks/falco-events" >}}).

## What it does not do

Falco watches system calls, not packets. Network flows are Hubble's job. Falco also only reports today: it does not stop a process or isolate a pod.
