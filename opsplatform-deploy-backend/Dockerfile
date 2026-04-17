FROM golang:1.24-alpine AS builder

WORKDIR /app
COPY go.mod go.sum* ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o deploy-backend .

FROM alpine:3.19
RUN apk --no-cache add ca-certificates tzdata git openssh
WORKDIR /app
COPY --from=builder /app/deploy-backend .

ENV TZ=Asia/Shanghai
EXPOSE 8080 8088

CMD ["./deploy-backend"]
