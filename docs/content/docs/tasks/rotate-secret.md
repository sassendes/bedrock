---
aliases: ["/tasks/rotate-secret/"]
title: "Rotate a secret"
weight: 11
description: "Change a password or token that a bedrock component reads, and get the component to use it."
---

Grafana, NATS, Valkey, Garage and the database application user get their secrets from OpenBao. The rest live in `tenant-workload/secrets/` as plain Kubernetes Secrets. Both kinds are read when a process starts, so changing one does nothing until the pods that use it restart, and Reloader deliberately does not watch Secrets. The steps differ per component, because some also keep the old value in their own data.

The bootstrap makes every OpenBao value once and keeps a copy on the controller in `~/.bedrock/`. Change the copy too, so a fresh install uses the new value.

## Which components read which secret

| Secret | Where | Read by | Kept in the component's own data |
|---|---|---|---|
| `apps/grafana/admin` | OpenBao | Grafana | Yes, in Grafana's database |
| `apps/garage/rpc`, `apps/garage/admin` | OpenBao | Garage, the bucket jobs (admin token only) | No |
| `apps/nats/server`, `apps/nats/app` | OpenBao | NATS, applications | No |
| `apps/valkey/auth` | OpenBao | Valkey and Sentinel, applications | No |
| `database/static-creds/bedrock-app` | OpenBao, rotated every 24 hours | Applications, as the `bedrock_app` role | Yes, in PostgreSQL, set by OpenBao |
| `cluster-1-app` | `cluster-1`, made by CloudNativePG | CloudNativePG, for the `appuser` role that owns the database | Yes, in PostgreSQL |
| `longhorn-crypto` | `longhorn-system` | Longhorn volume encryption | Yes, in every encrypted volume |
| `apps/failover/observer` | OpenBao | The failover observers and verifier, as the `bedrock_observer` role | Yes, in PostgreSQL |

## Write to OpenBao by hand

The commands below need the root token from `~/.bedrock/openbao-init.json`. Open a shell in OpenBao that reads it without showing it:

```sh
kubectl -n openbao exec -it openbao-0 -- sh -c 'read -rs BAO_TOKEN; export BAO_TOKEN; exec sh'
```

Paste the root token, then run the `bao` commands in that shell.

## Grafana admin password

Grafana only reads its password from OpenBao the first time it starts with an empty database. After that the password lives in Grafana's database, so it has to be changed there and in OpenBao together. One command does both and updates the copy on the controller:

```sh
make rotate-grafana-password
```

It makes a new random password, sets it with `grafana cli admin reset-admin-password` inside the running pod, then writes it to `apps/grafana/admin` and `~/.bedrock/grafana-admin`. Grafana does not restart. Read the new password from `~/.bedrock/grafana-admin`.

## Garage admin token

```sh
read -rs t; printf %s "$t" | bao kv put -mount=apps garage/admin token=-
```

Then restart Garage and put the same value in `~/.bedrock/garage-admin-token`:

```sh
kubectl -n garage rollout restart statefulset/garage
kubectl -n garage rollout status statefulset/garage
```

Leave the RPC secret alone on a running tenant. Every Garage node must share it, so nodes restarted with a new value cannot talk to the ones still running the old one.

## Valkey password

```sh
read -rs p; printf %s "$p" | bao kv put -mount=apps valkey/auth password=-
```

Then restart the StatefulSet and put the same value in `~/.bedrock/valkey-password`:

```sh
kubectl -n valkey rollout restart statefulset/valkey
kubectl -n valkey rollout status statefulset/valkey
```

## NATS passwords

NATS reads one file, `auth.conf`, which holds the three passwords and the route addresses built from the route password. Rewrite it whole with the new values:

```sh
read -rs route; read -rs sys; read -rs app
{
  printf 'ROUTE_PASSWORD: "%s"\nSYS_PASSWORD: "%s"\nAPP_PASSWORD: "%s"\n' "$route" "$sys" "$app"
  for i in 0 1 2; do printf 'ROUTE_%s: "nats://route:%s@nats-%s.nats-headless.nats.svc.cluster.local:6222"\n' "$i" "$route" "$i"; done
} | bao kv put -mount=apps nats/server auth.conf=-
printf %s "$app" | bao kv put -mount=apps nats/app user=app password=-
```

Then restart the StatefulSet and put the same values in `~/.bedrock/nats-route-password`, `nats-sys-password` and `nats-app-password`:

```sh
kubectl -n nats rollout restart statefulset/nats
kubectl -n nats rollout status statefulset/nats
```

For Valkey and NATS the pods use the same password to talk to each other, so while the restart rolls through, pods on the new value cannot reach pods on the old one. The bus is degraded until the last pod has restarted. Clients must be given the new password at the same time.

## Database application user

Applications log in as `bedrock_app`. OpenBao owns its password and changes it every 24 hours on its own, so there is nothing to rotate by hand. To rotate it now:

```sh
bao write -force database/rotate-role/bedrock-app
```

The new password reaches the pods' files within two minutes. Connections that are already open keep working, because PostgreSQL only checks a password at login.

`appuser` owns the `app` database. CloudNativePG made its password at `initdb` and keeps it in the `cluster-1-app` Secret. Applications should not use it.

OpenBao's own login, the `openbao` role, was rotated by OpenBao right after the bootstrap gave it to it. Nobody knows that password. To rotate it again:

```sh
bao write -force database/rotate-root/cluster-1
```

## Failover observer password

The password lives in OpenBao at `apps/failover/observer` and never changes on its own. The bootstrap sets the `bedrock_observer` role from it on every run. CloudNativePG does not manage this role. To change it, change OpenBao first and postgres right after.

1. In the OpenBao shell:

   ```sh
   bao kv put -mount=apps failover/observer password='<new password>'
   ```

2. On the current primary:

   ```sh
   kubectl exec -it -n cluster-1 "$(kubectl get cluster cluster-1 -n cluster-1 -o jsonpath='{.status.currentPrimary}')" -c postgres -- psql -d postgres -c "ALTER ROLE bedrock_observer PASSWORD '<new password>'"
   ```

The observers and the verifier read the file again on every connection and the Secrets Store driver refreshes it within two minutes. Until then postgres refuses their login. A refused login proves postgres answered, so an observer abstains and never votes down. The delayed replica keeps the old password until it replays the change.

## Longhorn encryption key

Do not rotate `longhorn-crypto` on a running tenant. Every encrypted volume is unlocked with this key, so changing it leaves existing volumes unable to attach. The bootstrap makes it once, keeps it in `~/.bedrock/longhorn-crypto-key` and reuses the value already in the cluster on every later run.
