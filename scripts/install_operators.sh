#!/bin/bash
set -euo pipefail

CNPG_VERSION="1.27.0"
PROM_OP_VERSION="0.93.1"

echo ">> installing cnpg operator v${CNPG_VERSION}"
kubectl apply --server-side -f \
  "https://raw.githubusercontent.com/cloudnative-pg/cloudnative-pg/release-1.27/releases/cnpg-${CNPG_VERSION}.yaml"

echo ">> waiting for cnpg operator to be ready"
kubectl wait --for=condition=Available deployment/cnpg-controller-manager \
  -n cnpg-system --timeout=180s

echo ">> installing prometheus-operator v${PROM_OP_VERSION}"
kubectl apply --server-side -f \
  "https://github.com/prometheus-operator/prometheus-operator/releases/download/v${PROM_OP_VERSION}/bundle.yaml"

echo ">> waiting for prometheus-operator to be ready"
kubectl wait --for=condition=Available deployment/prometheus-operator \
  -n default --timeout=180s

echo ">> operators installed"
kubectl get crd | grep -E 'postgresql.cnpg.io|monitoring.coreos.com'
