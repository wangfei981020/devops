#!/usr/bin/env bash
# Deploy Center backend smoke test
# 假设后端已启动、MySQL 就绪。真正 git/argocd 流程需要真实仓库。
set -euo pipefail
API=${API:-http://localhost:8080/api}

echo "== 1. GET global config =="
curl -sf "$API/global-config" | python -m json.tool | head -5

echo "== 2. list project_envs =="
curl -sf "$API/project-envs" | python -m json.tool | head -5

echo "== 3. preview deploy (will mark is_new if modules not scanned) =="
curl -sf -X POST "$API/deploy/preview-image" -H "Content-Type: application/json" \
  -d '{"project_env_id":1,"text":"foo:v2\nbar:v3"}' | python -m json.tool | head -20

echo "== 4. list deployments =="
curl -sf "$API/deployments" | python -m json.tool | head -10

echo "DONE (smoke finished; real git/argocd flow needs real repo + argocd)"
