#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."   # repo root, script lives in scripts/

TEMPLATE="bootstrap/hosts.ini.template"
INVENTORY="bootstrap/hosts.ini"

read -rp "SSH user (same on all 3 nodes): " USER
read -rp "SSH private key path (same on all 3): " KEY
read -rp "Node 1 (init) IP:  " IP1
read -rp "Node 2 (join) IP:  " IP2
read -rp "Node 3 (join) IP:  " IP3

cp "$TEMPLATE" "$INVENTORY"

sed -i \
  -e "s|__NODE_1_HOST__|bedrock-1|g" \
  -e "s|__NODE_2_HOST__|bedrock-2|g" \
  -e "s|__NODE_3_HOST__|bedrock-3|g" \
  -e "s|__NODE_1_IP__|$IP1|g" \
  -e "s|__NODE_2_IP__|$IP2|g" \
  -e "s|__NODE_3_IP__|$IP3|g" \
  -e "s|__NODE_1_USER__|$USER|g" \
  -e "s|__NODE_2_USER__|$USER|g" \
  -e "s|__NODE_3_USER__|$USER|g" \
  -e "s|__NODE_1_KEY__|$KEY|g" \
  -e "s|__NODE_2_KEY__|$KEY|g" \
  -e "s|__NODE_3_KEY__|$KEY|g" \
  "$INVENTORY"

echo "wrote $INVENTORY:"
cat "$INVENTORY"
echo

./scripts/ansible_deps.sh

ansible-playbook -i "$INVENTORY" bootstrap/bedrock-bootstrap.yml
