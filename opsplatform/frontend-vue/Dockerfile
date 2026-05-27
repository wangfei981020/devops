FROM node:22-alpine AS builder

WORKDIR /app

COPY package*.json ./
RUN rm -rf node_modules package-lock.json && npm install

COPY . .
RUN npm run build

FROM nginx:1.29-alpine

# 升级 OS 层包以消除 nginx:alpine 镜像里的已知 CVE（libcrypto3/libssl3/libpng/musl/zlib 等）
# 同时移除 curl/libxml2/freetype 等非 nginx 必需包以减少 CVE 暴露面
RUN apk upgrade --no-cache && \
    apk del --no-cache curl libcurl libxml2 freetype fontconfig libavif libheif 2>/dev/null || true

COPY --from=builder /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY nginx.conf /tmp/default.conf

EXPOSE 80

CMD ["nginx", "-g", "daemon off;"]
