#!/usr/bin/env node
/**
 * 守卫：品牌亮度只能有**一个**真值。
 *
 * # 为什么
 *
 * 同一个数存在两处：
 *   `packages/design/src/tokens.css` 的 `--ops-brand-l-light/dark`  ← 界面真正用的
 *   `packages/design/src/brand.ts` 的 `DEFAULT_L`                    ← 校验与单测用的
 *
 * `brand.ts` 里写着「与 tokens.css 的 --ops-brand-l 必须一致」，
 * 但**没有任何东西保证它** —— 而这类注释恰恰最靠不住。
 *
 * 🔴 实测已经分叉过：tokens.css 被改成 `dark: 0.59`，落在文件自己
 *	记着的「0.58 死区」里（白字 4.41、深色字 4.36，两头都掉出 4.5:1），
 *	于是深色主题下「主按钮白字 3.77/4.24」双双不达标（OPSCMDB-081）。
 *
 * ⚠️ 单测为什么没拦住：`brand.test.ts` 调的是 `checkBrand({c, h}, theme)`
 *	—— **不传 l**，用的是 `brand.ts` 自己的默认值。
 *	它验的是那张表算得对不对，不是界面真正在用的值。
 *	一个只校验自己那份副本的测试，永远发现不了副本与真值的分叉。
 *
 * # 判据
 *
 * 从两个文件里各抽出亮度值，逐档比对。不相等即报。
 */
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { ROOT } from './lib/products.mjs'

const cssPath = join(ROOT, 'packages/design/src/tokens.css')
const tsPath = join(ROOT, 'packages/design/src/brand.ts')
const css = readFileSync(cssPath, 'utf8')
const ts = readFileSync(tsPath, 'utf8')

const cssVal = (k) => {
  const m = new RegExp(`--ops-brand-${k}:\\s*([0-9.]+)`).exec(css)
  return m ? Number(m[1]) : null
}
const tsBlock = /DEFAULT_L\s*=\s*\{([^}]*)\}/.exec(ts)
const tsVal = (k) => {
  if (!tsBlock) return null
  const m = new RegExp(`${k}:\\s*([0-9.]+)`).exec(tsBlock[1])
  return m ? Number(m[1]) : null
}

const problems = []
// ⚠️ 取不到值也要报：正则失配时静默通过，等于这个守卫不存在
for (const [cssKey, tsKey] of [
  ['l-light', 'light'],
  ['l-dark', 'dark'],
]) {
  const a = cssVal(cssKey)
  const b = tsVal(tsKey)
  if (a === null) problems.push(`tokens.css 里读不到 --ops-brand-${cssKey}（正则失配？）`)
  else if (b === null) problems.push(`brand.ts 的 DEFAULT_L 里读不到 ${tsKey}（正则失配？）`)
  else if (a !== b) problems.push(`${tsKey}: tokens.css=${a} vs brand.ts=${b}`)
}

// 死区检查：0.58 附近白字与深色字都掉出 4.5，默认值必须避开
for (const [cssKey, tsKey] of [
  ['l-light', 'light'],
  ['l-dark', 'dark'],
]) {
  const v = cssVal(cssKey)
  if (v !== null && v > 0.565 && v < 0.605) {
    problems.push(
      `${tsKey}=${v} 落在死区（0.57–0.60）：白字和深色字都掉出 4.5:1，主按钮与品牌色文字会同时不达标`,
    )
  }
}

if (problems.length > 0) {
  console.error('✗ check-brand-l-sync: 品牌亮度出问题了：\n')
  for (const p of problems) console.error(`    ${p}`)
  console.error(`
这个数只能有一个真值。改动时两处一起改，并**重新实测对比度**
（用 canvas 让浏览器转色，不要用正则从 oklch 串里抓数字 —— 那会算出恒为 1.00 的假比率）。

⚠️ brand.test.ts 拦不住这类分叉：它调 checkBrand({c,h}) 不传 l，
   验的是 brand.ts 自己那份副本，不是界面真正在用的值。`)
  process.exit(1)
}
console.log(
  `✓ check-brand-l-sync: 品牌亮度两处一致（light=${cssVal('l-light')} dark=${cssVal('l-dark')}），且避开死区`,
)
