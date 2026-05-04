VERSION ?= v110
HARBOR  := localhost:8070/opsplatform
IMAGE   := $(HARBOR)/opsplatform-deploy-backend:$(VERSION)

.PHONY: build push test test-race run

build:
	docker build -t $(IMAGE) .

push: build
	docker push $(IMAGE)

test:
	go test ./... -v

# race detector 需要 Linux + CGO，Windows 本机无 gcc 就用 docker 跑
# 改动 services/concurrent.go 或其它并发代码后务必跑一次
test-race:
	MSYS_NO_PATHCONV=1 docker run --rm -v "$(CURDIR):/app" -w //app \
	  -e GOPROXY=https://goproxy.cn,direct golang:1.25-alpine \
	  sh -c "apk add --no-cache gcc musl-dev >/dev/null && CGO_ENABLED=1 go test -race ./..."

run:
	go build -o deploy-backend.exe . && ./deploy-backend.exe
