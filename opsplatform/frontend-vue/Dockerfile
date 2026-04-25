FROM node:22-alpine AS builder

WORKDIR /app

COPY package*.json ./
RUN rm -rf node_modules package-lock.json && npm install

COPY . .
RUN npm run build

FROM nginx:1.27-alpine

# 升级 OS 层包以消除 nginx:alpine 镜像里的已知 CVE（libcrypto3/libssl3/libpng/musl/zlib 等）
RUN apk upgrade --no-cache

COPY --from=builder /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY nginx.conf /tmp/default.conf

EXPOSE 80

CMD ["nginx", "-g", "daemon off;"]
