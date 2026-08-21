#!/usr/bin/env node
/**
 * 守卫：SQL 里写了 `tenant_id = ?` 就必须用**租户作用域**的句柄执行。
 *
 * # 为什么
 *
 * `sc := h.Store.Tenant(ctx)` 返回的句柄会**自动注入 tenant_id**；
 * 裸的 `h.DB` / `db` 不会。SQL 里留着那个占位符却没人填它，
 * 参数个数就永远差一个：
 *
 *	h.DB.Exec(`UPDATE … WHERE tenant_id = ? AND id=?`, a, b, c, id)
 *	→ sql: expected 5 arguments, got 4
 *
 * 🔴 而这个失败常常被包在 `200 + {ok:false}` 里 ——
 *	界面上显示的是"没改成"，读起来像**这一次**没成功，
 *	而不是**这个功能从来就没 work 过**（OPSCMDB-085：GKE 手动覆盖）。
 *
 * ⚠️ 同一个文件里两种写法并存才是根因：隔十几行的 ClearOverride 用的是 `sc.Exec`。
 *	所以判据不能只看"这一处对不对"，要看**有没有混用**。
 *
 * # 判据（锚在形状上）
 *
 * 调用形如 `<recv>.Exec|Query|QueryRow(` 且 SQL 含 `tenant_id\s*=\s*?`，
 * 而接收者不是租户句柄（约定名 `sc` / `tx`）→ 报。
 *
 * ⚠️ 不比参数个数：那要解析 Go 表达式，容易误报。
 *	"用错句柄"这个形状本身就足够判定，也更好懂。
 *
 * ⚠️ **租户机制自己的实现**要排除：`internal/store`（注入逻辑本体）、
 *	`middleware/tenant.go`（解析租户）、`testutil`（造数据）——
 *	它们本来就该显式传 tenant_id，报它们是误报。
 *	按**路径**排除而不是按变量名，路径是这几个文件的稳定特征。
 *
 * ⚠️ **只管 ops-cmdb**。判据锚在 ops-cmdb 的句柄约定（`sc = Store.Tenant(ctx)`）上；
 *	ops-sso 用的是另一套（到处是 `q`），它的 `q` 是不是注入 tenant 我没有验证过。
 *	把没验证过的产品也纳进来，只会产出 80 条我判不了的告警 ——
 *	而那种输出没人会认真看。要扩产品，先把那个产品的约定确认清楚再加。
 */
import { existsSync, readFileSync, readdirSync, statSync } from 'node:fs'
import { join, relative } from 'node:path'
import { ROOT, backendDirs } from './lib/products.mjs'

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

/** 从左括号配对到右括号，跳过字符串里的括号 */
function callArgs(src, open) {
  let i = open + 1
  let depth = 1
  while (depth > 0 && i < src.length) {
    const ch = src[i]
    if (ch === '(') depth++
    else if (ch === ')') depth--
    else if (ch === '`') {
      i++
      while (i < src.length && src[i] !== '`') i++
    } else if (ch === '"') {
      i++
      while (i < src.length && src[i] !== '"') {
        if (src[i] === '\\') i++
        i++
      }
    }
    i++
  }
  return src.slice(open + 1, i - 1)
}

// 约定的租户作用域句柄名。⚠️ 新增别名要同时加进来，否则守卫会误报。
const SCOPED = /^(sc|tx|scoped)$/
// 租户机制自身的实现：注入逻辑、租户解析中间件、测试工具
const SELF = /(internal\/store\/|middleware\/tenant\.go|internal\/testutil\/)/

const problems = []
let scanned = 0
for (const d of backendDirs().filter((d) => d.startsWith('ops-cmdb/'))) {
  for (const f of walkGo(join(ROOT, d))) {
    const rel = relative(ROOT, f)
    const src = readFileSync(f, 'utf8')
    scanned++
    for (const m of src.matchAll(/\b([\w.]+)\.(Exec|Query|QueryRow)\(/g)) {
      if (SELF.test(rel)) continue
      const recv = m[1]
      const last = recv.split('.').pop()
      if (SCOPED.test(last)) continue
      const args = callArgs(src, m.index + m[0].length - 1)
      if (!/tenant_id\s*=\s*\?/.test(args)) continue
      problems.push(
        `${rel}:${src.slice(0, m.index).split('\n').length}  ${recv}.${m[2]}(… tenant_id = ? …)`,
      )
    }
  }
}

if (problems.length > 0) {
  console.error('✗ check-tenant-scoped-sql: 带 tenant_id 占位符的 SQL 用了非租户句柄：\n')
  for (const p of problems) console.error(`    ${p}`)
  console.error(`
用 sc := h.Store.Tenant(c.Request.Context()) 拿到的句柄执行 —— 它会注入 tenant_id。
裸的 h.DB 不会，参数个数永远差一个，而失败常被包在 200 + {ok:false} 里，
界面上看起来像"这次没改成"，不像"这功能从来没 work 过"。`)
  process.exit(1)
}
console.log(
  `✓ check-tenant-scoped-sql: ${scanned} 个后端文件（ops-cmdb），带 tenant_id 的 SQL 都走租户句柄`,
)
