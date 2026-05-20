FROM golang:1.25-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o alert-backend .

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=builder /app/alert-backend .

ENV TZ=Asia/Shanghai
EXPOSE 8080 8088

ENTRYPOINT ["/app/alert-backend"]
