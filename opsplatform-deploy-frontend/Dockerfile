FROM node:22-alpine AS builder
WORKDIR /src
# v129 起：通过 --build-arg VERSION=vXXX 把版本号注入到 Vite build 时的环境变量，
# 前端 AppLayout 通过 import.meta.env.VITE_APP_VERSION 读，保证侧边栏显示的版本号
# 跟 image tag 永远一致（不再依赖手动改 AppLayout.vue 里的字符串）。
# 缺省 dev：未传 --build-arg 时（本地开发）显示 "vdev"。
ARG VERSION=dev
ENV VITE_APP_VERSION=$VERSION
ENV NPM_CONFIG_REGISTRY=https://registry.npmmirror.com
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
RUN npm run build

# nginx:mainline-alpine = nginx 1.29 on alpine 3.22（libxpm 3.5.19+，修 CVE-2026-4367）
FROM nginx:mainline-alpine
RUN apk upgrade --no-cache && \
    apk add --no-cache tzdata && \
    ln -sf /usr/share/zoneinfo/Asia/Shanghai /etc/localtime && \
    rm -f /etc/nginx/conf.d/default.conf
COPY --from=builder /src/dist /usr/share/nginx/html
# nginx.conf 走 ConfigMap 挂载到 /etc/nginx/conf.d/default.conf
EXPOSE 80
