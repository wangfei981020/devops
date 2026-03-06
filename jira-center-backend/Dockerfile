FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o jira-center-backend .

FROM alpine:latest
RUN apk --no-cache add ca-certificates tzdata
ENV TZ=Asia/Shanghai
WORKDIR /app
COPY --from=builder /app/jira-center-backend .
EXPOSE 8080
CMD ["./jira-center-backend"]
