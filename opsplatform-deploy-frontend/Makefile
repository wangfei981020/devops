VERSION ?= v106
HARBOR  := localhost:8070/opsplatform
IMAGE   := $(HARBOR)/opsplatform-deploy-frontend:$(VERSION)

.PHONY: build push dev

build:
	docker build -t $(IMAGE) .

push: build
	docker push $(IMAGE)

dev:
	npm run dev
