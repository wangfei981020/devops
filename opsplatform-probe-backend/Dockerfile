FROM golang:1.21-alpine AS builder
WORKDIR /build
RUN apk add --no-cache git
COPY go.mod ./
RUN go mod download || true
COPY . .
RUN go mod tidy
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags "-s -w" -o probe-backend .

FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=builder /build/probe-backend /app/probe-backend
EXPOSE 8080 8088
ENTRYPOINT ["/app/probe-backend"]
