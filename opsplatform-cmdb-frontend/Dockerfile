FROM node:22-alpine AS build
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
