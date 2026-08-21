# syntax=docker/dockerfile:1
#
# 构建必须传版本号：
#   docker build --build-arg VERSION=v12 --build-arg GIT_COMMIT=$(git rev-parse --short HEAD) .
# 漏传会落成 dev，线上排障时看不出前端是哪一版 —— 上一代四个前端全踩过。
# 构建脚本 tooling/scripts/build-image.sh 已把这件事收口，优先用它。

# ---------- 构建 ----------
FROM node:26-alpine AS builder

ARG VERSION=dev
ARG GIT_COMMIT=unknown
ENV VERSION=$VERSION GIT_COMMIT=$GIT_COMMIT

# Node 25 起官方镜像不再自带 corepack，直接装 pnpm。
# 版本与根 package.json 的 packageManager 字段保持一致 ——
# 不一致时 pnpm 会警告并可能用不同的 lockfile 解析规则，
# 导致镜像里装出来的依赖和本地不完全一样。
RUN npm i -g pnpm@11.20.0
WORKDIR /repo

# 先只拷清单，让依赖层能被缓存 —— 改业务代码时不必重装依赖
COPY pnpm-workspace.yaml pnpm-lock.yaml package.json ./
# 逐个列出来是为了让依赖层可缓存。加新共享包时这里要跟着加一行 ——
# 漏了的表现是 pnpm install 装不全，而错误信息指向业务代码。
COPY packages/design/package.json packages/design/
COPY packages/i18n/package.json packages/i18n/
COPY packages/ui/package.json packages/ui/
COPY packages/api/package.json packages/api/
COPY ops-alert/frontend/package.json ops-alert/frontend/
RUN pnpm install --frozen-lockfile

COPY . .
# 共享包要先产出 dist，应用才能解析到它们的类型与入口
# 用通配而不是逐个点名：漏掉一个共享包时，vite 只会说
# "Failed to resolve entry for package @ops/xxx"，完全看不出是"没构建"。
# 通配让加新包这件事不需要记得改 Dockerfile。
RUN pnpm -r --filter './packages/*' build \
 && pnpm --filter ops-alert-frontend build

# ---------- 运行 ----------
# 用 unprivileged 版：官方 nginx 镜像以 root 启动，
# 而 chart 里设了 runAsNonRoot，两者对不上 Pod 会 CrashLoop。
# 这个镜像默认监听 8080，非特权端口，不需要 NET_BIND_SERVICE。
FROM nginxinc/nginx-unprivileged:1.29-alpine

ARG VERSION=dev
ARG GIT_COMMIT=unknown
LABEL org.opencontainers.image.version=$VERSION \
      org.opencontainers.image.revision=$GIT_COMMIT \
      org.opencontainers.image.title="ops-alert-frontend"

USER root
RUN apk upgrade --no-cache
COPY --from=builder /repo/ops-alert/frontend/dist /usr/share/nginx/html
# 版本号同时落进静态文件，运行中的容器可以直接 curl /version.json 问它是哪一版
RUN printf '{"version":"%s","commit":"%s"}\n' "$VERSION" "$GIT_COMMIT" \
      > /usr/share/nginx/html/version.json \
 && chown -R 101:101 /usr/share/nginx/html
# ⚠️ 必须是**数字**，不能写 `USER nginx`。
# runAsNonRoot: true 时 kubelet 要在启动前判断镜像用户是不是 root，
# 名字它查不到 uid，直接报 image has non-numeric user，Pod 起不来。
# 101 是 nginx-unprivileged 镜像里 nginx 用户的 uid。
#
# ⚠️ uid 只在这里定义一次，chart 只写 runAsNonRoot（CONVENTIONS §4.5）。
USER 101

EXPOSE 8080
