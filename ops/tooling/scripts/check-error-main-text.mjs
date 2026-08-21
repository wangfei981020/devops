#!/usr/bin/env node
/**
 * 错误提示不能只显示技术描述。
 *
 * 正确的是两行：**主文案**（t(messageKey, params)）+ 小字 detail。
 * 写成 `toErrorInfo(e).detail` 单独一行，用户就只看到
 * "POST /api/environments → 409" —— 因为 detail **总是有值**。
 * 后端精心给的 "环境「PROD」已存在" / "还有 18 条在用" 全被吃掉。
 *
 * 🔴 这个坑一个文件里能同时存在对错两种写法：实测 basic/index.tsx 的
 *	删除路径写对了（注释里还讲了理由），创建路径写反了 ——
 *	知道这个坑的人只修了当时踩到的那一处。所以必须机器来盯。
 *
 * 判据：一个 <Banner tone="bad"> … </Banner> 块里出现了 .detail，
 * 就必须同时出现 messageKey（或整块用 <MutationError> 组件）。
 */
import { readFileSync, readdirSync, statSync } from 'node:fs'
import { resolve, join, relative } from 'node:path'

const root = resolve(process.argv[2] ?? '.')
const bases = ['ops-cmdb/frontend/src', 'ops-alert/frontend/src', 'ops-video-manager/frontend/src']

function walk(d, out = []) {
  for (const e of readdirSync(d)) {
    const p = join(d, e)
    if (statSync(p).isDirectory()) walk(p, out)
    else if (p.endsWith('.tsx') && !p.endsWith('.test.tsx')) out.push(p)
  }
  return out
}

const bad = []
for (const base of bases) {
  let files
  try { files = walk(resolve(root, base)) } catch { continue }
  for (const f of files) {
    const src = readFileSync(f, 'utf8')
    // 🔴 不只 Banner。
    //
    //	错误也常直接渲染在一个 <span className="…text-danger"> 里（按钮旁的小字）。
    //	实测域名页的同步失败就是这样：只显示 detail，而 detail 是**技术描述位** ——
    //	动作类接口把后端那句中文放在那儿，英文界面下就露出中文。
    //	只盯 Banner 的话这一类全漏。
    const re = /<Banner\s+tone="bad"[\s\S]*?<\/Banner>|<span[^>]*text-danger[^>]*>[\s\S]{0,320}?<\/span>/g
    for (const m of src.matchAll(re)) {
      const block = m[0]
      if (!/\.detail\b/.test(block)) continue
      if (/messageKey/.test(block)) continue
      // 用固定 key 写死一句主文案（t('xxx:failed')）也算表过态 ——
      // 不如 messageKey 精确（丢了后端给的具体原因），但至少有一句人话。
      // 这里只挡"连一句人话都没有、用户只看到 HTTP 状态码"的那一档。
      if (/\bt\(/.test(block)) continue
      // 主文案未必来自 t()：有的接口直接给一句人话（d.error），detail 只是补充。
      // 判据收紧成"detail 是这个块里**唯一**的动态内容"——
      // 只要还有别的 {…} 在渲染，就说明用户能看到 HTTP 状态码之外的东西。
      const exprs = block.match(/\{[^{}]*\}/g) ?? []
      const meaningful = exprs.filter((e) => !/^\{\s*['"`]/.test(e) && !/className|style/.test(e))
      if (meaningful.some((e) => !/\.detail\b/.test(e))) continue
      const line = src.slice(0, m.index).split('\n').length
      bad.push(`${relative(root, f)}:${line}`)
    }
  }
}

if (bad.length > 0) {
  console.error(`✗ 有 ${bad.length} 处错误提示**只有**技术描述，一句人话都没有：\n`)
  for (const b of bad) console.error(`  ${b}`)
  console.error('\n改成 <MutationError error={x.error} toInfo={toErrorInfo} t={t} />（@ops/ui），')
  console.error('它会渲染「主文案 + 小字 detail」两行。需要兜底文案时传 fallbackKey。')
  process.exit(1)
}
console.log('✓ 错误提示都带了主文案')
