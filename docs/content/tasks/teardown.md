---
title: "Tear down a tenant"
weight: 6
description: "Remove RKE2, etcd and every volume from the nodes and return them to a clean state."
---

The teardown removes everything the bootstrap installed. Use it to start over in a lab, or to rehearse a total loss.

{{< note "warning" >}}
This destroys the cluster, etcd and every Longhorn volume on all nodes, including all database data and backups stored in Garage. There is no undo.
{{< /note >}}

## Run it

From the controller:

```sh
make teardown
```

Ansible asks you to type `destroy` before it touches anything. Any other answer cancels the run.

## What it removes

On every node:

- RKE2, through its own uninstall script
- `/var/lib/rancher/rke2` and `/etc/rancher/rke2`, which hold etcd and the cluster configuration
- the `kubectl`, `crictl` and `ctr` links and the login profile
- the CIS kernel settings file
- `/var/lib/longhorn`, every volume replica on the node

On the controller, it deletes `~/.bedrock`, which holds the admin kubeconfig.

## Deploy again

The nodes are ready for a fresh bootstrap straight away:

```sh
make deploy
```

`make redeploy` runs the teardown and the deploy in one go.
