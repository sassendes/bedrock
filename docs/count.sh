#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
tw=tenant-workload
fc=failover-controller
alerts=$(grep -rhE '^\s*- alert:' $tw | wc -l)
netpols=$(grep -rhE '^kind: Cilium(Clusterwide)?NetworkPolicy' $tw | wc -l)
kyverno=$(grep -rhE '^kind: (Validating|Mutating|Generating)Policy' $tw | wc -l)
vaps=$(grep -rhE '^kind: ValidatingAdmissionPolicy$' $tw | wc -l)
images=$(grep -rhoE 'image: *"?[a-z0-9][^ "]+' $tw | grep -v '{{' | sed -E 's/image: *"?//' | sort -u)
total=$(printf '%s\n' "$images" | grep -c .)
pinned=$(printf '%s\n' "$images" | grep -c '@sha256:' || true)
namespaces=$(grep -cE '^kind: Namespace' $tw/platform/namespace.yaml)
golines=$(find $fc -name '*.go' ! -name '*_test.go' -exec cat {} + | wc -l)
testlines=$(find $fc -name '*_test.go' -exec cat {} + | wc -l)
specs=$(ls $fc/model/*.tla | grep -v '/MC' | wc -l)
configs=$(ls $fc/model/*.cfg | wc -l)
cat > docs/data/stats.json <<JSON
[
  {"value": "$alerts", "label": "alert rules", "path": "$tw/observability/alerting"},
  {"value": "$netpols", "label": "network policies on a cluster wide default deny", "path": "$tw/netpols"},
  {"value": "$kyverno", "label": "Kyverno policies", "path": "$tw/kyverno"},
  {"value": "$vaps", "label": "validating admission policies", "path": "$tw"},
  {"value": "$pinned/$total", "label": "images pinned by digest", "path": "$tw"},
  {"value": "$namespaces", "label": "namespaces", "path": "$tw/platform/namespace.yaml"},
  {"value": "$golines", "label": "lines of Go in the failover controller", "path": "$fc"},
  {"value": "$testlines", "label": "lines of tests for it", "path": "$fc"},
  {"value": "$specs", "label": "TLA+ specs checked in $configs model configurations", "path": "$fc/model"}
]
JSON
