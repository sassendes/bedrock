INVENTORY ?= tenant-workload/bootstrap/hosts.ini

FAILOVER_IMAGE ?= ghcr.io/sassendes/bedrock-failover:0.1.0

.PHONY: teardown deploy redeploy check failover-image failover-push
teardown:
	ansible-playbook -i $(INVENTORY) tenant-workload/bootstrap/bedrock-teardown.yml

deploy: failover-push
	ansible-playbook -i $(INVENTORY) tenant-workload/bootstrap/bedrock-bootstrap.yml
redeploy: teardown deploy
check:
	ansible-playbook -i $(INVENTORY) tenant-workload/bootstrap/bedrock-bootstrap.yml --syntax-check
failover-image:
	docker build -t $(FAILOVER_IMAGE) failover-controller
failover-push: failover-image
	gh auth token | docker login ghcr.io -u sassendes --password-stdin
	docker push $(FAILOVER_IMAGE)
