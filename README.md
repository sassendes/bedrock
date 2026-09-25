## Bedrock tenant workload

### Policy (Kyverno)
Kyverno enforces three rules in the `application` namespace:

- **Pinned image tags.** `latest` and untagged images are rejected. An image can't change under you between deploys.
- **Non-root containers.** Pods run as non-root. If a container is compromised, the attacker doesn't get root.
- **Resource limits required.** Every container must set CPU and memory limits. A leaking pod gets OOMKilled (exit 137) on its own instead of starving its neighbours.

### Database (CloudNativePG)
Postgres runs on the CloudNativePG operator.

- **Pooler.** PgBouncer in front of the primary. Apps share a small set of connections instead of opening hundreds.
- **Backups.** Barman ships every WAL file and a base backup every 6 hours to S3 (Ceph). This enables point-in-time recovery.

### Roadmap
- mTLS on every hop (service mesh)
