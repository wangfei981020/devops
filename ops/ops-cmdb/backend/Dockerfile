# syntax=docker/dockerfile:1
#
# ⚠️ 这行 syntax 声明不是装饰：下面的 `RUN --mount=type=cache` 依赖它。
# 少了它，BuildKit 按最老的语法解析，报的是「unknown flag: mount」——
# 而那个错完全看不出真因是缺一行注释。
# ⚠️ 构建上下文是 **ops/**（仓库里存放各产品的那一层），不是 backend/。
#
#	后端通过 `replace ops-kit/licensekit => ../../ops-kit/licensekit` 依赖同仓的
#	共享库。上下文若只给 backend/，那个目录根本不在里面，
#	`go mod download` 会报「open /ops-kit/licensekit/go.mod: no such file」。
#
#	正确构建命令（见 Makefile 的 image-backend）：
#	  docker buildx build -f ops-cmdb/backend/Dockerfile <...> ops/
#
# 多架构：构建层固定跑在本机架构，用 GOOS/GOARCH 交叉编译出目标架构二进制（免 QEMU）
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
WORKDIR /src

# 先只拷 go.mod/go.sum 让依赖层可缓存 —— 改业务代码不必重下依赖。
COPY ops-kit/licensekit/go.mod ops-kit/licensekit/go.sum* ./ops-kit/licensekit/
COPY ops-cmdb/backend/go.mod ops-cmdb/backend/go.sum ./ops-cmdb/backend/
WORKDIR /src/ops-cmdb/backend
# 模块缓存挂成 cache mount：依赖层本来就靠 COPY go.mod 分层缓存，
# 但那层一旦失效（改了任何一个依赖）就要重新下**全部**依赖。
# 有了 cache mount，失效时只下真正变了的那几个。
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

WORKDIR /src
COPY ops-kit/licensekit ./ops-kit/licensekit
COPY ops-cmdb/backend ./ops-cmdb/backend
WORKDIR /src/ops-cmdb/backend

ARG TARGETOS TARGETARCH
# VERSION 打进二进制，排障时能直接看出跑的是哪个版本 ——
# 靠镜像 tag 推断在滚动更新中途是不可靠的。
ARG VERSION=dev
# ⚠️ 两个 cache mount 都要有，少一个就白搭：
#   /go/pkg/mod        —— 依赖源码
#   /root/.cache/go-build —— **编译产物**，包括标准库
# 没有第二个的话，每次构建都要把整个标准库重编一遍（Go 不会把它预编译进镜像）。
#
# ⚠️ build cache 按架构分开（id 带 TARGETARCH）。
# 共用一份的话，两个架构会不停地把对方的产物挤出去 ——
# 表现是"明明有缓存却每次都全量编译"，而且看不出原因。
ARG TARGETPLATFORM
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build,id=gobuild-$TARGETPLATFORM \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -ldflags="-s -w -X main.version=${VERSION}" -o /out/ops-cmdb-backend .

FROM alpine:3.21
RUN apk upgrade --no-cache && \
    apk add --no-cache ca-certificates tzdata && \
    cp /usr/share/zoneinfo/Asia/Manila /etc/localtime
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
COPY --from=build --chown=10001:10001 /out/ops-cmdb-backend /app/ops-cmdb-backend
WORKDIR /app
USER 10001
EXPOSE 8080 8088
ENTRYPOINT ["/app/ops-cmdb-backend"]
