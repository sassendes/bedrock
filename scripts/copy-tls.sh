#!/bin/bash
set -e

kubectl get secret cluster-1-ca -n cluster-1 -o yaml | sed 's/namespace: cluster-1/namespace: cluster-2/' | sed '/resourceVersion:/d' | sed '/uid:/d' | sed '/creationTimestamp:/d' | sed '/ownerReferences:/,/^\s*$/d' | kubectl apply -n cluster-2 -f -

kubectl get secret cluster-1-ca -n cluster-1 -o yaml | sed 's/namespace: cluster-1/namespace: cluster-3/' | sed '/resourceVersion:/d' | sed '/uid:/d' | sed '/creationTimestamp:/d' | sed '/ownerReferences:/,/^\s*$/d' | kubectl apply -n cluster-3 -f -

kubectl get secret cluster-1-replication -n cluster-1 -o yaml | sed 's/namespace: cluster-1/namespace: cluster-2/' | sed '/resourceVersion:/d' | sed '/uid:/d' | sed '/creationTimestamp:/d' | sed '/ownerReferences:/,/^\s*$/d' | kubectl apply -n cluster-2 -f -

kubectl get secret cluster-1-replication -n cluster-1 -o yaml | sed 's/namespace: cluster-1/namespace: cluster-3/' | sed '/resourceVersion:/d' | sed '/uid:/d' | sed '/creationTimestamp:/d' | sed '/ownerReferences:/,/^\s*$/d' | kubectl apply -n cluster-3 -f -
