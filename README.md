# Bedrock

An app-agnostic, self-hosted k3s platform for high-availability stateful workloads.

## Stack
* **Cluster:** 3-node k3s
* **Database:** CloudNativePG (PostgreSQL 17)
* **Storage:** Longhorn (Replicated block storage)
* **Backups:** MinIO (S3-compatible WAL archiving)
* **Observability:** LGTM (Loki, Grafana, Prometheus)

## Architecture

```text
Ingress -> [NetPols] -> Microservices
                              |
                              v
    MinIO <--- CNPG <--- Longhorn
      ^
      |
    LGAM Stack
