INVENTORY ?= bootstrap/hosts.ini

.PHONY: teardown deploy redeploy check
teardown:
	ansible-playbook -i $(INVENTORY) bootstrap/bedrock-teardown.yml

deploy:
	ansible-playbook -i $(INVENTORY) bootstrap/bedrock-bootstrap.yml
redeploy: teardown deploy
check:
	ansible-playbook -i $(INVENTORY) bootstrap/bedrock-bootstrap.yml --syntax-check
