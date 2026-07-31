# 多架构：前端编译产物(dist)与架构无关，构建层固定跑本机架构（免 QEMU），只有 nginx 运行层按目标架构
FROM --platform=$BUILDPLATFORM node:22-alpine AS build
# ⚠️ 构建时必须传：docker build --build-arg VERSION=v<NN> ...
# 不传就落成 dev，登录页版本号显示 "dev"，排障时无法确认前端到底是哪一版。
ARG VERSION=dev
ENV VITE_APP_VERSION=$VERSION
WORKDIR /src
COPY package*.json ./
RUN npm ci || npm install
COPY . .
RUN npm run build

FROM nginx:1.29-alpine
RUN apk upgrade --no-cache
# nginx.conf 由 K8s ConfigMap 挂载到 /etc/nginx/conf.d/default.conf（改配置不用重 build）
COPY --from=build /src/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 80
