# 镜像作为"自包含安装包"载体：
#   docker pull <image>
#   docker run --rm -v /tmp/agent-install:/out <image> cp -rT /install /out
#   sudo bash /tmp/agent-install/install.sh
#
# 镜像里的 agent 二进制不会真在容器里运行，容器启动只是为了把 /install/* 拷出来

FROM golang:1.25-alpine AS builder
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
# 静态编译：linux/amd64，不依赖 glibc，复制到任何 Linux 直接跑
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -trimpath -ldflags="-s -w" \
    -o /out/deploy-vm-agent ./cmd/agent

FROM alpine:3.23
COPY --from=builder /out/deploy-vm-agent              /install/deploy-vm-agent
COPY installer/install.sh                              /install/install.sh
COPY installer/deploy-vm-agent.service                 /install/deploy-vm-agent.service
COPY installer/sudoers.template                        /install/sudoers.template
COPY installer/config.example.yaml                     /install/config.example.yaml

# entrypoint 默认打印用法，避免有人误把镜像当 daemon 跑
RUN chmod +x /install/install.sh /install/deploy-vm-agent
ENTRYPOINT ["/bin/sh", "-c"]
CMD ["echo '此镜像是 agent 安装包载体（不是 daemon）。\\n用法：\\n  docker run --rm -v /tmp/agent-install:/out <image> cp -rT /install /out\\n  sudo bash /tmp/agent-install/install.sh'"]
