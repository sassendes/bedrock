#!/bin/bash
set -e
apt update
apt install -y ansible sshpass python3-pip python3-bcrypt
pip3 install kubernetes --break-system-packages
ansible-galaxy collection install kubernetes.core
echo ">> deps done"
