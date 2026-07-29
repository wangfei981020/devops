#!/bin/bash
# 修复 k8s-port-proxy 的 nginx.conf 让它转发到 kind cluster 当前的 IP。
#
# 背景：每次 Docker Desktop 重启，kind 容器（desktop-control-plane）的 IP 会被
# Docker 重新分配（如 172.22.0.2 → 172.22.0.5），nginx.conf 写死的 IP 失效，
# 所有 NodePort（30081 运维平台 / 30826 发布中心 / 30091 MinIO ...）都会 502。
#
# 双击运行此脚本即可一键修复。
#
# 用法：
#   git-bash / wsl: bash fix-cluster-ip.sh
#   或 Windows: 创建快捷方式指向 "C:\Program Files\Git\bin\bash.exe fix-cluster-ip.sh"

set -e

CONF="$(dirname "$0")/nginx.conf"

if [ ! -f "$CONF" ]; then
  echo "✗ 找不到 nginx.conf: $CONF"
  exit 1
fi

# 查 kind cluster 当前实际 IP
NEW_IP=$(docker inspect desktop-control-plane -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' 2>/dev/null)
if [ -z "$NEW_IP" ]; then
  echo "✗ kind cluster (desktop-control-plane) 没起来，先启动 Docker Desktop"
  exit 1
fi
echo "kind cluster 当前 IP: $NEW_IP"

# 查 nginx.conf 里现有的 IP（取第一个 172.x.x.x —— kind 网段随 Docker 分配，
# 不能写死成 172.22.0.x，否则换了网段就抓不到旧 IP，替换会静默失败）
OLD_IP=$(grep -oE '172\.[0-9]+\.[0-9]+\.[0-9]+' "$CONF" | head -1)
echo "nginx.conf 里旧 IP: $OLD_IP"

if [ "$OLD_IP" = "$NEW_IP" ]; then
  echo "✓ IP 已经一致，无需修改"
else
  # 备份 + 替换
  cp "$CONF" "$CONF.bak"
  # macOS 自带的是 BSD sed：-i 必须带参数，且基本正则不支持 \+。
  # 用 -E（两个实现都支持扩展正则）并按实现分支传 -i，Windows/WSL 的 GNU sed 照常可用。
  if sed --version >/dev/null 2>&1; then
    sed -i -E "s|172\.[0-9]+\.[0-9]+\.[0-9]+|$NEW_IP|g" "$CONF"   # GNU
  else
    sed -i '' -E "s|172\.[0-9]+\.[0-9]+\.[0-9]+|$NEW_IP|g" "$CONF" # BSD / macOS
  fi
  echo "✓ 已把 $OLD_IP 全部替换成 $NEW_IP（备份 nginx.conf.bak）"
fi

# reload k8s-port-proxy
if docker ps --format '{{.Names}}' | grep -q '^k8s-port-proxy$'; then
  docker exec k8s-port-proxy nginx -s reload 2>&1
  echo "✓ k8s-port-proxy nginx 已 reload"
else
  echo "⚠ k8s-port-proxy 容器没跑，启动中..."
  cd "$(dirname "$0")" && docker compose up -d
fi

# 验证几个关键端口
echo
echo "=== 端口连通性检查 ==="
for p in 30081 30826 30829 30830 30091; do
  code=$(curl -sk -o /dev/null -w "%{http_code}" --max-time 3 "http://localhost:$p/" 2>/dev/null || echo "timeout")
  printf "  localhost:%-6s -> HTTP %s\n" "$p" "$code"
done

echo
echo "✓ 完成。浏览器刷新即可访问。"
