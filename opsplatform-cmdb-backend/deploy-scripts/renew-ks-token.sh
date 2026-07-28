#!/usr/bin/env bash
# KubeSphere token 自动续期 —— 拿新 token 写回 CMDB 数据源。
#
# 背景：KubeSphere 4.x 的 token 有效期由全局 authentication.issuer.accessTokenMaxAge 控制（DEV=168h/7天），
#       没法为单个 client 单独设不过期，所以用定时任务续。建议 6 天跑一次，留 1 天容错余量。
#
# 用法：
#   1) 同目录建 renew-ks-token.env（chmod 600），内容见下方变量说明
#   2) chmod +x renew-ks-token.sh && ./renew-ks-token.sh   # 先手动跑一次
#   3) crontab -e 加：  0 3 */6 * *  /opt/scripts/renew-ks-token.sh >> /var/log/ks-token-renew.log 2>&1
#
# 依赖：curl、jq
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENV_FILE="${ENV_FILE:-$SCRIPT_DIR/renew-ks-token.env}"
[ -f "$ENV_FILE" ] && . "$ENV_FILE"

# ---- 必填变量（放 env 文件里，别写死在脚本中）----
KS_URL="${KS_URL:?KubeSphere ks-apiserver 地址，如 http://10.146.40.241:32765}"
KS_USER="${KS_USER:?KubeSphere 只读账号，如 cmdb-readonly}"
KS_PASS="${KS_PASS:?KubeSphere 只读账号密码}"
KS_CLIENT_ID="${KS_CLIENT_ID:-kubesphere}"
KS_CLIENT_SECRET="${KS_CLIENT_SECRET:-kubesphere}"

CMDB_URL="${CMDB_URL:?CMDB 后端地址，如 https://cmdb-w.sl-devops.com}"
CMDB_USER="${CMDB_USER:?CMDB 管理员账号}"
CMDB_PASS="${CMDB_PASS:?CMDB 管理员密码}"
CMDB_CLUSTER_ID="${CMDB_CLUSTER_ID:-2}"   # 数据源绑定的集群（DEV=2）

# 保活用的探测路径：token 除了会到期，闲置超过 accessTokenInactivityTimeout 也会失效，
# 每次续期后主动调一次真实接口，避免"没人用 → 提前失效"。
KS_PROBE_PATH="${KS_PROBE_PATH:-/kapis/devops.kubesphere.io/v1alpha3/devops}"

log() { echo "[$(date '+%F %T')] $*"; }
die() { log "ERROR: $*"; exit 1; }

command -v jq >/dev/null || die "缺少 jq，请先安装（apt install jq / yum install jq）"

# ---- 1. 用密码模式换新 token ----
log "向 $KS_URL 换取新 token（user=$KS_USER）"
KS_RESP="$(curl -sS --max-time 20 -X POST "$KS_URL/oauth/token" \
  -H 'Content-Type: application/x-www-form-urlencoded' \
  --data-urlencode "grant_type=password" \
  --data-urlencode "username=$KS_USER" \
  --data-urlencode "password=$KS_PASS" \
  --data-urlencode "client_id=$KS_CLIENT_ID" \
  --data-urlencode "client_secret=$KS_CLIENT_SECRET")" || die "调用 /oauth/token 失败"

KS_TOKEN="$(echo "$KS_RESP" | jq -r '.access_token // empty')"
[ -n "$KS_TOKEN" ] || die "未拿到 access_token，响应：$(echo "$KS_RESP" | head -c 300)"
EXPIRES_IN="$(echo "$KS_RESP" | jq -r '.expires_in // "?"')"
log "拿到新 token，有效期 ${EXPIRES_IN}s"

# ---- 2. 验证新 token 真的可用（权限不够时早失败，别把废 token 写进 CMDB）----
PROBE_CODE="$(curl -sS -o /dev/null -w '%{http_code}' --max-time 20 \
  -H "Authorization: Bearer $KS_TOKEN" "$KS_URL$KS_PROBE_PATH")"
[ "$PROBE_CODE" = "200" ] || die "新 token 探测 $KS_PROBE_PATH 返回 $PROBE_CODE（权限或路径有问题），已中止，CMDB 保持旧 token"
log "新 token 权限校验通过（$KS_PROBE_PATH → 200）"

# ---- 3. 登录 CMDB ----
CMDB_JWT="$(curl -sS --max-time 20 -X POST "$CMDB_URL/api/login" \
  -H 'Content-Type: application/json' \
  -d "$(jq -n --arg u "$CMDB_USER" --arg p "$CMDB_PASS" '{username:$u,password:$p}')" \
  | jq -r '.token // empty')"
[ -n "$CMDB_JWT" ] || die "CMDB 登录失败"

# ---- 4. 取回该数据源的当前配置 ----
# ⚠️ PUT /obs-endpoints/{id} 是全字段覆盖，漏字段会被清空，必须先读回来原样带上。
EP="$(curl -sS --max-time 20 -H "Authorization: Bearer $CMDB_JWT" "$CMDB_URL/api/obs-endpoints" \
  | jq -c --argjson cid "$CMDB_CLUSTER_ID" 'map(select(.type=="kubesphere" and .cluster_id==$cid)) | .[0] // empty')"
[ -n "$EP" ] || die "CMDB 里没找到 cluster_id=$CMDB_CLUSTER_ID 的 kubesphere 数据源，请先在「数据源接入」页建好"

EP_ID="$(echo "$EP" | jq -r '.id')"
log "命中 CMDB 数据源 id=$EP_ID name=$(echo "$EP" | jq -r '.name')"

# ---- 5. 写回新 token ----
BODY="$(echo "$EP" | jq -c --arg tok "$KS_TOKEN" \
  '{name,type,url,env,cluster_id,enabled,token:$tok}')"
RESP="$(curl -sS --max-time 20 -X PUT "$CMDB_URL/api/obs-endpoints/$EP_ID" \
  -H "Authorization: Bearer $CMDB_JWT" -H 'Content-Type: application/json' -d "$BODY")"
echo "$RESP" | jq -e '.ok == true' >/dev/null || die "写回 CMDB 失败：$(echo "$RESP" | head -c 300)"

log "OK：token 已续期并写回 CMDB（endpoint id=$EP_ID）"
