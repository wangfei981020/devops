FROM node:22-alpine AS builder
WORKDIR /src
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
