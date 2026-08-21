#!/usr/bin/env node
/**
 * 反方向的字段检查：**后端返回了、前端一次都没引用**的字段。
 *
 * check-field-names 查的是「前端用的字段后端有没有」（防写错名字）。
 * 这个查的是反过来那件事 —— 后端辛苦算出来的东西，前端根本没接。
 *
 * 那是本仓最高频的一类缺陷（ops-cmdb 一次验收里撞了 17 次）：
 * 后端看正常、前端看也正常，只有把两侧对起来才看得见。
 * 实测在 ops-video-manager 上查出两条：令牌列表不显示创建人、
 * 总览页说「连续失败达阈值」却不显示阈值是几。
 *
 * ⚠️ 判据必须容忍两类**合法的不引用**，否则误报会淹掉真问题：
 *
 *   1. Record 遍历：`Object.entries(fail_reasons)` —— 源码里不会出现 `dns`
 *      → 只检查**顶层字段**，不下钻到 Record 的键
 *   2. 刻意冗余：provider_code（显示的是 provider_name）、
 *      binding_count（显示的是更细的 lifecycles 分布）
 *      → 白名单，且必须写理由
 *
 * ⚠️ 这个脚本要连着跑起来的后端才能工作（它读真实响应）。
 * 拿不到后端时**跳过并说明**，不能静默通过 —— 静默通过的绿色
 * 会被当成「查过了」。
 */
import { existsSync, readFileSync, readdirSync } from 'node:fs'
import { join, resolve } from 'node:path'

const repo = resolve(new URL('../..', import.meta.url).pathname)
const product = process.argv[2]
const baseURL = process.env.CHECK_BASE_URL
const creds = process.env.CHECK_LOGIN // 形如 admin:password

if (!product) {
  console.error('用法: check-field-usage.mjs <产品名>  （需要 CHECK_BASE_URL 与 CHECK_LOGIN）')
  process.exit(2)
}
if (!baseURL || !creds) {
  // 没给地址就跳过，但要说清楚跳过了什么 —— 见文件头的注释
  console.log(`⊘ check-field-usage: 跳过（未设 CHECK_BASE_URL / CHECK_LOGIN，本检查需要一个跑着的后端）`)
  process.exit(0)
}

/** 合法的不引用。加进来必须写理由。 */
const ALLOW = new Map([
  ['license.product', '产品标识，前端就是这个产品，不需要显示'],
  ['tables.binding_count', '界面显示的是更细的 lifecycles 分布，这个是它的和'],
  ['link-groups.specificity', '界面显示 match_rule（人话版），这是它的数值形式'],
  ['cdn-lines.provider_code', '界面显示 provider_name'],
])

const srcDir = join(repo, product, 'frontend/src')
const allSrc = (function read(dir, acc = []) {
  for (const e of readdirSync(dir, { withFileTypes: true })) {
    if (e.name === 'node_modules' || e.name === 'dist') continue
    const p = join(dir, e.name)
    if (e.isDirectory()) read(p, acc)
    else if (/\.tsx?$/.test(e.name)) acc.push(readFileSync(p, 'utf8'))
  }
  return acc
})(srcDir).join('\n')

const [user, pass] = creds.split(':')
const login = await fetch(`${baseURL}/api/v1/auth/login`, {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ username: user, password: pass }),
})
if (!login.ok) {
  console.error(`✗ check-field-usage: 登录失败 HTTP ${login.status}`)
  process.exit(1)
}
const { token } = await login.json()

// 前端实际调的 GET 接口，从源码里抓（不猜路径 —— 猜出来的 404 会被当成"没问题"）
const paths = new Set()
for (const m of allSrc.matchAll(/apiGet<[^>]*>\(\s*[`'"]([^`'"$]+)/g)) {
  paths.add(m[1].split('?')[0])
}

const problems = []
let checked = 0
for (const path of [...paths].sort()) {
  let body
  try {
    const r = await fetch(baseURL + path, { headers: { Authorization: 'Bearer ' + token } })
    if (!r.ok) continue // 403 多半是授权门控，不是缺陷
    body = await r.json()
  } catch {
    continue
  }
  // 取一行样本
  let sample = body
  for (const k of ['items', 'list']) {
    if (Array.isArray(body?.[k]) && body[k].length) { sample = body[k][0]; break }
  }
  if (!sample || typeof sample !== 'object' || Array.isArray(sample)) continue
  checked++

  const resource = path.replace(/^\/api\/v1\//, '').split('/')[0]
  for (const key of Object.keys(sample)) {
    if (ALLOW.has(`${resource}.${key}`)) continue
    // 分页壳字段不是业务字段
    if (['page', 'size', 'total', 'items', 'list', 'facets'].includes(key)) continue
    // ⚠️ 四种形式都算「用了」。漏掉最后一种（对象字面量的键，
    // 如请求体里的 `environment_scope: envScope`）会误报成没接 ——
    // 我自己就因此在字段已经接好之后又被这个守卫拦了一次。
    const used =
      allSrc.includes(`.${key}`) ||
      allSrc.includes(`"${key}"`) ||
      allSrc.includes(`'${key}'`) ||
      new RegExp(`\\b${key}\\s*:`).test(allSrc)
    if (!used) problems.push(`${path} → ${key}`)
  }
}

if (problems.length) {
  console.error(`✗ check-field-usage: ${problems.length} 个字段后端返回了、前端一次都没引用：\n`)
  for (const p of problems) console.error(`    ${p}`)
  console.error('\n后端算了前端没接 —— 两侧单看都正常，只有对起来才看得见。')
  console.error('确认是刻意冗余的请加进 ALLOW 并写理由。')
  process.exit(1)
}
console.log(`✓ check-field-usage: ${checked} 个接口的字段前端都引用了（范围 ${product}）`)
