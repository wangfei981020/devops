#!/usr/bin/env node
/**
 * 守卫：给用户看的文案里，「A → B」这种导航指引必须指向**真实存在的菜单**。
 *
 * # 为什么
 *
 * 后端的错误信息和处置建议里大量写着「到「X → Y」去改」。
 * 菜单改过名之后这些文案不会跟着变 —— 而它们**看起来仍然很专业**，
 * 只是让人在界面上找一个不存在的入口。
 *
 * 真事（2026-08-19）：全项目 9 处写着「接入管理 → …」，
 * 而新版 ops-cmdb **根本没有「接入管理」这个菜单**（早改成了「管理」）。
 * 其中一条是我当天刚写的：体检报出「集群标签值配错了」之后，
 * 让人去「接入管理 → 集群」改 —— 用户照着找不到，回来问我是不是搞错了。
 *
 * 🔴 这比没有指引更糟：没有指引人会自己找；错的指引让人先浪费时间找，
 * 再怀疑是不是自己看漏了。
 *
 * # 判据
 *
 * 从 nav.ts + 语言包里取出真实的菜单**组名**与**项名**，
 * 再扫 Go 源码里所有 `「X → Y」` 形状的指引，X 必须是真实组名。
 *
 * ⚠️ 只校验第一段（组名）。二级项名各页写法不完全一致（有的带后缀、
 * 有的用了同义词），全都卡死会产生一堆改不完的假问题 ——
 * 而组名是最容易过时、也最容易一眼看出错的那一段。
 */

import { existsSync, readFileSync, readdirSync, statSync } from 'node:fs'
import { join, relative } from 'node:path'
import { ROOT, products } from './lib/products.mjs'

function walk(dir, re, out = []) {
  if (!existsSync(dir)) return out
  for (const name of readdirSync(dir)) {
    if (name === 'node_modules' || name === 'dist' || name === 'vendor') continue
    const p = join(dir, name)
    if (statSync(p).isDirectory()) walk(p, re, out)
    else if (re.test(name)) out.push(p)
  }
  return out
}

const scopes = process.argv.slice(2)
const ALL = products()
const TARGETS = scopes.length ? ALL.filter((p) => scopes.includes(p)) : ALL
if (scopes.length && TARGETS.length === 0) {
  console.error(`✗ check-nav-paths: 没有匹配的产品（传入 ${scopes.join(', ')}）`)
  process.exit(1)
}

const problems = []
const notCovered = []
let checked = 0

for (const prod of TARGETS) {
  const navJSON = join(ROOT, 'packages/i18n/locales/zh-CN/nav.json')
  const beDir = join(ROOT, prod, 'backend')
  if (!existsSync(navJSON) || !existsSync(beDir)) continue

  const nav = JSON.parse(readFileSync(navJSON, 'utf8'))
  const groups = new Set(Object.values(nav.group ?? {}))
  if (groups.size === 0) {
    notCovered.push(`${prod} 的 nav.json 里没有 group —— 本守卫无法校验`)
    continue
  }
  // 页面内的一级入口也算合法起点（比如「集群 → 集群」）
  for (const sec of Object.values(nav)) {
    if (sec && typeof sec === 'object') for (const v of Object.values(sec)) groups.add(v)
  }

  for (const f of walk(beDir, /\.go$/)) {
    if (f.endsWith('_test.go')) continue
    const src = readFileSync(f, 'utf8')
    // 🔴 只认**祈使式导航**：前面带「到/去/在/请/前往」才算指路。
    //
    //	不收紧的话误报淹没真问题 —— 第一版把
    //	「症状 → 原因 → 方案」「K8s → 云厂商」「VS → gateway → svc」
    //	这些**描述性箭头**全报了出来（17 条里 14 条是误报）。
    //	而守卫误报的代价不是烦人，是人开始学着忽略它。
    for (const m of src.matchAll(/(?:到|去|在|请|前往)\s*「([^」]{2,12}?)\s*→/g)) {
      checked++
      const head = m[1].trim()
      if (groups.has(head)) continue
      problems.push({
        file: relative(ROOT, f),
        line: src.slice(0, m.index).split('\n').length,
        head,
      })
    }
  }
}

if (problems.length > 0) {
  console.error('✗ check-nav-paths: 文案里指向了不存在的菜单：\n')
  for (const p of problems) console.error(`    ${p.file}:${p.line}  「${p.head} → …」`)
  console.error(`
这些是**给用户看的**导航指引。菜单改过名之后文案不会跟着变，
而它们看起来仍然很专业 —— 只是让人在界面上找一个不存在的入口。

🔴 比没有指引更糟：没有指引人会自己找；错的指引让人先浪费时间找，
再怀疑是不是自己看漏了。2026-08-19 撞过一次：9 处写着「接入管理 → …」，
而新版根本没有这个菜单，用户照着找不到回来问。

真实的一级菜单取自 packages/i18n/locales/zh-CN/nav.json 的 group。`)
  process.exit(1)
}

for (const m of notCovered) console.log(`⊘ check-nav-paths: ${m}`)
console.log(
  `✓ check-nav-paths: ${checked} 处导航指引都指向真实菜单` +
    `（范围 ${scopes.length ? scopes.join(', ') : '全部产品'}）`,
)
