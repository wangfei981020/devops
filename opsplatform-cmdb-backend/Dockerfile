# 多架构：构建层固定跑在本机架构，用 GOOS/GOARCH 交叉编译出目标架构二进制（免 QEMU）
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags="-s -w" -o /out/cmdb-backend .

FROM alpine:3.21
RUN apk upgrade --no-cache && \
    apk add --no-cache ca-certificates tzdata && \
    cp /usr/share/zoneinfo/Asia/Shanghai /etc/localtime
COPY --from=build /out/cmdb-backend /app/cmdb-backend
WORKDIR /app
EXPOSE 8080 8088
ENTRYPOINT ["/app/cmdb-backend"]
