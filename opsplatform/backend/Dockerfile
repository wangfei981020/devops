# 构建阶段
# --platform=$BUILDPLATFORM: builder 始终跑在构建机原生架构上(不走 QEMU 模拟)，
# 靠 Go 交叉编译产出目标架构二进制，多架构构建快得多
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS builder

# buildx 自动注入，单平台 docker build 时为空则回落到构建机架构
ARG TARGETARCH

WORKDIR /app

# 安装 swag CLI（钉版本）
RUN go install github.com/swaggo/swag/cmd/swag@v1.16.4

# 复制依赖文件
COPY go.mod go.sum ./
RUN go mod download

# 复制源码
COPY . .

# 生成 OpenAPI spec（覆盖仓库里的版本，保证镜像里的是最新代码生成的）
RUN swag init --parseDependency --parseInternal -o ./docs/swagger -g main.go

# 构建二进制
# 必须显式指定 GOARCH，否则跟着构建机走：
# 在 Apple Silicon 上 docker build 会产出 arm64 镜像，GKE(amd64) 拉下来直接 exec format error
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH:-amd64} go build -o opsplatform .

# 运行阶段（钉小版本 + apk upgrade 兜底所有后发 CVE）
FROM alpine:3.21

WORKDIR /app

# 升级 OS 层 + 装 ca-certificates / tzdata 并保留时区
RUN apk upgrade --no-cache && \
    apk add --no-cache ca-certificates tzdata && \
    cp /usr/share/zoneinfo/Asia/Shanghai /etc/localtime && \
    echo "Asia/Shanghai" > /etc/timezone

# 复制二进制文件
COPY --from=builder /app/opsplatform .

# 复制 OpenAPI spec
COPY --from=builder /app/docs/swagger /app/docs/swagger

# 暴露端口
EXPOSE 8088

# 启动命令
CMD ["./opsplatform"]

