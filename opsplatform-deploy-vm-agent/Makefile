VERSION ?= v5
HARBOR  := localhost:8070/opsplatform
IMAGE   := $(HARBOR)/opsplatform-deploy-vm-agent:$(VERSION)

.PHONY: build test docker push run

# 本地编译（dev / smoke test）
build:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
	  -trimpath -ldflags="-s -w" \
	  -o ./bin/deploy-vm-agent ./cmd/agent

test:
	go test ./...

docker:
	docker build -t $(IMAGE) .

push: docker
	docker push $(IMAGE)

# 本地起一个测试 agent（HTTP，需要先 cp installer/config.example.yaml 改 token）
run: build
	./bin/deploy-vm-agent --config ./bin/config.yaml
