---
title: "Read the audit log"
weight: 9
description: "Find who did what at the Kubernetes API, using the audit log in Loki."
---

The API server writes an audit event for every request it handles, at metadata level, so request bodies and secret values are never logged. Alloy ships the log from each node to Loki.

## Open Loki in Grafana

```sh
kubectl -n application port-forward svc/grafana 3000:3000
```

Open `http://localhost:3000`, go to **Explore** and choose the **Loki** datasource.

## Labels

Audit events carry three labels:

| Label | Value |
|---|---|
| `job` | `kube-audit` |
| `node` | The server node that handled the request |
| `verb` | The API verb: `get`, `list`, `create`, `update`, `patch`, `delete` and so on |

## Example queries

Everything deleted in the time range:

```text
{job="kube-audit", verb="delete"}
```

Every access to Secrets, with the user and the object:

```text
{job="kube-audit"} | json | objectRef_resource="secrets" | line_format "{{.user_username}} {{.verb}} {{.objectRef_namespace}}/{{.objectRef_name}}"
```

Everything one user did:

```text
{job="kube-audit"} | json | user_username="kubernetes-admin"
```

Requests that were refused:

```text
{job="kube-audit"} | json | responseStatus_code >= 400
```

## What is recorded

The policy in `bootstrap/audit-policy.yaml` records requests at metadata level. Health checks, leases and events are left out as noise. Watches are left out too, except watches on Secrets, ServiceAccounts and RBAC objects, which are always recorded. Logs are kept for 30 days.
