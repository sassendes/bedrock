INVENTORY ?= tenant-workload/bootstrap/hosts.ini

FAILOVER_IMAGE ?= ghcr.io/sassendes/bedrock-failover:0.1.0

.PHONY: teardown deploy redeploy check failover-image print-failover-image rotate-proof-key
teardown:
	ansible-playbook -i $(INVENTORY) tenant-workload/bootstrap/bedrock-teardown.yml

deploy:
	ansible-playbook -i $(INVENTORY) tenant-workload/bootstrap/bedrock-bootstrap.yml
redeploy: teardown deploy
check:
	ansible-playbook -i $(INVENTORY) tenant-workload/bootstrap/bedrock-bootstrap.yml --syntax-check
failover-image:
	docker build -t $(FAILOVER_IMAGE) failover-controller
print-failover-image:
	@echo $(FAILOVER_IMAGE)
rotate-proof-key:
	ansible-playbook tenant-workload/bootstrap/rotate-proof-key.yml
