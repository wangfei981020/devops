VERSION ?= v32
HARBOR  := localhost:8070/opsplatform
IMAGE   := $(HARBOR)/opsplatform-deploy-backend:$(VERSION)

.PHONY: build push test run

build:
	docker build -t $(IMAGE) .

push: build
	docker push $(IMAGE)

test:
	go test ./... -v

run:
	go build -o deploy-backend.exe . && ./deploy-backend.exe
