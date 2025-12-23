# 构建阶段
FROM golang:1.24-alpine AS builder

WORKDIR /app

# 复制依赖文件
COPY go.mod go.sum ./
RUN go mod download

# 复制源码并构建
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o opsplatform .

# 运行阶段
FROM alpine:latest

WORKDIR /app

# 复制二进制文件
COPY --from=builder /app/opsplatform .

# 暴露端口
EXPOSE 8088

# 启动命令
CMD ["./opsplatform"]

