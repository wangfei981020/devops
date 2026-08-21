#!/usr/bin/env node
/**
 * 每个写接口都必须登记到 auditRoutes。
 *
 * # 为什么要有构建期的这一道
 *
 * 后端**本来就有**启动自检（handlers/audit_routes.go 的 AuditRouteCheck），
 * 而且它一直在正确地报告问题：
 *
 *     WARN 有 6 条写接口未登记变更捕获
 *     WARN   未登记: POST /api/users
 *     WARN   未登记: POST /api/license
 *
 * 但它**只打日志、不阻断启动**。于是这几行 WARN 在生产日志里躺了很久，
 * 没有任何人看 —— 期间「谁开了账号」「谁激活了授权」一条审计都没有。
 *
 * ⚠️ 教训不是"自检写得不好"，是**一个不会让人停下来的检查等于没有检查**。
 * 判据没变，改的是它在什么时候说话：从"跑起来之后写进日志"挪到"构建时挡住你"。
 *
 * # 判据
 *
 * 扫后端 `r.POST/PUT/DELETE("/x")` 得到写路由，与 auditRoutes 里的 key 比对。
 * SKIP 里是**确认不该登记**的，每条写原因。
 */
import { existsSync, readFileSync, readdirSync, statSync } from 'node:fs'
import { join, relative } from 'node:path'
import { ROOT, products } from './lib/products.mjs'

/**
 * 不登记变更捕获的接口。与后端 AuditRouteCheck 里的跳过条件保持一致 ——
 * ⚠️ 两处分叉的话，这个守卫会开始报后端根本不认为是问题的东西。
 */
const SKIP = [
  { re: /^\/api\/login$/, why: '登录本身由 auth 侧记录，且失败的登录不该产生"变更"' },
  { re: /^\/api\/portal-auth$/, why: '运维平台免登录入口，同上' },
  { re: /^\/api\/logout$/, why: '退出登录不改任何业务数据' },
  { re: /^\/api\/mcp/, why: 'MCP 的写动作由被调用的那个工具各自登记' },
  { re: /^\/api\/audit-/, why: '回滚接口自己写审计（含"回滚的回滚"要用的快照）' },
  { re: /^\/api\/oidc\//, why: 'OIDC 协议端点，由客户端按协议调用，不是人的操作' },
]

function walk(dir, out = []) {
  if (!existsSync(dir)) return out
  for (const name of readdirSync(dir)) {
    if (name === 'node_modules') continue
    const p = join(dir, name)
    if (statSync(p).isDirectory()) walk(p, out)
    else if (p.endsWith('.go')) out.push(p)
  }
  return out
}

const problems = []
let checked = 0

for (const prod of products()) {
  const beDir = join(ROOT, prod, 'backend')
  if (!existsSync(beDir)) continue

  const files = walk(beDir)
  // auditRoutes 的 key 长这样： "POST /api/users": {...}
  const registered = new Set()
  for (const f of files) {
    for (const m of readFileSync(f, 'utf8').matchAll(
      /"(POST|PUT|DELETE) (\/api\/[^"]*)":\s*\{/g,
    )) {
      registered.add(`${m[1]} ${m[2]}`)
    }
  }
  // 这个产品没有审计路由表就不管它（ops-sso 等）
  if (registered.size === 0) continue

  const missing = []
  for (const f of files) {
    // 测试文件里注册的路由是假的，别当真
    if (f.endsWith('_test.go')) continue
    const src = readFileSync(f, 'utf8')
    for (const m of src.matchAll(/\br\.(POST|PUT|DELETE)\(\s*"([^"]+)"/g)) {
      const path = m[2].startsWith('/api') ? m[2] : `/api${m[2]}`
      if (SKIP.some((s) => s.re.test(path))) continue
      const key = `${m[1]} ${path}`
      checked++
      if (!registered.has(key)) missing.push({ key, file: relative(ROOT, f) })
    }
  }

  if (missing.length > 0) {
    const lines = [...new Set(missing.map((x) => x.key))].sort().map((k) => `      ${k}`)
    problems.push(
      `${prod}: ${lines.length} 条写接口没有登记变更捕获\n${lines.join('\n')}` +
        `\n    这些操作**不会留下审计**。登记在 backend/handlers/audit_routes.go 的 auditRoutes；` +
        `\n    确实不该记的（协议端点之类）加进本脚本的 SKIP 并写原因。`,
    )
  }
}

if (problems.length > 0) {
  console.error('✗ 审计覆盖检查未通过：\n')
  for (const m of problems) console.error(`  - ${m}\n`)
  process.exit(1)
}
console.log(`✓ 审计覆盖：${checked} 条写接口全部登记了变更捕获`)
