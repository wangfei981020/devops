FROM node:20-alpine AS build
WORKDIR /src
COPY package*.json ./
RUN npm ci || npm install
COPY . .
RUN npm run build

FROM nginx:1.29-alpine
RUN apk upgrade --no-cache
# 不在镜像内放 nginx.conf —— 部署时由 K8s ConfigMap (gke-version-exporter-frontend-nginx)
# 挂载到 /etc/nginx/conf.d/default.conf，改 nginx 配置不用重 build。
COPY --from=build /src/dist /usr/share/nginx/html
EXPOSE 80
