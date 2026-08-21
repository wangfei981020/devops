#!/usr/bin/env node
/**
 * 守卫：写接口**收得下、前端一次都没发过**的字段。
 *
 * # 它抓的是哪一类缺陷
 *
 * `check-write-coverage` 守的是**路由级**：这条 PUT 有没有人调。
 * 但只要有人调过一次，它就绿了 —— 哪怕请求体里 5 个字段只发了 1 个。
 *
 * 真事（ops-cmdb 定时任务）：
 *
 *   后端 PUT /api/scheduled-tasks/:key 收 5 个字段
 *     schedule / notify_enabled / lark_group_id / notify_when / at_user_ids
 *   前端只发   { enabled: 0|1 }
 *
 * 于是「改执行频率」「配飞书通知（开关/群/时机/@谁）」这五件事，
 * 后端全都做好了、旧版界面上全都有，新版**一件也做不了**。
 * 而 check-write-coverage 一路绿灯 —— 那条 PUT 确实有人调。
 *
 * 🔴 这类缺口和「后端有前端没接」是同一个病，只是发生在**字段这一层**，
 * 而我们此前的守卫全部停在路由这一层。
 *
 * # 判据
 *
 * 后端：handler 里的 `var in struct { ... }`，取其 `json:"x"` 标签。
 * 前端：只在**可能构造请求体的地方**找这个键：
 *
 *   1. `useMutation( ... )` 整块（按括号配对切出来）——
 *      本仓的写请求体要么是 `mutationFn` 的参数类型，要么是内联对象，都在这里面
 *   2. `export interface *Input | *Payload | *Req | *Body`——
 *      具名请求体类型，`mutationFn` 只引用它的名字
 *
 * 🔴 **绝不能拿整个前端源码去搜这个键**。响应类型里也有同名字段
 * （`ScheduledTask.notify_when` 就在 queries.ts 里明晃晃写着），
 * 搜全量的话每一条都能"找到"，守卫恒绿。
 * 我第一版就是这么写的，10 条缺口一条都没报出来。
 *
 * ⚠️ 反过来，只搜 `useMutation` 块也不行：具名 `*Input` 接口在块外面，
 * 会把**发好了的**字段报成没发（Harbor 接入的 url / skip_verify 被误报过）。
 * 两个来源缺一不可。
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

/** 从 `useMutation(` 起按括号配对切出整块。正则做不到这件事（里面全是嵌套括号）。 */
function mutationBlocks(src) {
  const out = []
  for (const m of src.matchAll(/useMutation\s*\(/g)) {
    let depth = 0
    for (let i = m.index + m[0].length - 1; i < src.length; i++) {
      const ch = src[i]
      if (ch === '(') depth++
      else if (ch === ')') {
        depth--
        if (depth === 0) {
          out.push(src.slice(m.index, i + 1))
          break
        }
      }
    }
  }
  return out
}

/**
 * 确认不需要前端发的字段。加进来必须写理由。
 *
 * ⚠️ 别把"还没做"塞进这里 —— 那样这个守卫就退化成一张待办清单，
 * 而待办清单是不会让构建失败的。
 */
const ALLOW = new Map([])

/** 建档挂账：已建档、等排期。修一条删一条。 */
const FILED = new Map([
])

const scopes = process.argv.slice(2)
const ALL = products()
const TARGETS = scopes.length ? ALL.filter((p) => scopes.includes(p)) : ALL
if (scopes.length && TARGETS.length === 0) {
  console.error(`✗ check-write-fields: 没有匹配的产品（传入 ${scopes.join(', ')}）`)
  process.exit(1)
}

const fresh = []
const stale = []
const notCovered = []
let checked = 0
let carried = 0

for (const prod of TARGETS) {
  const beDir = join(ROOT, prod, 'backend')
  const feDir = join(ROOT, prod, 'frontend/src')
  if (!existsSync(beDir) || !existsSync(feDir)) continue

  const feFiles = walk(feDir, /\.tsx?$/)
  const corpus = feFiles
    .flatMap((f) => {
      const s = readFileSync(f, 'utf8')
      const inputs = [
        ...s.matchAll(/export interface (\w*(?:Input|Payload|Req|Body))\s*\{[\s\S]*?\n\}/g),
      ].map((m) => m[0])
      return [...mutationBlocks(s), ...inputs]
    })
    .join('\n')

  const beFiles = walk(beDir, /\.go$/).filter((f) => !f.endsWith('_test.go'))
  let structs = 0

  for (const f of beFiles) {
    const src = readFileSync(f, 'utf8')
    for (const m of src.matchAll(/var in struct \{([\s\S]*?)\n\t\}/g)) {
      const keys = [...m[1].matchAll(/json:"([a-z0-9_]+)"/g)].map((x) => x[1])
      if (keys.length === 0) continue
      structs++
      // handler 名：往回找最近的一个 gin handler 签名
      const fns = [...src.slice(0, m.index).matchAll(/func \([^)]*\) (\w+)\(c \*gin\.Context\)/g)]
      const handler = fns.length ? fns[fns.length - 1][1] : '?'
      for (const k of keys) {
        checked++
        const key = `${prod} ${handler}.${k}`
        // 键必须出现在"可能构造请求体"的语料里，两种写法都要认：
        //   显式键   { token: t }   /   token?: string
        //   简写      { token }      ← ⚠️ 没有冒号
        //
        // 🔴 只认显式键的话，`api.post('/license', { token })` 这种简写
        //	会被报成"没发" —— ops-sso 的授权激活当场被误报过。
        //	而守卫误报别的产品，是让人开始整体忽略守卫的最快方式。
        const explicit = new RegExp(`(^|[^\\w.'"\`])${k}\\s*[:?]`, 'm')
        const shorthand = new RegExp(`[{,]\\s*${k}\\s*[,}]`, 'm')
        const sent = explicit.test(corpus) || shorthand.test(corpus)
        if (sent) {
          if (ALLOW.has(key) || FILED.has(key)) stale.push(key)
          continue
        }
        if (ALLOW.has(key) || FILED.has(key)) {
          carried++
          continue
        }
        fresh.push({ key, file: relative(ROOT, f), handler, field: k })
      }
    }
  }

  // ⚠️ 一个产品**一个写请求结构体都没扫到**是可疑的，不能当正常情况跳过。
  //	被点名的产品要拦（那是它自己的构建），顺带扫到的只报不拦 ——
  //	守卫拦了跟自己无关的东西，是让人开始整体忽略守卫的最快方式。
  if (beFiles.length > 0 && structs === 0) {
    const msg =
      `${prod} 有 ${beFiles.length} 个 Go 文件，但一个 \`var in struct\` 都没扫到 —— ` +
      `本守卫对它实际上没有生效（它可能用具名结构体或标准库解码）。`
    if (scopes.includes(prod)) {
      console.error(`✗ check-write-fields: ${msg}\n  要么改判据，要么在这里显式说明为什么不适用。`)
      process.exit(1)
    }
    notCovered.push(msg)
  }
}

if (stale.length > 0) {
  console.error('✗ check-write-fields: 下面这些已经接上了，但还挂在名单里：\n')
  for (const k of stale) console.error(`    ${k}`)
  console.error(`
接上了就把它从 ALLOW / FILED 里删掉。
留着等于把那个位置**永久豁免** —— 下次它又被删掉时，守卫不会响。`)
  process.exit(1)
}

if (fresh.length > 0) {
  console.error('✗ check-write-fields: 后端收得下、前端一次都没发过的字段：\n')
  for (const p of fresh) {
    console.error(`    ${p.file}  ${p.handler}.${p.field}`)
  }
  console.error(`
路由有人调**不等于**字段都发了。定时任务那条 PUT 收 5 个字段、
前端只发 1 个，于是「改执行频率」「配飞书通知」全都做不了，
而 check-write-coverage 一路绿灯 —— 那条 PUT 确实有人调过。

三条出路，选一条：
  · 该让人改 → 界面上加入口，把它发出去
  · 后端本来就不该收 → 从请求体里删掉，别留着骗人
  · 确有理由不发 → 加进 ALLOW，**写清楚理由**

⚠️ 「暂时没空做」不是理由。那种要先建档，再挂进 FILED 并注明档号。`)
  process.exit(1)
}

for (const m of notCovered) console.log(`⊘ check-write-fields: ${m}`)
console.log(
  `✓ check-write-fields: ${checked} 个写请求字段都有前端发` +
    `（挂账 ${carried} 条，范围 ${scopes.length ? scopes.join(', ') : '全部产品'}）`,
)
