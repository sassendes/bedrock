---
title: "Find a dropped connection"
weight: 8
description: "Use Hubble to see which network policy blocked a flow and from where."
---

When two components cannot talk after a change, the cause is almost always a network policy. Cilium records every flow it drops, and Hubble lets you read them.

## Watch drops as they happen

The Cilium agent on every node includes the Hubble command line tool. Ask the relay for dropped flows across the whole cluster:

```sh
kubectl -n kube-system exec ds/cilium -- hubble observe --verdict DROPPED --follow
```

Each line shows the source pod, the destination, the port and the reason, for example:

```text
application/grafana-57ff584b78-26hpw:41234 -> garage/garage-0:3900 policy-verdict:none INGRESS DENIED (TCP Flags: SYN)
```

`INGRESS DENIED` means the receiver's policy does not allow the sender. `EGRESS DENIED` means the sender's own policy does not allow the destination.

## Narrow it down

Filter by namespace or pod to cut the noise:

```sh
kubectl -n kube-system exec ds/cilium -- hubble observe --verdict DROPPED --namespace garage --last 50
kubectl -n kube-system exec ds/cilium -- hubble observe --verdict DROPPED --pod application/loki-0 --last 50
```

## Use the interface

To browse flows as a graph, forward the Hubble interface and open it in a browser:

```sh
kubectl -n kube-system port-forward svc/hubble-ui 12000:80
```

Pick a namespace at `http://localhost:12000`. Dropped flows are drawn in red.

## Fix the policy

Every flow must be allowed on both sides. Open the two files in `netpols/`, one for the sender's namespace and one for the receiver's, and add:

- an egress rule in the sender's policy naming the receiver's pods and port
- an ingress rule in the receiver's policy naming the sender's pods and port

Apply both and watch the drops stop:

```sh
kubectl apply -f netpols/
```

The labels each policy matches on are listed in the [policy labels reference]({{< relref "/reference/netpol-labels" >}}).
