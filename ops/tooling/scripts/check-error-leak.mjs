#!/usr/bin/env node
/**
 * 守卫：**新增**的接口不要把原始 error 直接发给前端。
 *
 * # 为什么
 *
 * 生产实测：告警页环境筛选器报错，完整 SQL 错误被原样发到前端并渲染进
 * DOM 的 title：
 *
 *   Error 3065 (HY000): Expression #1 of ORDER BY clause ... references column
 *   'ops_cmdb.obs_endpoints.env' which is not in SELECT list
 *
 * 一行泄露了**库名 + 表名 + 列名**（验收会话 NEW-1）。企业版不该这样。
 *
 * 🔴 必须在后端收口。前端脱敏是假的：值已经在 HTTP 响应里，
 * F12 一看、或拿 token 直接 curl 就是明文
 * （同 CMDB-033/034 两个 P0 的教训，见 handlers/mask.go）。
 *
 * # 判据
 *
 * 后端源码里出现 `err.Error()` 且同一行在往 HTTP 响应里写（c.JSON / httpx.Fail
 * 等）→ 报。正确写法是 `SafeErr("哪一步", err)`。
 *
 * # ⚠️ 为什么只挡新增
 *
 * 存量有 369 处。一次性全改的风险大于收益：
 * 其中很多是**该原样透出**的（Prometheus 的 PromQL 语法错误、
 * 厂商 API 的业务报错），糊掉反而让人没法自助处置。
 * 所以现存的进 BASELINE 挂账，**新增的直接拦**。
 *
 * ⚠️ BASELINE 只记数量不记清单：记清单的话，任何一次重构挪动行号
 * 都要更新几百行名单，那种名单三天就烂了。数量变多就拦。
 */
import { existsSync, readFileSync, readdirSync, statSync } from 'node:fs'
import { join, relative } from 'node:path'
import { ROOT, products } from './lib/products.mjs'

/**
 * 存量基线：产品 → 允许的条数。
 * 🔴 只允许**变小**。修一处就把数字改小一点，别往上加。
 */
const BASELINE = new Map([
  // 2026-08-20 实测值。⚠️ 不要凭 grep 估 —— 我最初按单一写法数出 369，
  //	而守卫按四种响应写法扫出 427。基线必须来自守卫自己的口径。
  ['ops-cmdb', 383],
  // MCP 转发内部调用的错误，读者是 AI 不是终端用户；而且它需要原始错误
  // 才能判断下一步。⚠️ 但仍要有基线，防止顺手多写几处
  // ops-alert 的存量。⚠️ 这个守卫的定位是**挡新增**，
  //	不是逼别的产品一次性重构 —— 那样它会被整体绕过。
  ['ops-alert', 14],
  ['ops-video-manager', 1],
])

function walk(dir, out = []) {
  if (!existsSync(dir)) return out
  for (const name of readdirSync(dir)) {
    if (name === 'node_modules' || name === 'vendor') continue
    const p = join(dir, name)
    if (statSync(p).isDirectory()) walk(p, out)
    else if (p.endsWith('.go') && !p.endsWith('_test.go')) out.push(p)
  }
  return out
}

// 往 HTTP 响应里写的几种写法
const RESP = /(c\.JSON|c\.String|c\.Data|httpx\.Fail|httpx\.Error|gin\.H\{)/

const problems = []
for (const prod of products()) {
  const beDir = join(ROOT, prod, 'backend')
  if (!existsSync(beDir)) continue

  let n = 0
  const hits = []
  for (const f of walk(beDir)) {
    const lines = readFileSync(f, 'utf8').split('\n')
    lines.forEach((l, i) => {
      if (!l.includes('err.Error()') && !/\berr\b.*\.Error\(\)/.test(l)) return
      if (!RESP.test(l)) return
      n++
      hits.push(`${relative(ROOT, f)}:${i + 1}`)
    })
  }

  const base = BASELINE.get(prod)
  if (base === undefined) {
    // 没登记基线的产品：一条都不许有（新产品从干净开始）
    if (n > 0) {
      problems.push(
        `${prod}: ${n} 处把原始 error 发给前端，而这个产品没有历史基线 —— 新产品应当从零开始。\n` +
          hits.slice(0, 5).map((x) => `      ${x}`).join('\n'),
      )
    }
    continue
  }
  if (n > base) {
    problems.push(
      `${prod}: ${n} 处把原始 error 直接发给前端，超过基线 ${base}（新增 ${n - base} 处）。\n` +
        `    正确写法：SafeErr("哪一步", err) —— 见 backend/handlers/errsafe.go\n` +
        `    最近的几处：\n` +
        hits.slice(-5).map((x) => `      ${x}`).join('\n'),
    )
  } else if (n < base) {
    problems.push(
      `${prod}: 存量已降到 ${n} 处（基线还写着 ${base}）。把基线改成 ${n}，\n` +
        `    否则这个差值就是"白送的额度"——下次新增 ${base - n} 处不会被拦住。`,
    )
  }
}

if (problems.length > 0) {
  console.error('✗ check-error-leak：\n')
  for (const m of problems) console.error(`  - ${m}\n`)
  process.exit(1)
}
console.log(`✓ check-error-leak：原始 error 外发处数未超基线（${[...BASELINE].map(([p, n]) => `${p}=${n}`).join(', ')}）`)
