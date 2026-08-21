#!/usr/bin/env node
/**
 * 守卫：界面不许直接渲染后端的 `msg` / `hint` 原句。
 *
 * # 为什么
 *
 * 后端的 `msg` / `hint` 是**还没迁移**的中文原句，同时也是留给 MCP / 直接调 API
 * 那一侧的（他们读不到语言包）。界面直接渲染它，英文界面上就是一句中文：
 *
 *	{done.msg ?? t('common:write.saved')}     ← 后端发了中文就显示中文
 *
 * 正确写法是走三级回退助手，它会优先用 `msg_key` / `hint_key`：
 *
 *	actionMessage(t, done)      lib/actionMessage.ts
 *	hintText(t, resp)           lib/hintText.ts
 *
 * 🔴 这一条是「后端迁了前端没接」的专用防线：后端补上 `msg_key` 之后，
 *	如果前端还在读 `.msg`，那次迁移**一个字都不会生效** ——
 *	而两边都"改过了"，看起来像已经完成。
 *
 * # 判据
 *
 * 先剥掉所有字符串字面量（`t('cost:snapshot.hint')` 里的 hint 不算），
 * 再找形如 `X.msg` / `X.data?.hint` 的读取。
 *
 * ⚠️ 有些 `.hint` 是**数据值**不是文案：MCP 令牌列表里的 `token.hint`
 *	是令牌前缀（`sk-abc…`），翻译它没有意义。这类只能逐个判过后进基线。
 *
 * ⚠️ 两类**不算**，直接排除（否则守卫会误报，而误报的代价是人开始忽略它）：
 *
 *	1. `queries.ts` 里的**字段映射**：`hint: d.hint ?? ''` 是把后端字段
 *	   搬进本地类型，正是三级回退所需要的一步 —— 报它等于反对正确写法。
 *	2. **前端自己的** toast/本地状态：`toast.msg` 与后端响应无关。
 *	   判据：同一文件里没有从 api/fetch 拿到这个对象的痕迹时不报 ——
 *	   实现上按变量名兜底（toast/msg0 这类本地状态命名）。
 */
import { existsSync, readFileSync, readdirSync, statSync } from 'node:fs'
import { join, relative } from 'node:path'
import { ROOT, frontendDirs } from './lib/products.mjs'

// 已知存量。⚠️ 只能减不能加。每条都必须**逐个判过**：
// 是"还没迁"（该改）还是"数据值"（不该翻译）。
const BASELINE = new Set([
  // —— 数据值，不是文案：令牌前缀 sk-abc…，翻译它没有意义 ——
  'ops-cmdb/frontend/src/routes/mcp/index.tsx:token.hint',
  // —— 还没迁：逐条项级的失败原因，后端尚未给 key ——
  'ops-cmdb/frontend/src/routes/domains/DnsRecordsDialog.tsx:e.msg',
  'ops-cmdb/frontend/src/routes/domains/DnsRecordsDialog.tsx:d.msg',
  'ops-cmdb/frontend/src/routes/domains/RenewDialog.tsx:r.msg',
  'ops-cmdb/frontend/src/routes/domains/RenewDialog.tsx:it.msg',
  'ops-cmdb/frontend/src/routes/hostrecords/RecordRowActions.tsx:check.data.msg',
  'ops-cmdb/frontend/src/routes/notify/index.tsx:test.data.msg',
  'ops-cmdb/frontend/src/routes/cost/index.tsx:snap.data.msg',
  'ops-cmdb/frontend/src/routes/health/DetailDialog.tsx:p.hint',
  'ops-cmdb/frontend/src/routes/domains/QualityView.tsx:d.hint',
  'ops-cmdb/frontend/src/routes/audit/DetailDialog.tsx:extra.hint',
  'ops-cmdb/frontend/src/routes/namespaces/NsProjectDialog.tsx:preview.hint',
  'ops-cmdb/frontend/src/routes/alerts/index.tsx:query.data.hint',
])

function walk(dir, out = []) {
  if (!existsSync(dir)) return out
  for (const n of readdirSync(dir)) {
    const p = join(dir, n)
    if (statSync(p).isDirectory()) walk(p, out)
    else if (/\.tsx?$/.test(p)) out.push(p)
  }
  return out
}

/** 剥掉字符串字面量与注释 —— t('xxx.hint') 里的 hint 不是在读字段 */
function strip(src) {
  return src
    .replace(/\/\*[\s\S]*?\*\//g, '')
    .replace(/\/\/.*$/gm, '')
    .replace(/'(?:[^'\\]|\\.)*'/g, "''")
    .replace(/"(?:[^"\\]|\\.)*"/g, '""')
    .replace(/`(?:[^`\\]|\\.)*`/g, '``')
}

const problems = []
const seen = new Set()
let scanned = 0
for (const d of frontendDirs()) {
  for (const f of walk(join(ROOT, d))) {
    const rel = relative(ROOT, f)
    if (rel.includes('/lib/actionMessage') || rel.includes('/lib/hintText')) continue
    scanned++
    // ① queries.ts / *.ts 里的字段映射不算：那是把后端字段搬进本地类型，
    //	  正是三级回退需要的一步。只管**渲染层**（.tsx）。
    if (!rel.endsWith('.tsx')) continue
    const body = strip(readFileSync(f, 'utf8'))
    for (const m of body.matchAll(/\b([A-Za-z_$][\w$]*(?:\??\.[\w$]+)*?)\??\.(msg|hint)\b(?!_key|Key|s\b)/g)) {
      // 归一化成 `对象.字段`，去掉可选链符号，便于写进基线
      const obj = m[1].replace(/\?/g, '')
      // ② 前端自己的 toast / 本地状态与后端响应无关
      if (/^(toast|msg\d*|local|state)$/i.test(obj)) continue
      const key = `${rel}:${obj}.${m[2]}`
      if (seen.has(key)) continue
      seen.add(key)
      if (!BASELINE.has(key)) problems.push(key)
    }
  }
}

const stale = [...BASELINE].filter((k) => !seen.has(k))

if (problems.length > 0) {
  console.error('✗ check-action-message-usage: 界面在直接渲染后端的 msg / hint 原句：\n')
  for (const p of problems) console.error(`    ${p}`)
  console.error(`
用三级回退助手，它会优先取 msg_key / hint_key：
    actionMessage(t, resp)   lib/actionMessage.ts
    hintText(t, resp)        lib/hintText.ts

⚠️ 后端补上 key 之后，前端若还在读 .msg，那次迁移一个字都不会生效 ——
   而两边看起来都"改过了"。`)
  process.exit(1)
}
if (stale.length > 0) {
  console.error('✗ check-action-message-usage: 基线里有已经不存在的条目，请删掉：\n')
  for (const s of stale) console.error(`    ${s}`)
  process.exit(1)
}
console.log(
  `✓ check-action-message-usage: ${scanned} 个前端文件，无新增（存量基线 ${BASELINE.size} 处）`,
)
