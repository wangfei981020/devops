#!/usr/bin/env node
/**
 * 产品不许自己实现侧栏外壳，必须用 `@ops/ui` 的 AppShell。
 *
 * # 这个守卫存在的理由
 *
 * 三个产品曾各写各的外壳：ops-cmdb 351 行、ops-alert 394 行、ops-sso 87 行。
 * 后果不只是"长得不一样"——**能力也不一样**：
 * ops-alert 那份是从 cmdb 抄过去再改的，抄的过程中漏掉了权限过滤，
 * 菜单不按权限收，用户点进去才拿到 403。
 *
 * ⚠️ 关键在于：约定**早就写在** CONVENTIONS §2.7.1 里了，照样没拦住。
 * 复制粘贴出来的第二份代码，从复制那一刻起就开始漂移，
 * 而没有人会在改第二份的时候回头对照文档。
 *
 * 所以拦截点不能是文档，只能是「根本不存在第二份」。
 *
 * # 判据
 *
 * 找**导出应用外壳的文件**（导出名为 AppShell / Shell 的组件），
 * 要求它从 @ops/ui 引 AppShell。
 *
 * ⚠️ 第一版判据是「文件里出现 <aside>」——立刻误报了三个页面：
 * 详情面板、模拟器面板、登录页的左半屏都合法地用了 aside，它是侧栏的语义标签，
 * 但"页面内的侧边面板"和"应用外壳"是两回事。
 * 一个天天误报的守卫会被人直接关掉，那比没有守卫更糟。
 */
import { readFileSync, readdirSync, statSync } from 'node:fs'
import { join, relative } from 'node:path'
import { ROOT, frontendDirs } from './lib/products.mjs'

/**
 * 允许自己写 aside 的文件。
 *
 * ⚠️ 加白名单要写清楚理由。「先加上让它过」是这个机制失效的第一步 ——
 * 白名单一旦变成垃圾桶，守卫就只剩形式了。
 */
const ALLOW = [
  // 门户是磁贴网格 + 顶栏导航（只有 4 项），刻意不用侧栏；
  // 控制台那半边才用 AppShell。理由见 shared/Shell.tsx 顶部注释。
  'ops-sso/frontend/src/shared/Shell.tsx',
]

function walk(dir, out = []) {
  for (const name of readdirSync(dir)) {
    const p = join(dir, name)
    if (statSync(p).isDirectory()) walk(p, out)
    else if (/\.tsx$/.test(name)) out.push(p)
  }
  return out
}

const problems = []

for (const d of frontendDirs()) {
  const abs = join(ROOT, d)
  for (const file of walk(abs)) {
    const rel = relative(ROOT, file)
    if (ALLOW.includes(rel)) continue
    const src = readFileSync(file, 'utf8')
    // 只看导出应用外壳的文件
    if (!/export function (AppShell|Shell)\b/.test(src)) continue
    // 从 @ops/ui 引了 AppShell 的，说明是薄封装，放行
    if (/from '@ops\/ui'/.test(src) && /AppShell/.test(src)) continue
    problems.push(rel)
  }
}

if (problems.length > 0) {
  console.error('✗ 这些文件自己实现了侧栏，没有用 @ops/ui 的 AppShell：\n')
  for (const p of problems) console.error(`  - ${p}`)
  console.error(
    '\n各产品自己写外壳的代价不是"不好看"，是能力会分叉：\n' +
      '  ops-alert 抄过去时漏掉了权限过滤，菜单不按权限收，用户点进去才 403。\n' +
      '  而这类漂移靠文档拦不住 —— 约定早就写在 CONVENTIONS §2.7.1 里。\n\n' +
      '产品只需要提供三样：NavGroup[] 菜单、can(perm) 判据、品牌名与版本号。\n' +
      '样板见 ops-cmdb/frontend/src/layouts/AppShell.tsx（66 行）。\n',
  )
  process.exit(1)
}
console.log(`✓ 应用外壳：${frontendDirs().length} 个前端均使用 @ops/ui 的 AppShell`)
