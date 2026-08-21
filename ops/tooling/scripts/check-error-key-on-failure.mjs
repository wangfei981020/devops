#!/usr/bin/env node
/**
 * 守卫：非 2xx 响应里的 `*_key` 必须配 `message_key`，否则前端根本看不到它。
 *
 * # 为什么
 *
 * 前端的 `normalizeError` 判断"这是不是一个结构化错误"看的是 `code` / `message_key`。
 * 只塞一个 `error_key` 的话它认不出来，会退到**按状态码兜底**：
 *
 *	后端返回 403 + {"error_key":"error.secretContentRefused", ...}
 *	界面显示 「The upstream returned an error … → 502」这类通用句子
 *
 * 🔴 **迁移看起来做了，实际一个字都没生效** —— 而且两边都"改过了"。
 *	实测撞到两次（OPSCMDB-054 迁移过程中：域名拨测 502、Secret 拒绝 403）。
 *
 * # 判据
 *
 * 非 2xx 的 `c.JSON(...)` / `c.AbortWithStatusJSON(...)` 里
 * 出现任何 `"*_key"` 但没有 `"message_key"` → 报。
 *
 * 正确写法是 `httpx.FailKeyWith(c, code, key, params, extra)`：
 * 它一定会发 `code` + `message_key`，额外字段放 extra。
 *
 * ⚠️ 200 + `{ok:false}` 的动作类接口**不在此列** —— 那种形态前端是
 *	显式读 `error_key` / `msg_key` 的（见 lib/actionMessage.ts、lib/hintText.ts），
 *	不走 normalizeError。这也是本产品的动作类接口约定。
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

// ⚠️ 两种写法都要认：字面量状态码和 http.StatusXxx 常量。
//	只认一种的话会静默漏掉一半 —— 这个仓库为此栽过四次。
const OK2XX = /^(?:2\d\d|http\.Status(?:OK|Created|Accepted|NoContent|NonAuthoritativeInfo|ResetContent|PartialContent))$/

/**
 * 从 `c.JSON(` 的左括号配对到它的右括号，返回调用的完整实参串。
 *
 * 🔴 不能用 `[\s\S]{0,800}?\n\t*\}\)` 这种正则收尾：**单行**写法
 *	`c.JSON(400, gin.H{"error": x})` 不在那里终止，匹配会一路吃进后面的代码，
 *	把下一段里的 `_key` 算成这一处的 —— 实测三条误报全是这么来的。
 */
function callArgs(src, openParen) {
  let i = openParen + 1
  let depth = 1
  while (depth > 0 && i < src.length) {
    const ch = src[i]
    if (ch === '(') depth++
    else if (ch === ')') depth--
    else if (ch === '"' || ch === '`') {
      // 跳过字符串，里面的括号不算
      const q = ch
      i++
      while (i < src.length && src[i] !== q) {
        if (src[i] === '\\' && q === '"') i++
        i++
      }
    }
    i++
  }
  return src.slice(openParen + 1, i - 1)
}

const problems = []
let scanned = 0
for (const d of backendDirs()) {
  for (const f of walkGo(join(ROOT, d))) {
    const src = readFileSync(f, 'utf8')
    scanned++
    for (const m of src.matchAll(/c\.(?:JSON|AbortWithStatusJSON)\(/g)) {
      const args = callArgs(src, m.index + m[0].length - 1)
      const status = /^\s*(\d{3}|http\.Status\w+)\s*,/.exec(args)?.[1]
      if (!status || OK2XX.test(status)) continue
      if (!/"\w*_key"/.test(args)) continue
      if (/"message_key"/.test(args)) continue
      problems.push(
        `${relative(ROOT, f)}:${src.slice(0, m.index).split('\n').length}  ${status}  ${args.trim().slice(0, 80).replace(/\s+/g, ' ')}`,
      )
    }
  }
}

if (problems.length > 0) {
  console.error('✗ check-error-key-on-failure: 非 2xx 响应里的 *_key 前端收不到：\n')
  for (const p of problems) console.error(`    ${p}`)
  console.error(`
改用 httpx.FailKeyWith(c, code, key, params, extra) —— 它一定会发 code + message_key。

⚠️ 只塞 error_key 的话，前端认不出这是结构化错误，会按状态码兜底显示通用句子。
   迁移看起来做了，实际一个字都没生效，而两边都"改过了"。`)
  process.exit(1)
}
console.log(`✓ check-error-key-on-failure: ${scanned} 个后端文件，非 2xx 的 *_key 都配了 message_key`)
