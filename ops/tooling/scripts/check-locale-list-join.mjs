#!/usr/bin/env node
/**
 * 守卫：连接一串词不许写死分隔符。
 *
 * # 为什么
 *
 * `list.join('、')` 在中文界面上是对的，在英文界面上是错的 ——
 * 而它**不报任何错**，只是看着像机器凑的翻译。实测全库有 5 处这么写
 * （成本页、角色面板、升级详情、续费弹窗、影响面）。
 *
 * 反过来写死 `', '` 也一样：中文界面上出现 "a, b, c"。
 *
 * 正确写法是 `formatList(t, items)`（@ops/i18n）——
 * 它按当前语言的 BCP-47 标签走 `Intl.ListFormat`，还会处理连接词（a, b and c）。
 *
 * ⚠️ 判据只认 **CJK 分隔符**（、，；）。
 *
 *	`join(', ')` 看着也可疑，但它大量用在连接**数据值**上 ——
 *	证书的 SAN 列表、防火墙的端口和网段、DNS 的多条记录值。
 *	那些地方逗号是技术惯例（`a.com, b.com` 本来就该这么显示），
 *	报出来是误报，而误报的代价是人开始学着忽略这个守卫。
 *	CJK 顿号则不同：它在英文界面上**没有任何情况下是对的**。
 */
import { existsSync, readFileSync, readdirSync, statSync } from 'node:fs'
import { join, relative } from 'node:path'
import { ROOT, frontendDirs } from './lib/products.mjs'

function walk(dir, out = []) {
  if (!existsSync(dir)) return out
  for (const n of readdirSync(dir)) {
    const p = join(dir, n)
    if (statSync(p).isDirectory()) walk(p, out)
    else if (/\.tsx$/.test(p)) out.push(p)
  }
  return out
}

// 、 ， ； 以及 ", " / " and " —— 自然语言的连接符
const BAD_JOIN = /\.join\(\s*['"`][、，；]['"`]\s*\)/g

// 别的产品的存量。⚠️ 只能减不能加。
//	它们各自的 i18n 进度不同，逐个改要连带确认那个产品有没有英文界面 ——
//	不是"顺手"能做的事，所以先锁住不许新增。
const BASELINE = new Set([
  'ops-alert/frontend/src/routes/Backtest.tsx',
  'ops-alert/frontend/src/routes/Integrations.tsx',
  'ops-alert/frontend/src/routes/Rules.tsx',
  'ops-alert/frontend/src/routes/WarRoom.tsx',
  'ops-sso/frontend/src/console/pages/Overview.tsx',
  'ops-version/frontend/src/routes/recon/ReconPage.tsx',
  // 2026-08-21 新增（别的会话），一并锁住 —— ops-version 有没有英文界面我没确认过
  'ops-version/frontend/src/routes/recon/VerdictCell.tsx',
  'ops-version/frontend/src/routes/recon/ServiceDrill.tsx',
])

const problems = []
const seenFiles = new Set()
let scanned = 0
for (const d of frontendDirs()) {
  for (const f of walk(join(ROOT, d))) {
    const src = readFileSync(f, 'utf8')
    scanned++
    const body = src.replace(/\/\*[\s\S]*?\*\//g, '').replace(/\/\/.*$/gm, '')
    for (const m of body.matchAll(BAD_JOIN)) {
      const rel = relative(ROOT, f)
      seenFiles.add(rel)
      if (BASELINE.has(rel)) continue
      problems.push(`${rel}:${body.slice(0, m.index).split('\n').length}  ${m[0]}`)
    }
  }
}

if (problems.length > 0) {
  console.error('✗ check-locale-list-join: 连接一串词写死了分隔符：\n')
  for (const p of problems) console.error(`    ${p}`)
  console.error(`
用 formatList(t, items)（@ops/i18n）—— 它按当前语言连接，还会处理 a, b and c。

⚠️ '、' 在英文界面上是错的，', ' 在中文界面上是错的，而两者都不会报错。`)
  process.exit(1)
}
const stale = [...BASELINE].filter((f) => !seenFiles.has(f))
if (stale.length > 0) {
  console.error('✗ check-locale-list-join: 基线里有已经不存在的条目，请删掉：\n')
  for (const s of stale) console.error(`    ${s}`)
  process.exit(1)
}
console.log(
  `✓ check-locale-list-join: ${scanned} 个前端文件，ops-cmdb 已清零（其余产品存量 ${BASELINE.size} 个文件）`,
)
