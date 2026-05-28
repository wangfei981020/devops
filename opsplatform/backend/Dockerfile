# 构建阶段
FROM golang:1.25-alpine AS builder

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
RUN CGO_ENABLED=0 GOOS=linux go build -o opsplatform .

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

