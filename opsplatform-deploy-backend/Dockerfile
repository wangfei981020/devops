# ---- builder ----
FROM golang:1.23-alpine AS builder
WORKDIR /src
ENV GOPROXY=https://goproxy.cn,direct
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/deploy-backend .

# ---- runtime ----
FROM alpine:3.19
RUN apk add --no-cache git ca-certificates tzdata && \
    ln -sf /usr/share/zoneinfo/Asia/Shanghai /etc/localtime
WORKDIR /app
COPY --from=builder /out/deploy-backend /app/deploy-backend
EXPOSE 8080 8081
ENV PORT=:8080 HEALTH_PORT=:8081 GIT_CACHE_DIR=/app/git-cache
CMD ["/app/deploy-backend"]
