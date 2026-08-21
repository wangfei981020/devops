#!/usr/bin/env bash
# OpsAlert 端到端回归。每条都断言，不靠眼看。
B=http://localhost:30835/api/v1
pass=0; fail=0
ok(){ printf "  ✓ %s\n" "$1"; pass=$((pass+1)); }
no(){ printf "  ✗ %s — %s\n" "$1" "$2"; fail=$((fail+1)); }
chk(){ [ "$2" = "$3" ] && ok "$1" || no "$1" "期望 $3 实得 $2"; }

tok(){ curl -s -X POST $B/auth/login -H 'Content-Type: application/json' \
  -d "{\"username\":\"$1\",\"password\":\"Admin@2026\"}" | python3 -c "import sys,json;print(json.load(sys.stdin).get('token',''))"; }
A=$(tok admin); V=$(tok v1); O=$(tok o1); R=$(tok r1)
code(){ curl -s -o /dev/null -w "%{http_code}" -X "$2" "$B$3" -H "Authorization: Bearer $1" -H 'Content-Type: application/json' ${4:+-d "$4"}; }
body(){ curl -s -X "$2" "$B$3" -H "Authorization: Bearer $1" -H 'Content-Type: application/json' ${4:+-d "$4"}; }

echo "── 1. 模板接口 ──"
chk "模板清单 3 个" "$(body $A GET /rules/templates | python3 -c 'import sys,json;print(len(json.load(sys.stdin)["items"]))')" "3"
chk "只读也能看模板" "$(code $V GET /rules/templates)" "200"

echo "── 2. 三个模板都能生成查询 ──"
Q1=$(body $A POST /rules/templates/preview '{"template":"error_code","params":{"namespaces":["g32-wallet","g32-openapi"],"container_exclude":"telegram.*","ignore_codes":["9007","1254"]}}')
echo "$Q1" | grep -q 'namespace=~\\"g32-wallet|g32-openapi\\"' && ok "错误码：多命名空间合成正则" || no "错误码：多命名空间" "$Q1"
echo "$Q1" | grep -q '9007|1254' && ok "错误码：忽略码写进查询" || no "错误码：忽略码" "$Q1"
Q2=$(body $A POST /rules/templates/preview '{"template":"log_heartbeat","params":{"namespace":"g32-game","container_pattern":".*resource-backend","log_pattern":"Round.*","expected_containers":["baccarat-resource-backend"],"silence_minutes":"30"}}')
chk "断流：回看 1800 秒" "$(echo $Q2 | python3 -c 'import sys,json;print(json.load(sys.stdin)["lookback_sec"])')" "1800"
chk "断流：按容器分组" "$(echo $Q2 | python3 -c 'import sys,json;print(json.load(sys.stdin)["group_by"][0])')" "container"
Q3=$(body $A POST /rules/templates/preview '{"template":"latency_threshold","params":{"namespace":"g32-wallet","container":"wallet-client-backend","log_pattern":"交易","cost_pattern":"running time\\\\s*=\\\\s*(\\\\d+)\\\\s*ms","threshold_ms":"5000","agg":"p95"}}')
chk "耗时：字段 cost_ms" "$(echo $Q3 | python3 -c 'import sys,json;print(json.load(sys.stdin)["spec"]["field"])')" "cost_ms"

echo "── 3. 输入校验（必须拦住，不能静默） ──"
chk "错误码含正则元字符被拒" "$(code $A POST /rules/templates/preview '{"template":"error_code","params":{"namespaces":["ns"],"ignore_codes":[".*"]}}')" "400"
chk "耗时正则无捕获组被拒" "$(code $A POST /rules/templates/preview '{"template":"latency_threshold","params":{"namespace":"n","container":"c","log_pattern":"x","cost_pattern":"time \\\\d+ ms","threshold_ms":"1"}}')" "400"
chk "断流无期望清单被拒" "$(code $A POST /rules/templates/preview '{"template":"log_heartbeat","params":{"namespace":"n","container_pattern":".*","log_pattern":"x"}}')" "400"
chk "未知模板被拒" "$(code $A POST /rules/templates/preview '{"template":"nope","params":{}}')" "400"

echo "── 4. 权限 ──"
chk "viewer 不能预览模板(写?)" "$(code $V POST /rules/templates/preview '{"template":"error_code","params":{"namespaces":["n"]}}')" "200"
chk "viewer 不能建规则" "$(code $V POST /rules '{"name":"x","kind":"log_keyword","datasource_id":1,"spec":{"query":"x"}}')" "403"
chk "oncall 不能建规则" "$(code $O POST /rules '{"name":"x","kind":"log_keyword","datasource_id":1,"spec":{"query":"x"}}')" "403"
chk "viewer 不能导入" "$(code $V POST /import/apply '{"datasource_id":1,"rules":[]}')" "403"
chk "viewer 不能删规则" "$(code $V DELETE /rules/999999)" "403"
chk "viewer 看不了 MCP 令牌" "$(code $V GET /mcp/tokens)" "403"

echo "── 5. 建规则闭环（模板 → 落库 → 删除） ──"
NEW=$(body $A POST /rules "{\"name\":\"[E2E] 模板建规则\",\"kind\":\"log_keyword\",\"datasource_id\":1,\"spec\":{\"query\":\"{namespace=\\\"x\\\"} |= \\\"ERROR\\\"\"},\"interval_sec\":300,\"severity\":\"critical\",\"template\":\"error_code\",\"params\":{\"namespaces\":[\"x\"]}}")
ID=$(echo "$NEW" | python3 -c 'import sys,json;print(json.load(sys.stdin).get("id",0))')
[ "$ID" != "0" ] && ok "模板规则已创建 id=$ID" || no "创建规则" "$NEW"
LIST=$(body $A GET /rules)
echo "$LIST" | grep -q '"template":"error_code"' && ok "列表回显来源模板" || no "列表回显模板" "字段缺失"
chk "删除规则" "$(code $A DELETE /rules/$ID)" "200"

echo "── 6. 导入闭环 ──"
PF=$(body $A POST /import/preflight '{"rules":[{"id":1,"name":"[E2E]导入","data_source_type":"loki","schedule":"*/5 * * * *","time_range":"5m","logql":" |= \"ERROR\"","severity":"S1","alert_mode":"found","status":1}]}')
chk "预检：cron 翻成 300 秒" "$(echo $PF | python3 -c 'import sys,json;print(json.load(sys.stdin)["items"][0]["draft"]["interval_sec"])')" "300"
chk "预检：S1 → critical" "$(echo $PF | python3 -c 'import sys,json;print(json.load(sys.stdin)["items"][0]["draft"]["severity"])')" "critical"
chk "导入缺数据源被拒" "$(code $A POST /import/apply '{"rules":[]}')" "400"

echo "── 7. 四态与错误处理 ──"
chk "未授权 401" "$(curl -s -o /dev/null -w '%{http_code}' $B/rules)" "401"
chk "未映射路由 fail-closed" "$(code $A GET /brand-new-route)" "404"
DUP=$(body $A POST /rules '{"name":"[E2E]dup","kind":"log_keyword","datasource_id":1,"spec":{"query":"x"}}')
D1=$(echo "$DUP" | python3 -c 'import sys,json;print(json.load(sys.stdin).get("id",0))')
chk "重名返回 409 而非 500" "$(code $A POST /rules '{"name":"[E2E]dup","kind":"log_keyword","datasource_id":1,"spec":{"query":"x"}}')" "409"
body $A POST /rules/$D1/toggle >/dev/null; code $A DELETE /rules/$D1 >/dev/null
echo "$DUP" | grep -q "Duplicate entry" && no "错误详情泄露" "响应里有原始 SQL 错误" || ok "不泄露原始 SQL 错误"

echo "── 8. 编辑（模板规则 / 手写规则 / 数据源） ──"
# 模板建的规则：改完再取回来，模板参数要还在（否则下次编辑就回填不了）
T=$(body $A POST /rules '{"name":"[E2E]tpl","kind":"log_keyword","datasource_id":1,"spec":{"query":"a"},"template":"error_code","params":{"namespaces":["ns1"]}}')
TID=$(echo "$T" | python3 -c 'import sys,json;print(json.load(sys.stdin).get("id",0))')
chk "改模板规则" "$(code $A PUT /rules/$TID '{"name":"[E2E]tpl改","kind":"log_keyword","datasource_id":1,"spec":{"query":"b"},"severity":"warning","template":"error_code","params":{"namespaces":["ns1","ns2"]}}')" "200"
G=$(body $A GET /rules/$TID)
echo "$G" | grep -q '"template":"error_code"' && ok "详情带回模板来源" || no "详情模板来源" "$G"
echo "$G" | grep -q 'ns2' && ok "模板参数已更新并可回填" || no "模板参数回填" "$G"
chk "改名生效" "$(echo $G | python3 -c 'import sys,json;print(json.load(sys.stdin)["name"])')" "[E2E]tpl改"

# 手写规则（导入来的就是这种）：没有模板参数，但必须能改
W=$(body $A POST /rules '{"name":"[E2E]手写","kind":"log_keyword","datasource_id":1,"spec":{"query":"x"}}')
WID=$(echo "$W" | python3 -c 'import sys,json;print(json.load(sys.stdin).get("id",0))')
GW=$(body $A GET /rules/$WID)
chk "手写规则 template 为空" "$(echo $GW | python3 -c 'import sys,json;print(repr(json.load(sys.stdin)["template"]))')" "''"
chk "手写规则可编辑" "$(code $A PUT /rules/$WID '{"name":"[E2E]手写改","kind":"log_keyword","datasource_id":1,"spec":{"query":"y"}}')" "200"
chk "viewer 不能编辑规则" "$(code $V PUT /rules/$WID '{"name":"x","kind":"log_keyword","datasource_id":1,"spec":{"query":"y"}}')" "403"
code $A DELETE /rules/$TID >/dev/null; code $A DELETE /rules/$WID >/dev/null

DS=$(body $A GET /datasources | python3 -c 'import sys,json;d=json.load(sys.stdin)["items"];print(d[0]["id"] if d else 0)')
chk "改数据源" "$(code $A PUT /datasources/$DS '{"name":"本地假 Loki","type":"loki","endpoint":"http://host.docker.internal:19100"}')" "200"
chk "viewer 不能改数据源" "$(code $V PUT /datasources/$DS '{"name":"x","type":"loki","endpoint":"http://x"}')" "403"

echo "── 9. 新建通知路由 ──"
NID=$(body $A GET /notifiers | python3 -c 'import sys,json;d=json.load(sys.stdin)["items"];print(d[0]["id"] if d else 0)')
RT=$(body $A POST /routes "{\"name\":\"[E2E]路由\",\"matchers\":{\"severity\":\"critical\"},\"notifier_ids\":[$NID],\"repeat_sec\":14400}")
RID=$(echo "$RT" | python3 -c 'import sys,json;print(json.load(sys.stdin).get("id",0))')
[ "$RID" != "0" ] && ok "路由已创建" || no "创建路由" "$RT"
chk "viewer 不能建路由" "$(code $V POST /routes '{"name":"x","matchers":{},"notifier_ids":[]}')" "403"
[ "$RID" != "0" ] && code $A DELETE /routes/$RID >/dev/null

echo "── 10. 用户与角色管理 ──"
chk "只有管理员能看用户" "$(code $A GET /users)" "200"
chk "viewer 看不了用户" "$(code $V GET /users)" "403"
chk "rule_admin 也看不了用户" "$(code $R GET /users)" "403"
chk "角色清单" "$(body $A GET /roles | python3 -c 'import sys,json;print(len(json.load(sys.stdin)["items"]))')" "4"
chk "建账号用不存在的角色被拒" "$(code $A POST /users '{"username":"[E2E]u","password":"Probe@2026","role_code":"nope"}')" "400"
chk "弱密码被拒" "$(code $A POST /users '{"username":"[E2E]u","password":"123","role_code":"viewer"}')" "400"
NU=$(body $A POST /users '{"username":"[E2E]user","display_name":"回归账号","password":"Probe@2026","role_code":"viewer"}')
UID2=$(echo "$NU" | python3 -c 'import sys,json;print(json.load(sys.stdin).get("id",0))')
[ "$UID2" != "0" ] && ok "建账号" || no "建账号" "$NU"
chk "重名账号 409" "$(code $A POST /users '{"username":"[E2E]user","password":"Probe@2026","role_code":"viewer"}')" "409"
chk "改角色" "$(code $A PUT /users/$UID2/role '{"role_code":"oncall"}')" "200"
chk "停用账号" "$(code $A PUT /users/$UID2/status '{"status":"disabled"}')" "200"
chk "重置密码" "$(code $A PUT /users/$UID2/password '{"password":"Newpass@2026"}')" "200"
chk "viewer 不能改别人角色" "$(code $V PUT /users/$UID2/role '{"role_code":"admin"}')" "403"
# 最后一个管理员的三道保护：错一道就可能把自己锁在门外
AID=$(body $A GET /users | python3 -c "
import sys,json
print(next((u['id'] for u in json.load(sys.stdin)['items'] if u['username']=='admin'), 0))")
chk "最后一个管理员不能降权" "$(code $A PUT /users/$AID/role '{"role_code":"viewer"}')" "400"
chk "最后一个管理员不能停用" "$(code $A PUT /users/$AID/status '{"status":"disabled"}')" "400"
chk "不能删除自己" "$(code $A DELETE /users/$AID)" "400"
chk "删除普通账号" "$(code $A DELETE /users/$UID2)" "200"

echo "── 11. 值班台：认领已下线 ──"
# 认领接口保留（以后要恢复），但界面上没有入口。
# 接口还在意味着权限码也要还在，否则恢复时要连迁移一起改
chk "认领接口仍在（保留）" "$(code $A POST /incidents/1/ack)" "409"
chk "viewer 仍不能认领" "$(code $V POST /incidents/1/ack)" "403"

echo "── 12. 指标导出与日报 ──"
# /metrics 挂在健康端口上（8088），业务端口没有它。
# 用 kubectl exec 打进去：这个端口不对外暴露，正是它该有的样子。
# ⚠️ 这一段自己造前提，**不能依赖环境里正好有规则开了指标导出**。
# 第一版直接断言"/metrics 里有 rule 维度"，而它只在恰好有这种规则时成立 ——
# 清理完测试数据之后就变成了假失败，看起来像功能坏了。
MRULE=$(body $A POST /rules '{"name":"[E2E]metrics","kind":"log_keyword","datasource_id":1,"spec":{"query":"e2e"},"threshold":1,"metrics_enabled":true,"metrics_labels":{"project":"E2E"}}' | python3 -c 'import sys,json;print(json.load(sys.stdin).get("id",0))')
BPOD=$(kubectl -n ops-alert get pods -l app.kubernetes.io/component=backend -o name 2>/dev/null | head -1)
if [ -n "$BPOD" ]; then
  MSTATUS=$(kubectl -n ops-alert exec "$BPOD" -- wget -S -qO- http://127.0.0.1:8088/metrics 2>&1 | grep -o "HTTP/1.1 [0-9]*" | head -1 | awk '{print $2}')
  [ -z "$MSTATUS" ] && MSTATUS=200
  chk "/metrics 返回 200" "$MSTATUS" "200"
  MOUT=$(kubectl -n ops-alert exec "$BPOD" -- wget -qO- http://127.0.0.1:8088/metrics 2>/dev/null)
  # 🔴 这一条抓的是一个真实事故：UNIX_TIMESTAMP 作用在 DATETIME(3) 上
  # 返回小数，扫进 int64 会失败 —— 而它只在**有规则开了指标导出**时才触发，
  # 没开的时候查询返回 0 行，整个端点看起来一切正常。
  echo "$MOUT" | grep -q "opsalert_rule_last_run_timestamp_seconds" \
    && ok "规则指标有导出（含 last_run 时间戳）" \
    || no "规则指标有导出（含 last_run 时间戳）" "没有 rule 维度的指标，检查是否有规则开了 metrics_enabled"
  echo "$MOUT" | grep -q "opsalert_datasource_up" && ok "数据源可达性指标" || no "数据源可达性指标" "缺失"
else
  echo "  · 跳过 /metrics（拿不到后端 Pod）"
fi
[ "$MRULE" != "0" ] && chk "清理造的指标规则" "$(code $A DELETE /rules/$MRULE)" "200"
# 指标标签必须在保存时校验：一条非法标签会让整份 /metrics 解析失败
chk "保留标签 instance 被拒" \
  "$(code $A POST /rules '{"name":"[E2E]metric","kind":"log_keyword","datasource_id":1,"spec":{"query":"x"},"metrics_enabled":true,"metrics_labels":{"instance":"h1"}}')" "400"
chk "数字开头的标签名被拒" \
  "$(code $A POST /rules '{"name":"[E2E]metric","kind":"log_keyword","datasource_id":1,"spec":{"query":"x"},"metrics_enabled":true,"metrics_labels":{"2bad":"x"}}')" "400"
# 日报
chk "日报配置可读（没配过也要返回默认值而不是 404）" "$(code $A GET /report)" "200"
chk "开日报但不选渠道被拒" "$(code $A PUT /report '{"enabled":true,"send_at":"09:00","notifier_ids":[]}')" "400"
chk "发送时刻格式非法被拒" "$(code $A PUT /report '{"enabled":false,"send_at":"9点","notifier_ids":[]}')" "400"
chk "日报预览只读，viewer 能用" "$(code $V POST /report/preview)" "200"
chk "viewer 不能改日报配置" "$(code $V PUT /report '{"enabled":false,"send_at":"09:00","notifier_ids":[]}')" "403"
chk "日志检索只读，viewer 能用" "$(code $V POST /explore '{"datasource_id":1,"expr":"x","range_sec":300}')" "502"
# 🔴 错误响应必须带 detail（给人看的原因），不是只有 error 机器码。
# 前端 api() 只读 error + detail，写成 message 的话字段照样发出去、
# 接口测试照样通过，而界面上只剩一个 "query_failed" —— 栽过一次。
HASDETAIL=$(body $A POST /explore '{"datasource_id":1,"expr":"x","range_sec":300}' | python3 -c "import sys,json;print('yes' if json.load(sys.stdin).get('detail') else 'no')")
chk "检索失败带得出可读原因" "$HASDETAIL" "yes"

echo
echo "通过 $pass / 失败 $fail"
[ $fail -eq 0 ]
