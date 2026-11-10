---
title: "Rotate a secret"
weight: 11
description: "Change a password or token that a platform component reads, and get the component to use it."
---

The platform's credentials live in `tenant-workload/secrets/` as plain Kubernetes Secrets. Every component reads them as environment variables, which a process only reads when it starts. Changing a Secret therefore does nothing until the pods that use it restart, and Reloader deliberately does not watch Secrets. The steps differ per component, because some also keep the old value in their own data.

{{< note >}}
The values in `tenant-workload/secrets/` are the source of truth, so change them there and apply the file rather than editing the Secret in the cluster.
{{< /note >}}

## Which components read which Secret

| Secret | Namespace | Read by | Kept in the component's own data |
|---|---|---|---|
| `grafana-admin` | `application` | Grafana | Yes, in its database |
| `garage-secrets` | `garage` | Garage | No |
| `nats-auth` | `nats` | NATS | No |
| `redis-auth` | `redis` | Redis and Sentinel | No |
| `user-secret-1` | `cluster-1` | CloudNativePG, for the `appuser` role | Yes, in PostgreSQL |
| `longhorn-crypto` | `longhorn-system` | Longhorn volume encryption | Yes, in every encrypted volume |
| `bedrock-observer` | `cluster-1`, copied into `bedrock-failover` by Kyverno | CloudNativePG for the `bedrock_observer` role, the failover observers and verifier | Yes, in PostgreSQL |

## Grafana admin password

Grafana only reads the admin password from its environment the first time it starts with an empty database. After that the password lives in its database, so set it there, then update the Secret to match:

```sh
kubectl -n application exec deploy/grafana -c grafana -- grafana cli admin reset-admin-password '<new password>'
```

Put the same value in `tenant-workload/secrets/grafana-admin.yaml` and apply it, so a fresh install uses it too:

```sh
kubectl apply -f tenant-workload/secrets/grafana-admin.yaml
```

## Garage admin token

Change `admin_token` in `tenant-workload/secrets/garage-secrets.yaml`, apply it and restart Garage:

```sh
kubectl apply -f tenant-workload/secrets/garage-secrets.yaml
kubectl -n garage rollout restart statefulset/garage
kubectl -n garage rollout status statefulset/garage
```

Leave `rpc_secret` alone on a running tenant. Every Garage node must share it, so nodes restarted with a new value cannot talk to the ones still running the old one.

## NATS and Redis passwords

Change the values in `tenant-workload/secrets/nats-auth.yaml` or `tenant-workload/secrets/redis-auth.yaml`, apply the file and restart the StatefulSet:

```sh
kubectl apply -f tenant-workload/secrets/redis-auth.yaml
kubectl -n redis rollout restart statefulset/redis
kubectl -n redis rollout status statefulset/redis
```

The pods use the same password to talk to each other, so while the restart rolls through, pods on the new value cannot reach pods on the old one. The bus is degraded until the last pod has restarted. Clients must be given the new password at the same time.

## Database application user

The `appuser` password is a role in PostgreSQL. Set it on the primary, then store it in the Secret:

```sh
kubectl -n cluster-1 exec cluster-1-1 -c postgres -- psql -U postgres -c "ALTER ROLE appuser PASSWORD '<new password>'"
kubectl apply -f tenant-workload/secrets/user-secret-1.yaml
```

`cluster-1-1` is an example; run it against whichever instance is the current primary, shown by `kubectl -n cluster-1 get cluster cluster-1`. The replica clusters receive the change through replication.

## Failover observer password

CloudNativePG sets the `bedrock_observer` role's password from this Secret, and Kyverno copies the Secret into `bedrock-failover`. The observers and the verifier read the mounted file again on every connection, so no restart is needed: Kubernetes refreshes a mounted Secret within about a minute.

1. Change `password` in `tenant-workload/secrets/bedrock-observer.yaml` and apply it.
2. Wait until the copy in `bedrock-failover` has the new value:

   ```sh
   kubectl -n bedrock-failover get secret bedrock-observer -o jsonpath='{.data.password}' | base64 -d
   ```

3. Watch `bedrock_observer_ballot` in Grafana. For the minute between CloudNativePG changing the role and the mounted file refreshing, observers can vote down. The verifier needs three witnesses as well as the votes, so this never fails over a live database, but `BedrockFailoverBlind` can fire briefly.

The role is managed by cluster-1's spec. After a failover, add the same `managed.roles` entry to the new primary's cluster before rotating.

## Longhorn encryption key

Do not rotate `longhorn-crypto` on a running tenant. Every encrypted volume is unlocked with this key, so changing it leaves existing volumes unable to attach. Choose its value before the first deploy.
