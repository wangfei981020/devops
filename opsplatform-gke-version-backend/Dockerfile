FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/gke-version-backend .

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata && \
    cp /usr/share/zoneinfo/Asia/Shanghai /etc/localtime
COPY --from=build /out/gke-version-backend /app/gke-version-backend
WORKDIR /app
EXPOSE 8080
ENTRYPOINT ["/app/gke-version-backend"]
