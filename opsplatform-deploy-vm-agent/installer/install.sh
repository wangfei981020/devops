#!/bin/bash
# install.sh — 把 deploy-vm-agent 装到 ansible 控制机
#
# 用法：
#   sudo bash install.sh [-u <agent_user>] [-p <listen_port>] [--cert <path>] [--key <path>]
#
#   默认：
#     agent_user = deploy-agent
#     listen_port = 8443
#     无 TLS 证书（跑 HTTP，仅本地测试；生产请提供 --cert / --key）
#
# 行为：幂等的；多次运行只覆盖二进制 + systemd unit + sudoers，不重置 token/config

set -euo pipefail

AGENT_USER="${AGENT_USER:-deploy-agent}"
LISTEN_PORT="${LISTEN_PORT:-8443}"
TLS_CERT=""
TLS_KEY=""
COPY_ROOT_SSH_KEY="${COPY_ROOT_SSH_KEY:-no}"  # yes 表示从 /root/.ssh 复制 key 给 agent 用户

# ---- 参数解析 ----
while [[ $# -gt 0 ]]; do
  case $1 in
    -u|--user)         AGENT_USER="$2"; shift 2 ;;
    -p|--port)         LISTEN_PORT="$2"; shift 2 ;;
    --cert)            TLS_CERT="$2"; shift 2 ;;
    --key)             TLS_KEY="$2"; shift 2 ;;
    --copy-ssh-key)    COPY_ROOT_SSH_KEY="yes"; shift ;;
    -h|--help)
      sed -n '2,15p' "$0"
      exit 0 ;;
    *)
      echo "❌ 未知参数: $1" >&2
      exit 1 ;;
  esac
done

if [[ $EUID -ne 0 ]]; then
  echo "❌ 必须以 root 运行（sudo bash install.sh ...）" >&2
  exit 1
fi

INSTALL_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN_NAME="deploy-vm-agent"
BIN_SRC="$INSTALL_DIR/$BIN_NAME"
BIN_DST="/usr/local/bin/$BIN_NAME"
SERVICE_SRC="$INSTALL_DIR/deploy-vm-agent.service"
SERVICE_DST="/etc/systemd/system/deploy-vm-agent.service"
SUDOERS_SRC="$INSTALL_DIR/sudoers.template"
SUDOERS_DST="/etc/sudoers.d/deploy-vm-agent"
CONFIG_DIR="/etc/deploy-vm-agent"
CONFIG_FILE="$CONFIG_DIR/config.yaml"
LOG_DIR="/var/log/deploy-vm-agent"

ANSIBLE_ROOT="${ANSIBLE_ROOT:-/etc/ansible}"

echo "▶ 安装 deploy-vm-agent"
echo "  user:        $AGENT_USER"
echo "  port:        $LISTEN_PORT"
echo "  tls cert:    ${TLS_CERT:-（空，HTTP）}"
echo "  ansible:     $ANSIBLE_ROOT"
echo

# ---- 1. 依赖检查 ----
echo "▶ 检查依赖"
for cmd in ansible-playbook git python python3 systemctl sudo; do
  if ! command -v "$cmd" >/dev/null 2>&1; then
    echo "  ⚠️  $cmd 不在 PATH"
  else
    echo "  ✅ $cmd"
  fi
done

# ---- 2. 创建用户 ----
if id "$AGENT_USER" &>/dev/null; then
  echo "▶ 用户 $AGENT_USER 已存在，跳过"
else
  echo "▶ 创建用户 $AGENT_USER"
  useradd --system --create-home --shell /bin/bash "$AGENT_USER"
fi

# ---- 3. chown ansible 目录给 agent 用户 ----
if [[ -d "$ANSIBLE_ROOT" ]]; then
  echo "▶ chown $ANSIBLE_ROOT → $AGENT_USER（让 agent 直接 git pull，不需 sudo）"
  chown -R "$AGENT_USER":"$AGENT_USER" "$ANSIBLE_ROOT"
else
  echo "  ⚠️  $ANSIBLE_ROOT 不存在，先 git clone ansible_cicd 再装 agent"
fi

# ---- 4. SSH key（让 agent 能 SSH 到目标 VM）----
AGENT_HOME="$(getent passwd "$AGENT_USER" | cut -d: -f6)"
mkdir -p "$AGENT_HOME/.ssh"
chmod 700 "$AGENT_HOME/.ssh"
if [[ "$COPY_ROOT_SSH_KEY" == "yes" ]] && [[ -f /root/.ssh/id_rsa ]]; then
  echo "▶ 复制 /root/.ssh/id_rsa → $AGENT_HOME/.ssh/"
  cp -p /root/.ssh/id_rsa{,.pub} "$AGENT_HOME/.ssh/" 2>/dev/null || true
  if [[ -f /root/.ssh/known_hosts ]]; then
    cp -p /root/.ssh/known_hosts "$AGENT_HOME/.ssh/" 2>/dev/null || true
  fi
fi
chown -R "$AGENT_USER":"$AGENT_USER" "$AGENT_HOME/.ssh"
chmod 600 "$AGENT_HOME/.ssh/"* 2>/dev/null || true

# ---- 5. sudoers ----
echo "▶ 安装 sudoers $SUDOERS_DST"
sed "s/__AGENT_USER__/$AGENT_USER/g" "$SUDOERS_SRC" > "$SUDOERS_DST"
chmod 0440 "$SUDOERS_DST"
visudo -cf "$SUDOERS_DST" >/dev/null

# ---- 6. 二进制 ----
echo "▶ 装二进制 $BIN_DST"
install -m 0755 "$BIN_SRC" "$BIN_DST"

# ---- 7. systemd unit ----
echo "▶ 装 systemd unit $SERVICE_DST"
sed "s/__AGENT_USER__/$AGENT_USER/g" "$SERVICE_SRC" > "$SERVICE_DST"

# ---- 8. 配置目录 + config.yaml（首次安装才生成 token）----
mkdir -p "$CONFIG_DIR" "$LOG_DIR"
chown "$AGENT_USER":"$AGENT_USER" "$LOG_DIR"

if [[ ! -f "$CONFIG_FILE" ]]; then
  echo "▶ 生成 $CONFIG_FILE（首次安装）"
  TOKEN=$(head -c 32 /dev/urandom | base64 | tr -d '/+=' | head -c 40)
  CONFIG_TEMPLATE="$INSTALL_DIR/config.example.yaml"
  cp "$CONFIG_TEMPLATE" "$CONFIG_FILE"
  # 替换 token / port / tls 路径
  sed -i "s|REPLACE_ME_WITH_RANDOM_TOKEN|$TOKEN|" "$CONFIG_FILE"
  sed -i "s|listen: \":8443\"|listen: \":$LISTEN_PORT\"|" "$CONFIG_FILE"
  if [[ -n "$TLS_CERT" ]]; then
    sed -i "s|tls_cert: \"/etc/deploy-vm-agent/agent.crt\"|tls_cert: \"$TLS_CERT\"|" "$CONFIG_FILE"
    sed -i "s|tls_key:  \"/etc/deploy-vm-agent/agent.key\"|tls_key:  \"$TLS_KEY\"|" "$CONFIG_FILE"
  else
    # 没传证书 → 注释掉 TLS（跑 HTTP，仅本地测试）
    sed -i 's|^tls_cert:|#tls_cert:|' "$CONFIG_FILE"
    sed -i 's|^tls_key:|#tls_key:|' "$CONFIG_FILE"
  fi
  chmod 0640 "$CONFIG_FILE"
  chown root:"$AGENT_USER" "$CONFIG_FILE"
  GENERATED_NEW_TOKEN=1
else
  echo "▶ $CONFIG_FILE 已存在，保留现有配置（含 token）"
  TOKEN=$(grep -E '^token:' "$CONFIG_FILE" | sed 's/^token: *"\(.*\)"$/\1/')
  GENERATED_NEW_TOKEN=0
fi

# ---- 9. 启动 ----
echo "▶ systemctl enable + start"
systemctl daemon-reload
systemctl enable deploy-vm-agent >/dev/null 2>&1 || true
systemctl restart deploy-vm-agent

sleep 1
if systemctl is-active --quiet deploy-vm-agent; then
  echo "  ✅ deploy-vm-agent 已启动"
else
  echo "  ❌ 启动失败，查日志："
  echo "      journalctl -u deploy-vm-agent --since '1 minute ago' --no-pager"
  exit 1
fi

# ---- 10. 输出 ----
HOST_IP="$(hostname -I 2>/dev/null | awk '{print $1}')"
SCHEME=$([[ -n "$TLS_CERT" ]] && echo "https" || echo "http")

echo
echo "════════════════════════════════════════════════════════════════"
echo "  ✅ 安装完成"
echo "════════════════════════════════════════════════════════════════"
echo "  Agent URL: ${SCHEME}://${HOST_IP}:${LISTEN_PORT}"
if [[ "$GENERATED_NEW_TOKEN" -eq 1 ]]; then
  echo "  Agent Token (一次性显示，请妥善保存):"
  echo
  echo "      $TOKEN"
  echo
  echo "  ⚠️  Token 之后存在 $CONFIG_FILE（root + $AGENT_USER 可读）"
fi
echo
echo "  下一步：在 deploy-center →【系统设置】→ Deploy Agents → 添加："
echo "    name: ansible-main"
echo "    url:  ${SCHEME}://${HOST_IP}:${LISTEN_PORT}"
echo "    token: <上面那个或现有 token>"
echo
echo "  Smoke test:"
echo "    curl -k ${SCHEME}://${HOST_IP}:${LISTEN_PORT}/v1/health"
echo
echo "  日志:  journalctl -u deploy-vm-agent -f"
echo "  停止:  sudo systemctl stop deploy-vm-agent"
echo "════════════════════════════════════════════════════════════════"
