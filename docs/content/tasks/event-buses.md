---
title: "Turn on an event bus"
weight: 3
description: "Deploy NATS, Redis or Kafka on a tenant that needs one."
---

The three event buses are off by default, because most tenants do not need them and each one uses CPU, memory and three volumes. Their namespaces and network policies are always present, so turning one on needs no policy change.

| Bus | Variable | Use it for |
|---|---|---|
| NATS JetStream | `nats_enabled` | Durable, replayable streams between services. The first choice when a bus is needed. |
| Redis with Sentinel | `redis_enabled` | Sessions, rate limits and caches |
| Kafka (Strimzi) | `kafka_enabled` | Data pipelines, change data capture and long-term event retention |

## Turn one on

Run the bootstrap with the variable set to `true`. For NATS:

```sh
ansible-playbook -i bootstrap/hosts.ini bootstrap/bedrock-bootstrap.yml -e nats_enabled=true
```

Several at once:

```sh
ansible-playbook -i bootstrap/hosts.ini bootstrap/bedrock-bootstrap.yml \
  -e nats_enabled=true -e redis_enabled=true
```

The bootstrap leaves everything that is already installed as it is and waits for the bus to have three ready pods.

## Check it

```sh
kubectl -n nats get pods
```

Three pods, one per node, all `Running`. For Redis use the `redis` namespace and for Kafka the `kafka` namespace.

## Let an application use it

No platform component talks to the buses, so their network policies only admit traffic from their own namespace. When an application needs a bus, add an ingress rule to the bus's policy in `netpols/event-buses.yaml` that names the application's pods, and an egress rule in the application's own policy that names the bus. Both sides have to allow the flow.

Credentials for the buses are placeholders in `secrets/` until OpenBao replaces them. Change them before an application stores anything real.

## Make it permanent

The `-e` flag applies to one run. To keep a bus on for every future deploy, set its variable to `true` in the `vars` block of the second play in `bootstrap/bedrock-bootstrap.yml`.
