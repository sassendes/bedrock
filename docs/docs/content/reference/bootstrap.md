---
title: "Bootstrap"
weight: 6
description: "The files, variables and stages of the bootstrap and teardown playbooks."
---

## Commands

| Command | Runs |
|---|---|
| `make deploy` | `ansible-playbook -i bootstrap/hosts.ini bootstrap/bedrock-bootstrap.yml` |
| `make teardown` | `ansible-playbook -i bootstrap/hosts.ini bootstrap/bedrock-teardown.yml` |
| `make redeploy` | Teardown, then deploy |
| `make check` | Syntax check of the bootstrap playbook |

Set `INVENTORY=path/to/hosts.ini` to use another inventory file.

## Inventory groups

| Group | Members |
|---|---|
| `server_init` | The first node, which creates the cluster |
| `server_join` | The other server nodes, joined one at a time |
| `rke2_servers` | Both groups together |

## Variables

| Variable | Default | Effect |
|---|---|---|
| `rke2_version` | `v1.36.4+rke2r1` | RKE2 release to install |
| `nats_enabled` | `false` | Deploy NATS JetStream |
| `redis_enabled` | `false` | Deploy Redis with Sentinel |
| `kafka_enabled` | `false` | Deploy Strimzi and Kafka |
| `kubeconfig` | `~/.bedrock/kubeconfig` | Where the admin kubeconfig is written on the controller |

## Stages

| Stage | What runs |
|---|---|
| 1 | Node packages (open-iscsi, nfs-common, cryptsetup, lvm2), the etcd user, CIS kernel settings, the `dm_crypt` module |
| 2 | Audit policy, RKE2 configuration, Cilium configuration, RKE2 install from a checksummed release |
| 3 | The first server, then the others one at a time |
| 4 | `kubectl`, `crictl` and `ctr` links and the login profile on every node |
| 5 | Namespaces, priority classes and the ValidatingAdmissionPolicy |
| 6 | Kyverno, scaled to one admission replica per node, then its policies |
| 7 | Placeholder secrets |
| 8 | Longhorn, with its RBAC rewritten without wildcards |
| 9 | CloudNativePG operator and prometheus-operator |
| 10 | Longhorn encryption key and storage class, Garage, the bucket job |
| 11 | cluster-1, its first backup, the certificate sync, then cluster-2 and cluster-3 |
| 12 | Poolers, scheduled backups and disruption budgets |
| 13 | Event buses, if enabled |
| 14 | Loki, Alloy, Tempo, Prometheus and exporters, alerting, Grafana, OpenCost, Falco, Reloader, dashboards |
| 15 | Token automount turned off on every default service account |
| 16 | Network policies, then a check that every namespace running pods has one |

## Files the bootstrap writes on the nodes

| Path | Contents |
|---|---|
| `/etc/rancher/rke2/config.yaml` | RKE2 configuration |
| `/etc/rancher/rke2/bedrock-audit.yaml` | API audit policy |
| `/var/lib/rancher/rke2/server/manifests/rke2-cilium-config.yaml` | Cilium settings |
| `/etc/sysctl.d/60-rke2-cis.conf` | CIS kernel settings and inotify limits |
| `/etc/modules-load.d/dm_crypt.conf` | Loads `dm_crypt` at boot for volume encryption |
| `/etc/profile.d/rke2.sh` | Sets `KUBECONFIG` and `CRI_CONFIG_FILE` at login |
| `/var/lib/rancher/rke2/server/logs/audit.log` | The API audit log, shipped to Loki |
