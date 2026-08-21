#!/usr/bin/env node
/**
 * 守卫：纯图标按钮必须有 `aria-label` 和 `title`。
 *
 * # 为什么
 *
 * 一个只有图标的按钮，**唯一能说明自己是什么的途径**就是这两个属性：
 *
 *   aria-label  给读屏软件（没有它，这个按钮对读屏用户等于不存在）
 *   title       给鼠标用户（hover 才知道是什么）
 *
 * 少了 aria-label：可访问性直接归零，而且**测试里也点不到它**
 * （按名字定位的选择器全失效）—— 这是那种不报错、只是"某些人用不了"的缺陷。
 *
 * 少了 title：桌面用户只能靠猜。
 *
 * # ⚠️ 触屏上没有 hover
 *
 * 所以 title 是**补救**不是解决方案。真正的规矩是：
 * 只有语义公认的图标才配做纯图标按钮（下载/同步/刷新/历史/日志），
 * 语义特殊的必须留文字。那一条机器判不了，写在 Button 的注释里由人守。
 *
 * # 判据
 *
 * 找 `iconOnly`（或 `aria-label` 缺失的 `<button>` 里只有一个 `<Icon />`）的地方，
 * 检查同一个标签上有没有 aria-label 和 title。
 */

import { existsSync, readFileSync, readdirSync, statSync } from 'node:fs'
import { join, relative } from 'node:path'
import { ROOT, products } from './lib/products.mjs'

function walk(dir, out = []) {
  if (!existsSync(dir)) return out
  for (const name of readdirSync(dir)) {
    if (name === 'node_modules' || name === 'dist') continue
    const p = join(dir, name)
    if (statSync(p).isDirectory()) walk(p, out)
    else if (/\.tsx$/.test(name)) out.push(p)
  }
  return out
}

const scopes = process.argv.slice(2)
const ALL = products()
const TARGETS = scopes.length ? ALL.filter((p) => scopes.includes(p)) : ALL
if (scopes.length && TARGETS.length === 0) {
  console.error(`✗ check-icon-buttons: 没有匹配的产品（传入 ${scopes.join(', ')}）`)
  process.exit(1)
}

/**
 * 从标签名之后扫出属性串，扫到深度为 0 的 `>` 为止。
 *
 * ⚠️ 花括号深度必须记：JSX 属性值里的 `>`（`icon={<X />}`、`onClick={() => f()}`）
 * 都在花括号内，它们不是标签结束。
 */
function attrsOf(src, from) {
  let depth = 0
  for (let i = from; i < src.length && i < from + 4000; i++) {
    const ch = src[i]
    if (ch === '{') depth++
    else if (ch === '}') depth--
    else if (ch === '>' && depth === 0) return src.slice(from, i)
  }
  return null // 没扫到结束（超长或写法异常）—— 不下结论
}

const problems = []
let checked = 0

for (const prod of TARGETS) {
  for (const dir of [join(ROOT, prod, 'frontend/src'), join(ROOT, 'packages/ui/src')]) {
    for (const f of walk(dir)) {
      const src = readFileSync(f, 'utf8')
      if (!src.includes('iconOnly')) continue
      // 逐个 <Button ... iconOnly ... /> 检查
      //
      // 🔴 不能用 `[^>]*?` 去截标签 —— JSX 的属性值里有 `>`：
      //	  icon={<Download className="size-3.5" />}
      //	截到第一个 `>` 就停了，后面的 aria-label 根本没看到，
      //	于是一个**写对了的**按钮被报成缺属性（我第一版就这样，当场误报）。
      //
      //	正确做法：从标签名往后扫，记花括号深度，只有深度为 0 的 `>` 才是标签结束。
      for (const m of src.matchAll(/<(\w*Button)\b/g)) {
        const attrs = attrsOf(src, m.index + m[0].length)
        if (attrs === null || !/\biconOnly\b/.test(attrs)) continue
        checked++
        const missing = []
        if (!/\baria-label\s*=/.test(attrs)) missing.push('aria-label')
        if (!/\btitle\s*=/.test(attrs)) missing.push('title')
        if (missing.length === 0) continue
        problems.push({
          file: relative(ROOT, f),
          line: src.slice(0, m.index).split('\n').length,
          missing,
        })
      }
    }
  }
}

if (problems.length > 0) {
  console.error('✗ check-icon-buttons: 纯图标按钮缺少说明自己是什么的属性：\n')
  for (const p of problems) {
    console.error(`    ${p.file}:${p.line}  缺 ${p.missing.join(' 和 ')}`)
  }
  console.error(`
一个只有图标的按钮，**唯一能说明自己是什么的途径**就是这两个属性：

  aria-label  给读屏软件 —— 没有它，这个按钮对读屏用户等于不存在，
              而且测试里也点不到（按名字定位的选择器全失效）
  title       给鼠标用户 —— hover 才知道是什么

⚠️ 触屏上没有 hover，所以 title 是补救不是解决方案。
真正的规矩是：只有语义公认的图标才配做纯图标按钮（下载/同步/刷新/历史/日志），
语义特殊的必须留文字。那一条机器判不了，见 Button 的 iconOnly 注释。`)
  process.exit(1)
}

console.log(
  `✓ check-icon-buttons: ${checked} 个纯图标按钮都有 aria-label 和 title` +
    `（范围 ${scopes.length ? scopes.join(', ') : '全部产品'}）`,
)
