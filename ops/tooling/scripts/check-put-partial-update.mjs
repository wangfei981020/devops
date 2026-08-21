#!/usr/bin/env node
/**
 * 守卫：PUT/PATCH 不许把「没传的字段」当成「要清空」。
 *
 * # 为什么
 *
 * 写接口的常见写法是「一次 UPDATE 写死所有列」，值取自绑定后的结构体。
 * Go 的零值让**「没传这个字段」和「把它设成空」完全无法区分**：
 *
 *	PUT /api/domains/171  {"expiry_at": "2026-09-02"}
 *	→ 200 {"ok": true}，而这个域名的 name 变成了空（OPSCMDB-083）
 *
 * 🔴 界面上**永远看不出来**，因为表单总是全量提交（打开弹窗时先填好现有值）。
 *	而同一个接口对外开着（MCP / 脚本 / AI），那一侧按 REST 直觉发部分字段，
 *	就会静默清空其余的 —— 200 OK，没有任何提示。
 *	这是「参数缺失被当成一个有意义的取值」的一种（另一种见 OPSCMDB-058）。
 *
 * # 判据（锚在形状上）
 *
 * 一个 PUT/PATCH 处理器同时满足这三条即报：
 *   ① ShouldBindJSON(&x) 绑定了某个结构体
 *   ② 执行了 `UPDATE ... SET` 且赋值列 >= 2
 *   ③ 该结构体里存在**非指针**的标量字段
 *
 * ⚠️ 判据不看字段名、不看表名 —— 换个命名就绕过去的判据等于没有。
 *
 * # 基线（棘轮）
 *
 * 存量 25 处是同一形态，逐个改要连带改前端与调用方，分批做。
 * 这里只挡**新增**：基线只能降不能升。
 */
import { existsSync, readFileSync, readdirSync, statSync } from 'node:fs'
import { join, relative } from 'node:path'
import { ROOT, backendDirs } from './lib/products.mjs'

// 已知存量。⚠️ 只能减不能加 —— 改好一个就从这里删掉一行。
//
// ⚠️ 不是每一条都必须改成 PATCH：像整份设置的保存（saveOIDCConfig）
//	本来就是"整体替换"语义。但**留在基线里就必须逐条判过**，
//	不能因为"看着像故意的"就默认它没问题 —— domains 那条当初也看着像故意的。
const BASELINE = new Set([
  'ops-alert/backend/internal/api/crud.go:updateDatasource',
  'ops-alert/backend/internal/api/msgtemplate.go:updateMsgTemplate',
  'ops-alert/backend/internal/api/oidc_admin.go:saveOIDCConfig',
  'ops-alert/backend/internal/api/rules.go:updateRule',
  'ops-cmdb/backend/handlers/audit_api.go:RevertChange',
  'ops-cmdb/backend/handlers/basic.go:UpdateCdn',
  'ops-cmdb/backend/handlers/basic.go:UpdateEnv',
  'ops-cmdb/backend/handlers/basic.go:UpdateProject',
  'ops-cmdb/backend/handlers/basic.go:UpdateStatus',
  'ops-cmdb/backend/handlers/cdn.go:SaveAccount',
  'ops-cmdb/backend/handlers/cert_inspect.go:Ignore',
  'ops-cmdb/backend/handlers/ci.go:Update',
  'ops-cmdb/backend/handlers/domains.go:BulkIgnore',
  'ops-cmdb/backend/handlers/gke_upgrade.go:OverrideSchedule',
  'ops-cmdb/backend/handlers/harbor.go:Save',
  'ops-cmdb/backend/handlers/hosts.go:UpdateAccount',
  'ops-cmdb/backend/handlers/hosts.go:UpdateComputeRate',
  'ops-cmdb/backend/handlers/hosts.go:UpdateProject',
  'ops-cmdb/backend/handlers/k8s_clusters.go:Update',
  'ops-cmdb/backend/handlers/obs_endpoints.go:Update',
  'ops-cmdb/backend/handlers/records.go:BulkIgnore',
  'ops-cmdb/backend/handlers/records.go:Update',
  'ops-cmdb/backend/handlers/users.go:ChangeRole',
  'ops-cmdb/backend/internal/api/automate/notify/lark.go:Update',
  'ops-cmdb/backend/internal/api/inventory/registrar/handler.go:Update',
])

function walkGo(dir, out = []) {
  if (!existsSync(dir)) return out
  for (const n of readdirSync(dir)) {
    if (n === 'vendor' || n === 'node_modules') continue
    const p = join(dir, n)
    if (statSync(p).isDirectory()) walkGo(p, out)
    else if (p.endsWith('.go') && !p.endsWith('_test.go')) out.push(p)
  }
  return out
}

/** 从 `{` 起配对到对应的 `}`，返回函数体 */
function bodyFrom(src, start) {
  let i = start
  let depth = 1
  while (depth > 0 && i < src.length) {
    if (src[i] === '{') depth++
    else if (src[i] === '}') depth--
    i++
  }
  return src.slice(start, i)
}

/**
 * 结构体字段里有没有非指针的标量。
 *
 * ⚠️ 必须同时认**匿名内联结构体**（`var in struct { ... }`）——
 *	实测 19 处存量里 15 处是内联写法，只查 `type X struct` 的话
 *	守卫会一条都扫不到然后报绿。判据覆盖形态定义太窄，它的绿色就是假的。
 */
function firstNonPointerScalar(structBody) {
  for (const line of structBody.split('\n')) {
    if (/^\s*(\/\/|$)/.test(line)) continue
    const f = /^\s*(\w+)\s+(\[\]|\*|map\[)?\s*(string|int|int64|bool|float64)\b/.exec(line)
    if (f && !f[2]) return f[1] // 非指针、非切片、非 map
  }
  return false
}

/** 绑定变量的结构体字段区（内联或具名）。找不到定义返回 null —— 不猜 */
function structBodyOf(src, fnBody, varName) {
  const inline = new RegExp(`var ${varName} struct \\{`).exec(fnBody)
  if (inline) return bodyFrom(fnBody, inline.index + inline[0].length)
  const named = new RegExp(`var ${varName} (\\w+)`).exec(fnBody)
  if (!named) return null
  const def = new RegExp(`type ${named[1]} struct \\{`).exec(src)
  if (!def) return null
  return bodyFrom(src, def.index + def[0].length)
}

const problems = []
const seen = new Set()
let scanned = 0
for (const d of backendDirs()) {
  for (const f of walkGo(join(ROOT, d))) {
    const rel = relative(ROOT, f)
    const src = readFileSync(f, 'utf8')
    scanned++
    for (const m of src.matchAll(/func \(\w+ \*(\w+)\) (\w+)\(c \*gin\.Context\) \{/g)) {
      const fn = m[2]
      const body = bodyFrom(src, m.index + m[0].length)
      const bind = /ShouldBindJSON\(&(\w+)\)/.exec(body)
      if (!bind) continue
      const sb = structBodyOf(src, body, bind[1])
      if (sb === null) continue // 定义找不到就不猜
      const updates = [...body.matchAll(/UPDATE\s+\w+\s+SET\s+([\s\S]*?)WHERE/gi)]
      if (updates.length === 0) continue
      const cols = Math.max(
        ...updates.map((u) => (u[1].match(/=\s*\?|=\s*NULLIF|=\s*COALESCE/g) ?? []).length),
      )
      if (cols < 2) continue
      const bad = firstNonPointerScalar(sb)
      if (!bad) continue
      const key = `${rel}:${fn}`
      if (seen.has(key)) continue
      seen.add(key)
      if (!BASELINE.has(key)) problems.push(`${key}  （字段 ${bad} 是非指针，缺省会被写成零值）`)
    }
  }
}

// 基线里已经改好的要及时删掉，否则基线会掩护后来的回归
const stale = [...BASELINE].filter((k) => !seen.has(k))

if (problems.length > 0) {
  console.error('✗ check-put-partial-update: 写接口会把「没传的字段」清空：\n')
  for (const p of problems) console.error(`    ${p}`)
  console.error(`
用指针区分三态（见 handlers/patchset.go）：
  nil = 没传（不动它） / 非 nil 指向零值 = 显式清空 / 非 nil 有值 = 改成它

⚠️ 不要用"缺省时保留原值"绕过：那会让调用方再也没法把一个字段真的改成空。
⚠️ 界面看不出这个问题 —— 表单总是全量提交。受害的是 MCP / 脚本 / AI 那一侧。`)
  process.exit(1)
}
if (stale.length > 0) {
  console.error('✗ check-put-partial-update: 基线里有已经不存在的条目，请删掉：\n')
  for (const s of stale) console.error(`    ${s}`)
  console.error('\n基线只能减不能加。留着过时的条目会掩护后来的回归。')
  process.exit(1)
}
console.log(
  `✓ check-put-partial-update: ${scanned} 个后端文件，无新增（存量基线 ${BASELINE.size} 处待分批改）`,
)
