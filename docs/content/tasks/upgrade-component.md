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
   kubectl apply -f observability/loki/loki.yaml
   kubectl -n application rollout status statefulset/loki
   ```

The same steps work for the PostgreSQL image, `imageName` in the database cluster files. CloudNativePG restarts the replicas first and then switches over the primary, because `primaryUpdateStrategy` is `unsupervised`. A minor PostgreSQL version can be changed this way; a major version needs a new cluster.

## Upgrade a bundled operator

Kyverno, Longhorn, CloudNativePG, the Prometheus operator and Strimzi are installed from their release manifests by `bootstrap/bedrock-bootstrap.yml`. Each download has a URL with the version in it and a `checksum`.

1. Download the new manifest and compute its checksum:

   ```sh
   curl -sL <new release URL> | sha256sum
   ```

2. In the bootstrap task, change the version in the task name and the URL, and replace the `checksum` with `sha256:<new sum>`.

3. Apply the new manifest:

   ```sh
   kubectl apply --server-side --force-conflicts -f <new release URL>
   ```

   Strimzi's manifest has no namespace of its own, so add `-n kafka`. Then wait for the operator's deployments to be ready.

4. For Kyverno, the manifest resets the admission controller to its default replica count. Scale it back to one replica per server node, as the bootstrap does:

   ```sh
   kubectl -n kyverno scale deployment kyverno-admission-controller --replicas=<server nodes>
   ```

The bootstrap task is the record of which version a tenant runs, so a fresh install uses the new version from then on.

## Upgrade RKE2

The RKE2 version is the `rke2_version` variable in the bootstrap. Upgrade the nodes one at a time, servers first, and wait for each to report `Ready` before moving on, so etcd keeps its majority throughout. RKE2's own documentation describes the per-node steps.
