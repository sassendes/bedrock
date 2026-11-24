IMAGE ?= ghcr.io/sassendes/bedrock-failover:0.4

.PHONY: build vet test image
build:
	go build ./...
vet:
	go vet ./...
test:
	go test ./...
image:
	docker build -t $(IMAGE) .
