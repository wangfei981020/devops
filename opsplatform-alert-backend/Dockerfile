# --platform=$BUILDPLATFORM pins the builder to the machine doing the build, so
# the Go toolchain runs natively and cross-compiles, rather than the whole build
# stage running under emulation.
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Without GOARCH the output silently follows whoever ran the build: an image
# built on Apple Silicon lands on an amd64 node as "exec format error", which
# surfaces only at pod start. TARGETOS/TARGETARCH come from the build platform
# (--platform, or the daemon default), so the binary always matches the image
# it is being put in.
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -o alert-backend .

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=builder /app/alert-backend .

ENV TZ=Asia/Shanghai
EXPOSE 8080 8088

ENTRYPOINT ["/app/alert-backend"]
