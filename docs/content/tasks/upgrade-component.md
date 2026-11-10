---
title: "Upgrade a component"
weight: 12
description: "Move a platform component to a new version, keeping its image pinned by digest."
---

Every image in the repository is pinned by tag and digest, and every bundle the bootstrap downloads is checked against a sha256 checksum. An upgrade changes both the version and its pin, so the cluster only ever runs exactly what was reviewed.

## Before you begin

- Read the component's release notes for the versions between yours and the target. Some components, Longhorn among them, only support upgrading one minor version at a time.
- Make sure the platform is healthy. Follow [verify a tenant]({{< relref "verify" >}}) first.

## Upgrade an image

1. Look up the digest of the new tag. The digest of the multi-arch index is the one to pin:

   ```sh
   docker buildx imagetools inspect grafana/loki:<new tag>
   ```

   Copy the `Digest:` line at the top.

2. Change the image in the component's file to the new `tag@digest`:

   ```yaml
   image: grafana/loki:<new tag>@sha256:<digest>
   ```

3. Check that the registry is allowed. Images in `application`, `node-agents` and `reloader` must come from a publisher in the [registry allowlist]({{< relref "image-publishers" >}}). A new version from the same publisher is already allowed.

4. Apply the file and wait for the rollout:

   ```sh
   kubectl apply -f tenant-workload/observability/loki/loki.yaml
   kubectl -n application rollout status statefulset/loki
   ```

The same steps work for the PostgreSQL image, `imageName` in the database cluster files. CloudNativePG restarts the replicas first and then switches over the primary, because `primaryUpdateStrategy` is `unsupervised`. A minor PostgreSQL version can be changed this way; a major version needs a new cluster.

## Upgrade a bundled operator

Kyverno, Longhorn, CloudNativePG, the Prometheus operator and Strimzi are installed from their release manifests by `tenant-workload/bootstrap/bedrock-bootstrap.yml`. The bootstrap does more than apply them: it checks each download against a checksum, removes Longhorn's wildcard RBAC and its cluster-wide Secret read, moves the Prometheus operator out of `default`, and gives Kyverno's admission controller one replica per node, the top priority class and anti-affinity. Never apply a release URL directly, because that undoes all of it.

1. Download the new manifest and compute its checksum:

   ```sh
   curl -sL <new release URL> | sha256sum
   ```

2. In the bootstrap task, change the version in the task name and the URL, and replace the `checksum` with `sha256:<new sum>`.

3. Run only that operator's tasks. They are tagged `kyverno`, `longhorn`, `cnpg-operator`, `prometheus-operator` and `strimzi`:

   ```sh
   ansible-playbook -i tenant-workload/bootstrap/hosts.ini tenant-workload/bootstrap/bedrock-bootstrap.yml --tags longhorn
   ```

   Strimzi only installs with Kafka turned on, so add `-e kafka_enabled=true` for it. A tagged run skips the database tasks, so it also works after a failover, when a full bootstrap refuses to run.

The bootstrap task is the record of which version a tenant runs, so a fresh install uses the new version from then on.

## Upgrade RKE2

The RKE2 version is the `rke2_version` variable in the bootstrap. Upgrade the nodes one at a time, servers first, and wait for each to report `Ready` before moving on, so etcd keeps its majority throughout. RKE2's own documentation describes the per-node steps.
