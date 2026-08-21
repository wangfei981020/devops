# ⚠️ 构建上下文是 **ops/**（仓库里存放各产品的那一层），不是 backend/。
#
#	后端通过 `replace ops-kit/licensekit => ../../ops-kit/licensekit` 依赖同仓的
#	共享库。上下文若只给 backend/，那个目录根本不在里面，
#	`go mod download` 会报「open /ops-kit/licensekit/go.mod: no such file」。
#
#	正确构建命令（见 Makefile 的 image-backend）：
#	  docker buildx build -f ops-alert/backend/Dockerfile <...> ops/
#
# 多架构：构建层固定跑在本机架构，用 GOOS/GOARCH 交叉编译出目标架构二进制（免 QEMU）
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
WORKDIR /src

# 先只拷 go.mod/go.sum 让依赖层可缓存 —— 改业务代码不必重下依赖。
COPY ops-kit/licensekit/go.mod ops-kit/licensekit/go.sum* ./ops-kit/licensekit/
COPY ops-alert/backend/go.mod ops-alert/backend/go.sum ./ops-alert/backend/
WORKDIR /src/ops-alert/backend
RUN go mod download

WORKDIR /src
COPY ops-kit/licensekit ./ops-kit/licensekit
COPY ops-alert/backend ./ops-alert/backend
WORKDIR /src/ops-alert/backend

ARG TARGETOS TARGETARCH
# VERSION 打进二进制，排障时能直接看出跑的是哪个版本 ——
# 靠镜像 tag 推断在滚动更新中途是不可靠的。
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -ldflags="-s -w -X main.version=${VERSION}" -o /out/ops-alert-backend .

FROM alpine:3.21
# tzdata 是必需的：业务时区由 TZ 环境变量给（默认 Asia/Shanghai），
# 程序用 time.LoadLocation 解析它。剥掉 tzdata 的话除 UTC 外所有名字都解析失败。
#
# ⚠️ 这里**不再** cp 某个时区到 /etc/localtime。
# 原来硬编码成 Asia/Manila，于是"日报每天 09:00 发"的实际时刻
# 由镜像决定而不是由配置决定，而这件事没有任何一处会说出来。
# 现在进程本地时区是 UTC，业务判定一律走 cfg.Location。
RUN apk upgrade --no-cache && \
    apk add --no-cache ca-certificates tzdata
# 非 root 运行：容器里跑的是只读采集 + 存着云凭据的服务，
# 没有任何需要 root 的操作，给了就是白送一个提权面。
#
# ⚠️ uid **只在这里定义一次**，chart 里只写 runAsNonRoot、不写 runAsUser
# （CONVENTIONS §4.5）。两处各写一个数必然分叉：撞到过 Dockerfile 写 10001、
# chart 写 65532，而 65532 在镜像的 /etc/passwd 里根本不存在 ——
# k8s 里 chart 赢跑 65532，docker run 跑 10001，同一个镜像两种身份，
# 而且两种都能跑，所以一直没人发现。
#
# ⚠️ 必须是数字。写 `USER app` 的话 kubelet 在 runAsNonRoot 下查不到 uid，
# 直接报 image has non-numeric user，Pod 起不来。
#
# `adduser -D` 会顺带建一个同名同 gid 的组，所以进程不会落进 gid 0。
RUN adduser -D -u 10001 app
COPY --from=build --chown=10001:10001 /out/ops-alert-backend /app/ops-alert-backend
WORKDIR /app
USER 10001
EXPOSE 8080 8088
ENTRYPOINT ["/app/ops-alert-backend"]
