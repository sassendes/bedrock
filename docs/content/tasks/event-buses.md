---
title: "Turn on an event bus"
weight: 3
description: "Deploy NATS, Redis or Kafka on a tenant that needs one."
---

The three event buses are off by default, because most tenants do not need them and each one uses CPU, memory and a volume per pod. Their namespaces and network policies are always present, so turning one on needs no policy change.

| Bus | Variable | Use it for |
|---|---|---|
| NATS JetStream | `nats_enabled` | Durable, replayable streams between services. The first choice when a bus is needed. |
| Redis with Sentinel | `redis_enabled` | Sessions, rate limits and caches |
| Kafka (Strimzi) | `kafka_enabled` | Data pipelines, change data capture and long-term event retention |

Redis refuses writes while none of its replicas is in sync with the master (`min-replicas-to-write 1`, `min-replicas-max-lag 10`). A master cut off from its replicas therefore cannot accept writes that a Sentinel failover would later throw away. The cost is that Redis stops taking writes if both replicas are down.

## Turn one on

Run the bootstrap with the variable set to `true`. For NATS:

```sh
ansible-playbook -i tenant-workload/bootstrap/hosts.ini tenant-workload/bootstrap/bedrock-bootstrap.yml -e nats_enabled=true
```

Several at once:

```sh
ansible-playbook -i tenant-workload/bootstrap/hosts.ini tenant-workload/bootstrap/bedrock-bootstrap.yml \
  -e nats_enabled=true -e redis_enabled=true
```

The bootstrap leaves everything that is already installed as it is and waits for every pod of the bus to be ready.

{{< note "warning" >}}
The bootstrap refuses to run once a failover has happened, because it would turn the new primary back into a replica. After a failover, turn NATS or Redis on by applying its manifest directly, since its secret and network policy are already in place:

```sh
kubectl apply -f tenant-workload/event-buses/nats.yaml
kubectl apply -f tenant-workload/event-buses/redis.yaml
```

Kafka also needs the Strimzi operator, which the bootstrap downloads, checks and patches first. Follow the Strimzi and Kafka tasks in `tenant-workload/bootstrap/bedrock-bootstrap.yml` by hand.
{{< /note >}}

## Check it

```sh
kubectl -n nats get pods
```

One pod per node, all `Running`. For Redis use the `redis` namespace and for Kafka the `kafka` namespace.

## Let an application use it

No platform component talks to the buses, so their network policies only admit traffic from their own namespace. When an application needs a bus, add an ingress rule to the bus's policy in `tenant-workload/netpols/event-buses.yaml` that names the application's pods, and an egress rule in the application's own policy that names the bus. Both sides have to allow the flow.

Credentials for the buses are placeholders in `tenant-workload/secrets/`. Change them before an application stores anything real.

## Make it permanent

The `-e` flag applies to one run. To keep a bus on for every future deploy, set its variable to `true` in the `vars` block of the second play in `tenant-workload/bootstrap/bedrock-bootstrap.yml`.
