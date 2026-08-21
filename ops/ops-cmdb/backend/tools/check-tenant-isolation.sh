#!/usr/bin/env bash
#
# 租户隔离的静态防线。
#
# store 层能拦住「走了 store 但忘写 tenant_id」，拦不住「压根没走 store」。
# 这个脚本补的就是后者：业务代码直接持有 *sql.DB 就等于绕过了全部隔离。
#
# ⚠️ 当前处于重构期：存量 handlers/ 还是直接用 *sql.DB，逐步迁移中。
#    所以这里用**基线数量**而不是零容忍 —— 只要不比基线更差就放行，
#    每迁走一个 handler 就把基线调小。零容忍会让脚本从第一天起就是红的，
#    而一个永远红的检查等于没有检查。
#
# 用法：
#   tools/check-tenant-isolation.sh          检查
#   tools/check-tenant-isolation.sh -update  迁移后更新基线
# 不开 pipefail：本脚本全是统计型 grep，无匹配返回 1 是正常结果。
# 开了 pipefail 会让「没发现问题」变成「脚本崩溃」—— 恰好是最该通过的情况反而失败。
set -eu
cd "$(dirname "$0")/.."

BASELINE_FILE="tools/.tenant-isolation-baseline"

fail=0
note() { printf '  %s\n' "$*"; }

# ── 1. 新代码（internal/）绝不许直接碰 *sql.DB ────────────────────
# internal/ 是重构后的新结构，这里零容忍。
# 排除三类：注释行、测试文件、以及本来就该直连的基础设施。
#
# ⚠️ 排除注释是必须的 —— 讲解「为什么不能用 *sql.DB」的文档注释里必然出现这个词，
#    不排除的话，写注释解释规则的人反而会被规则拦下。这已经是本轮第三次踩误报了
#    （前两次：check-undefined 不认识 defineProps、字符类检查匹配到自己的注释）。
new_raw=$( { grep -rn '\*sql\.DB' --include='*.go' internal/ 2>/dev/null || true; } \
  | grep -vE ':[[:space:]]*(//|\*|/\*)' \
  | grep -v '_test.go' \
  | grep -v 'internal/store/' \
  | grep -v 'internal/testutil/' \
  | grep -v 'internal/api/middleware/tenant.go' \
  || true)
if [ -n "$new_raw" ]; then
  echo "❌ internal/ 下不允许直接持有 *sql.DB（必须经 store 层）："
  echo "$new_raw" | sed 's/^/    /'
  fail=1
fi

# ── 2. 存量代码的迁移进度 ────────────────────────────────────────
legacy=$(grep -rln '\*sql\.DB' --include='*.go' handlers/ 2>/dev/null | grep -v '_test.go' | wc -l | tr -d ' ')

UPDATE_MODE=${1:-}
if [ "${UPDATE_MODE}" = "-update" ]; then
  echo "${legacy}" > "$BASELINE_FILE"
  echo "✅ 存量文件基线 → ${legacy}"
fi

# ⚠️ 基线文件缺失时**不能**回落到当前值 —— 那样基线永远等于现状，
#    检查永远通过，等于没有检查。越权检查曾因此形同虚设。
if [ ! -f "$BASELINE_FILE" ]; then
  echo "❌ 缺少基线 $BASELINE_FILE，先跑一次 -update 初始化"
  exit 1
fi
baseline=$(cat "$BASELINE_FILE")
if [ "$legacy" -gt "$baseline" ]; then
  echo "❌ 直接用 *sql.DB 的存量文件从 $baseline 增加到 $legacy"
  note "新代码必须走 store 层。若确属存量改动，说明理由后用 -update 调整基线。"
  fail=1
else
  echo "✅ 存量 *sql.DB 文件数 ${legacy}（基线 ${baseline}）"
fi

# ── 3. 平台查询器的调用点必须可控 ────────────────────────────────
# Platform() 能绕开租户过滤，调用点多了就失去意义。
plat=$( { grep -rn '\.Platform(' --include='*.go' . 2>/dev/null || true; } \
  | grep -v '_test.go' | grep -v 'internal/store/' | wc -l | tr -d ' ')
if [ "$plat" -gt 20 ]; then
  echo "❌ Platform() 调用点 $plat 处，超过 20 —— 它能绕开租户过滤，不该遍地都是"
  fail=1
else
  echo "✅ Platform() 调用点 $plat 处"
fi

# ── 4. 迁移里新增的表必须有 tenant_id 或进白名单 ──────────────────
# 只查最近未登记的新建表，避免每次都扫全量历史。
new_tables=$(grep -ohiE 'CREATE TABLE (IF NOT EXISTS )?`?[a-z0-9_]+`?' database/migrations/*.sql 2>/dev/null \
  | sed -E 's/.*TABLE (IF NOT EXISTS )?//I' | tr -d '`' | sort -u || true)
missing=""
for t in $new_tables; do
  # 白名单里的跳过
  if grep -q "\"${t}\":" internal/store/whitelist.go 2>/dev/null; then continue; fi
  # 096 里加过 tenant_id 的跳过
  if grep -q "ALTER TABLE $t ADD COLUMN tenant_id" database/migrations/*.sql 2>/dev/null; then continue; fi
  # 建表语句自带 tenant_id 的跳过
  if grep -A40 -iE "CREATE TABLE (IF NOT EXISTS )?\`?${t}\`?" database/migrations/*.sql 2>/dev/null \
     | grep -qi 'tenant_id'; then continue; fi
  missing="$missing $t"
done
if [ -n "$missing" ]; then
  echo "❌ 下列表既没有 tenant_id 也不在平台白名单里：$missing"
  note "要么补迁移加列，要么在 internal/store/whitelist.go 登记（需评审）"
  fail=1
else
  echo "✅ 所有表都有租户归属"
fi

# ── 5. 越权模式：写操作用 WHERE id=? 而没有 tenant_id ────────────
#
# 这是最危险的一类：任何租户只要知道 id 就能改删别人的数据，
# 而功能测试全过（改自己的能成功），只有跨租户用例能抓到。
#
# registrars 迁移时实测存在，全库共 70 处。同样用基线制 ——
# 迁一个少一个，绝不许增加。
#
# ⚠️ 平台表要排除：users / auth_sessions 等本来就跨租户，
#    对它们写 `WHERE id=?` 不是越权。不排除的话这几条会永远挂在计数里，
#    基线降不到 0，久而久之就没人再看这个数字了 ——
#    一个永远"还剩几条"的检查，和没有检查差不多。
#    （users 的可见范围另有设计问题，见 docs/plans/open-question-user-scoping.md）
overreach=$( { grep -rn "UPDATE .* WHERE id[[:space:]]*=[[:space:]]*?\|DELETE FROM .* WHERE id[[:space:]]*=[[:space:]]*?" \
  --include='*.go' handlers/ 2>/dev/null || true; } | grep -v '_test.go' \
  | grep -vE "(UPDATE|DELETE FROM) (users|auth_sessions|tenants|user_tenants|idp_configs|idp_group_mappings|licenses|schema_migrations|ci_types)\b" \
  | wc -l | tr -d ' ')
ob_file="tools/.overreach-baseline"
if [ "${UPDATE_MODE}" = "-update" ]; then
  echo "${overreach}" > "${ob_file}"
  echo "✅ 越权基线 → ${overreach}"
fi
if [ ! -f "${ob_file}" ]; then
  echo "❌ 缺少基线文件 ${ob_file}，先跑一次 -update 初始化"
  exit 1
fi
ob=$(cat "${ob_file}")
if [ "${overreach}" -gt "${ob}" ]; then
  echo "❌ 越权写操作从 ${ob} 增加到 ${overreach} 处（WHERE id=? 缺 tenant_id）"
  note "新代码必须走 store 层；存量迁移只会让这个数字变小"
  fail=1
else
  echo "✅ 越权写操作 ${overreach} 处（基线 ${ob}，迁一个少一个）"
fi

# ── 6. Scoped 调用的 SQL 必须带租户条件 ──────────────────────────
#
# store.Scoped 在运行时会拒绝没有 tenant_id 的语句，但那要等到线上有人
# 点开那个页面才暴露。这里把同一条规则前移到静态检查。
if python3 tools/check-scoped-sql.py; then
  :
else
  fail=1
fi

# ── 7. 有 Store 字段的 struct，构造函数必须真的设置它 ────────────
#
# 批量迁移时踩过：struct 加了 Store 字段但构造函数忘了传，
# **编译完全通过**（nil 指针在编译期合法），运行时第一次调用就 panic。
# 这类"编译绿但代码坏"的状态最危险 —— CI 全绿，上线才炸。
missing_store=""
for f in $(grep -rl 'Store \*store\.Store' --include='*.go' handlers/ internal/ 2>/dev/null || true); do
  case "${f}" in *_test.go) continue;; esac
  # ⚠️ 不要用 `grep -c ... || echo 0`：grep -c 无匹配时**既输出 0 又返回 1**，
  #    `|| echo 0` 会再追加一个 0，变量变成两行，数值比较随之失效 ——
  #    检查于是永远不报错。用 grep -q 判存在最直接。
  if grep -qE 'func New[A-Za-z]+\(' "${f}" && ! grep -q 'Store:' "${f}"; then
    missing_store="${missing_store} ${f}"
  fi
done
if [ -n "${missing_store}" ]; then
  echo "❌ 下列文件有 Store 字段但构造函数未设置（运行时会 nil panic）：${missing_store}"
  fail=1
else
  echo "✅ Store 字段均已在构造函数中设置"
fi

# ── 8. 后台循环必须过 leader 闸 ─────────────────────────────────
if python3 tools/check-background-loops.py; then
  :
else
  fail=1
fi

# ── 9. 字符类必须含数字 ─────────────────────────────────────────
# 血泪教训：用 [a-z_]+ 提表名会把 k8s_pods 截断成 k，
# 25 张表因此静默漏掉了 tenant_id。这条防止同样的正则再被写出来。
# 排除注释行 —— 本文件的注释里就写着这个反例，不排除会自己报自己。
# （误报比漏报更糟：防线一旦开始喊狼来了，人就会绕过它。）
bad_charclass=$( { grep -rn "a-z_\]+" --include='*.sh' tools/ 2>/dev/null || true; } \
  | grep -v 'a-z0-9_' | grep -vE ':[[:space:]]*#' || true)
if [ -n "${bad_charclass}" ]; then
  echo "❌ 脚本里出现不含数字的表名字符类 —— 会漏掉 k8s_* 这类表名"
  echo "${bad_charclass}" | sed 's/^/    /'
  fail=1
else
  echo "✅ 表名字符类均含数字"
fi

echo
[ "$fail" = 0 ] && echo "✅ 租户隔离检查通过" || echo "❌ 租户隔离检查未通过"
exit $fail
