FROM node:20-alpine AS builder
WORKDIR /src
ENV NPM_CONFIG_REGISTRY=https://registry.npmmirror.com
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM nginx:1.27-alpine
RUN apk upgrade --no-cache && \
    apk add --no-cache tzdata && \
    ln -sf /usr/share/zoneinfo/Asia/Shanghai /etc/localtime && \
    rm -f /etc/nginx/conf.d/default.conf
COPY --from=builder /src/dist /usr/share/nginx/html
# nginx.conf 走 ConfigMap 挂载到 /etc/nginx/conf.d/default.conf
EXPOSE 80
