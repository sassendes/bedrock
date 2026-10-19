---
title: "Allow a new image publisher"
weight: 5
description: "Add a container image source to the registry allowlist and pin the image by digest."
---

The registry allowlist only admits images from known publishers. When you add a component whose images come from somewhere new, you have to add that publisher first, or its pods are refused.

## Before you begin

You need the full image reference you want to run, for example `quay.io/example/tool:v1.2.3`.

## Find the image digest

Every image in bedrock is pinned by digest. Get the digest of the multi-architecture index for your tag:

```sh
docker buildx imagetools inspect quay.io/example/tool:v1.2.3
```

Copy the `Digest:` line and write the image as `quay.io/example/tool:v1.2.3@sha256:<digest>` in your manifest.

## Add the publisher

Open `kyverno/registry-allowlist.yaml`. It contains two policies with the same pattern, one for application namespaces and one for data namespaces. The pattern has two groups:

- single repositories, such as `quay\\.io/kiwigrid/k8s-sidecar`, for when you trust one image
- whole organisations, such as `quay\\.io/(prometheus|prometheus-operator|strimzi)/`, for when you trust everything a publisher ships

Add your entry to the right group in **both** policies. Prefer a single repository unless you really trust the whole organisation.

Images from Docker Hub without a registry name, like `nats:2.14`, are matched as `docker.io/nats`. Official images from Docker Hub do not include `library/` after normalisation.

## Test before you apply

Write a test pod using the new image and check it with the Kyverno CLI:

```sh
kyverno apply kyverno/registry-allowlist.yaml --resource test-pod.yaml
```

Also test a pod with an image that should still be refused, such as `nginx:1.27`, and confirm it fails. A policy that passes everything looks the same as a working one until you test the negative case.

## Apply it

```sh
kubectl apply -f kyverno/registry-allowlist.yaml
```

The change affects new pods only. Running pods are not evicted.
