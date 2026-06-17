#!/bin/sh
# CMDB 证书自动拉取脚本（A+ 拉取式）——放目标服务器，cron 每天跑一次。
# 证书版本变了才下载 + reload nginx，没变直接退出。平台不需要你的服务器凭据。
#
# 用法（cron）：
#   0 3 * * *  CMDB_URL=http://cmdb.内网:30829 /opt/fetch-cert.sh <证书CI_ID> <deploy_token> /etc/nginx/certs
set -eu

CMDB="${CMDB_URL:-http://localhost:30829}"
CERT_ID="${1:?证书CI_ID}"
TOKEN="${2:?deploy_token}"
DIR="${3:-/etc/nginx/certs}"
API="$CMDB/api/certs/$CERT_ID/bundle?token=$TOKEN"

mkdir -p "$DIR"
ver="$(curl -fsS "$API&part=version")" || { echo "拉取版本失败"; exit 1; }

if [ -f "$DIR/.version" ] && [ "$(cat "$DIR/.version")" = "$ver" ]; then
  echo "证书已是最新版本 v$ver，无需更新"
  exit 0
fi

curl -fsS "$API&part=fullchain" -o "$DIR/fullchain.pem"
curl -fsS "$API&part=key"       -o "$DIR/key.pem"
chmod 600 "$DIR/key.pem"
echo "$ver" > "$DIR/.version"

# reload（按需改成你的 web 服务）
nginx -s reload 2>/dev/null || systemctl reload nginx 2>/dev/null || true
echo "证书已更新到 v$ver 并 reload"
