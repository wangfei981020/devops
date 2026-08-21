#!/usr/bin/env node
/**
 * 检查各产品的 AppShell 包装层有没有开启 responsive（窄屏折叠）。
 *
 * # 为什么需要这个守卫
 *
 * `@ops/ui` 的 AppShell 支持 drawer/rail/full 三种模式，但 `responsive`
 * **默认 false**（opt-in 是有意的：已上线产品要各自验证窄屏表现再开）。
 *
 * 代价是：**没开的产品会静默地不响应式**。ops-cmdb 就这样过了一个版本 ——
 * 能力做好了、构建进了 v0.109.0，只是没人打开这个开关，
 * 于是它在修复进度表上是「已修」，在生产上 390px 视口下侧栏照样占 55%
 * （OPSCMDB-031 P0-24）。
 *
 * ⚠️ 这个守卫**不判失败**：opt-in 是正当设计，没开不等于错。
 * 它只负责把"谁开了谁没开"打印出来 —— 让遗漏可见，而不是让它安静地留在那儿。
 * （本项目的教训：靠文档拦不住漂移，只有"看得见"拦得住。）
 */
import { readdirSync, readFileSync, existsSync } from 'node:fs'
import { join } from 'node:path'

const root = process.argv[2] || '.'
const products = readdirSync(root, { withFileTypes: true })
  .filter((d) => d.isDirectory() && d.name.startsWith('ops-'))
  .map((d) => d.name)

const rows = []
for (const p of products) {
  const f = join(root, p, 'frontend/src/layouts/AppShell.tsx')
  if (!existsSync(f)) continue
  const src = readFileSync(f, 'utf8')
  // 只认真正传给 SharedShell 的写法，注释里提到不算
  const body = src.replace(/\/\*[\s\S]*?\*\//g, '').replace(/\/\/.*$/gm, '')
  rows.push({ product: p, on: /\bresponsive\b/.test(body) })
}

if (rows.length === 0) {
  // ⚠️ 扫到 0 个必须报出来：解析不到和"全都合规"看起来一样
  console.error('✗ check-shell-responsive: 一个产品的 AppShell 都没扫到，判据可能失效')
  process.exit(1)
}

console.log('窄屏折叠（AppShell responsive）开启情况：')
for (const r of rows.sort((a, b) => a.product.localeCompare(b.product))) {
  console.log(`  ${r.on ? '✅' : '⬜'} ${r.product}${r.on ? '' : '  —— 未开启，窄屏下侧栏固定占位'}`)
}
const off = rows.filter((r) => !r.on)
if (off.length) {
  console.log(`\n  ${off.length}/${rows.length} 个产品未开启。opt-in 是有意设计，`)
  console.log('  但开启前需各自验证：页头按钮换行、顶栏用户区收起。')
}
