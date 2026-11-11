INVENTORY ?= tenant-workload/bootstrap/hosts.ini

FAILOVER_IMAGE ?= ghcr.io/sassendes/bedrock-failover:0.3.5

.PHONY: teardown deploy redeploy check check-allowlist failover-image print-failover-image rotate-proof-key rotate-exchange-cert add-replica
teardown:
	ansible-playbook -i $(INVENTORY) tenant-workload/bootstrap/bedrock-teardown.yml

deploy:
	ansible-playbook -i $(INVENTORY) tenant-workload/bootstrap/bedrock-bootstrap.yml
redeploy: teardown deploy
check: check-allowlist
	ansible-playbook -i tenant-workload/bootstrap/hosts.example.ini tenant-workload/bootstrap/bedrock-bootstrap.yml --syntax-check
check-allowlist:
	@f=tenant-workload/kyverno/admission/registry-allowlist.yaml; n=$$(grep -c "p.matches(r'" $$f); u=$$(grep -o "p.matches(r'[^']*')" $$f | sort -u | wc -l); \
	if [ "$$n" -lt 3 ] || [ "$$u" -ne 1 ]; then echo "the registry allowlists in $$f drifted: $$n patterns, $$u different"; exit 1; fi
failover-image:
	docker build -t $(FAILOVER_IMAGE) failover-controller
print-failover-image:
	@echo $(FAILOVER_IMAGE)
rotate-proof-key:
	ansible-playbook tenant-workload/bootstrap/rotate-proof-key.yml
rotate-exchange-cert:
	ansible-playbook tenant-workload/bootstrap/rotate-exchange-cert.yml
add-replica:
	ansible-playbook tenant-workload/bootstrap/add-live-replica.yml -e replica=$(REPLICA)
