---
title: "Rotate the exchange certificate"
weight: 14
description: "Give the failover observers a new mutual TLS certificate before the old one expires."
---

The observers ask each other about the primary over mutual TLS on port 8443. Every observer uses the same leaf certificate, signed by a small certificate authority that the bootstrap created on the controller in `~/.bedrock/failover/exchange`. The leaf is valid for 825 days and the authority for 10 years.

When the leaf expires, every exchange fails and observers can only abstain when their own check fails. `BedrockExchangeCertExpiring` warns 30 days ahead and `BedrockExchangeCertExpiryCritical` 7 days ahead.

## Rotate it

```sh
make rotate-exchange-cert
```

| Step | What it does |
|---|---|
| 1 | Checks that the authority is still there and outlives the new leaf |
| 2 | Signs a new leaf with the same authority, so old and new observers trust each other during the switch |
| 3 | Writes it into the `bedrock-observer-exchange` Secret |
| 4 | Restarts `bedrock-observer-a`, waits for it on every node, then does the same for `bedrock-observer-b` |

A DaemonSet restarts one node at a time. While a node's observer restarts, that node abstains, which stays within the one abstaining node the decider tolerates.

## Check it

```sh
kubectl -n bedrock-failover get pods -l app.kubernetes.io/name=bedrock-observer
```

All observers are `Running`. In Grafana, this metric shows the new expiry on every one of them:

```promql
bedrock_observer_exchange_cert_expiry_timestamp_seconds
```

## When the authority itself expires

The playbook refuses to sign a leaf that would outlive the authority. Replacing the authority means every observer has to trust the new one before any presents a leaf from it. Until a playbook does that, run the bootstrap's two exchange tasks by hand on a fresh `~/.bedrock/failover/exchange` and restart both observer DaemonSets.
