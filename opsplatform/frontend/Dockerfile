FROM nginx:alpine

# 复制前端文件
COPY . /usr/share/nginx/html/

# 复制 Nginx 主配置（含 Gzip）
COPY nginx-main.conf /etc/nginx/nginx.conf

# 复制 Server 配置（使用新文件名避免缓存）
COPY nginx-server.conf /etc/nginx/conf.d/default.conf

EXPOSE 80

CMD ["nginx", "-g", "daemon off;"]
