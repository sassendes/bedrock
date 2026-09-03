#!/bin/bash
set -euo pipefail

TARGET="${1:-}"
TARGET_NS="${2:-}"
SOURCE="${3:-}"
SOURCE_NS="${4:-}"

if [ -z "$TARGET" ] || [ -z "$TARGET_NS" ] || [ -z "$SOURCE" ] || [ -z "$SOURCE_NS" ]; then
  echo "usage: $0 <target-cluster> <target-ns> <dead-source-cluster> <dead-source-ns>"
  exit 1
fi

if ! kubectl get cluster "$TARGET" -n "$TARGET_NS" >/dev/null 2>&1; then
  echo "target cluster $TARGET not found in ns $TARGET_NS"
  exit 1
fi

IS_REPLICA=$(kubectl get cluster "$TARGET" -n "$TARGET_NS" -o jsonpath='{.spec.replica.enabled}' 2>/dev/null || echo "")
if [ "$IS_REPLICA" != "true" ]; then
  echo "target $TARGET is not a replica cluster, aborting"
  exit 1
fi

kubectl get cluster "$SOURCE" -n "$SOURCE_NS" 2>/dev/null || true
kubectl get pods -n "$SOURCE_NS" 2>/dev/null || true

read -r -p "confirmed $SOURCE is truly dead, not just unreachable? [type: yes] " C1
if [ "$C1" != "yes" ]; then
  echo "aborting"
  exit 1
fi

read -r -p "type the target cluster name to promote ($TARGET): " C2
if [ "$C2" != "$TARGET" ]; then
  echo "name mismatch, aborting"
  exit 1
fi

if kubectl get cluster "$SOURCE" -n "$SOURCE_NS" >/dev/null 2>&1; then
  kubectl patch cluster "$SOURCE" -n "$SOURCE_NS" --type merge -p '{"spec":{"instances":0}}' || true
fi

sleep 15

kubectl patch cluster "$TARGET" -n "$TARGET_NS" --type merge -p '{"spec":{"replica":{"enabled":false}}}'

sleep 5
kubectl get cluster "$TARGET" -n "$TARGET_NS"
