---
title: "Verify a deployment"
weight: 1
description: "Check that a tenant is running, that its network policies are in place and that it passes the CIS benchmark."
---

Run these checks after every deploy. They take a few minutes and confirm the cluster is both healthy and locked down.

## Before you begin

You need `kubectl` pointed at the tenant, either on the controller with `export KUBECONFIG=~/.bedrock/kubeconfig` or on any node, where the login profile sets it for you.

## Check the pods

```sh
kubectl get pods -A
```

Every pod should be `Running`. A handful of `Completed` jobs in `kube-system` are the RKE2 installers and are normal. To list only the pods that are not healthy:

```sh
kubectl get pods -A --field-selector=status.phase!=Running,status.phase!=Succeeded
```

The output should be empty.

## Check the network policies

```sh
kubectl get cnp,ccnp -A
```

Confirm three things:

- `cluster-default-deny` exists as a cluster-wide policy
- every namespace that runs pods has its own policy
- every policy shows `VALID` as `True`

The bootstrap already fails if a namespace has no policy, so a gap here usually means a namespace was added by hand afterwards.

## Check the databases

```sh
kubectl get clusters.postgresql.cnpg.io -A
```

All three clusters should report `Cluster in healthy state`. To confirm synchronous replication on cluster-1, ask its primary:

```sh
kubectl -n cluster-1 exec cluster-1-1 -- psql -U postgres -c "show synchronous_standby_names;"
```

The result should name the other instance, for example `ANY 1 ("cluster-1-2")`. If `cluster-1-1` is not the current primary, run the command on `cluster-1-2` instead.

## Check storage encryption

```sh
kubectl -n longhorn-system get volumes.longhorn.io \
  -o custom-columns=NAME:.metadata.name,ENCRYPTED:.spec.encrypted
```

The three Garage volumes show `true`. Other volumes use the default storage class and show `false` or nothing.

## Run the CIS benchmark

On one of the nodes:

```sh
curl -sSLO https://github.com/aquasecurity/kube-bench/releases/download/v0.16.0/kube-bench_0.16.0_linux_amd64.tar.gz
tar xzf kube-bench_0.16.0_linux_amd64.tar.gz
./kube-bench run --config-dir ./cfg --benchmark rke2-cis-1.9 --skip 4.3.1 | tail -20
```

Expect 85 checks passed and 1 failed (5.1.6). Any other failure is a regression.

## Check the alerts

Forward Prometheus and open its alerts page:

```sh
kubectl -n application port-forward svc/prometheus-operated 9090:9090
```

At `http://localhost:9090/alerts`, only `Watchdog` should be firing.
