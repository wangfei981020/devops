#!/usr/bin/env node
/**
 * 后端用到的每个错误码，都必须在 statusOf 里登记 HTTP 状态码。
 *
 * 没登记的会落到默认的 500，而那不只是"状态码不好看"：
 *
 *   - 监控看到 5xx 率飙升，而服务完全健康
 *   - 客户端的重试逻辑会无谓重试一个永远不会变的结果
 *   - 前端按「服务器故障」提示，而正确的话术可能是「这个档次不含，请联系采购」
 *
 * 实测撞到过：ops-video-manager 接完 licensekit，社区版下访问三个受限接口
 * 全返回 500 —— 门控本身是对的，只是漏登记了 feature_not_licensed。
 *
 * ⚠️ 默认落 500 这个设计本身是对的（把未知错误当 4xx 会让监控漏掉真故障），
 * 所以正确的做法不是改默认值，而是让"漏登记"在构建时就被发现。
 */
import { existsSync, readFileSync, readdirSync } from 'node:fs'
import { join, resolve } from 'node:path'

const repo = resolve(new URL('../..', import.meta.url).pathname)
const scopes = process.argv.slice(2)

const products = readdirSync(repo, { withFileTypes: true })
  .filter((e) => e.isDirectory() && e.name.startsWith('ops-') && e.name !== 'ops-kit')
  .map((e) => e.name)
  .filter((n) => !scopes.length || scopes.includes(n))

function* walk(dir) {
  let entries
  try { entries = readdirSync(dir, { withFileTypes: true }) } catch { return }
  for (const e of entries) {
    const p = join(dir, e.name)
    if (e.isDirectory()) { if (e.name !== 'vendor') yield* walk(p) }
    else if (e.name.endsWith('.go') && !e.name.endsWith('_test.go')) yield p
  }
}

let failed = false
let checked = 0

for (const prod of products) {
  const be = join(repo, prod, 'backend')
  if (!existsSync(be)) continue

  // 状态码表。各产品的 httpx 是刻意复制的（Go 的 internal 不能跨 module），
  // 所以要各查各的
  const errFiles = [...walk(be)].filter((f) => /httpx\/errors\.go$/.test(f))
  if (errFiles.length === 0) continue

  // ⚠️ 两种写法都要认，否则会**静默跳过**整个产品：
  //
  //   ops-video-manager   statusOf 的 key 是字符串字面量  "not_found": http.StatusNotFound
  //   ops-cmdb            key 是类型化常量               CodeNotFound: http.StatusNotFound
  //
  // 只认第一种时，ops-cmdb 的 mapped 为空 → `continue` → 那个产品完全不受检查，
  // 而守卫照常打印 ✓。这是本仓库反复出现的同一种守卫缺陷
  // （见 check-dead-state / check-i18n-usage 的注释）。
  const constLiteral = new Map() // 常量名 → 字面量
  for (const f of walk(be)) {
    for (const m of readFileSync(f, 'utf8').matchAll(/\b(Code[A-Z]\w*)\s*=\s*"([a-z_]+)"/g)) {
      constLiteral.set(m[1], m[2])
    }
  }
  const resolve1 = (k) => constLiteral.get(k) ?? k

  const mapped = new Set()
  for (const f of errFiles) {
    const src = readFileSync(f, 'utf8')
    for (const m of src.matchAll(/"([a-z_]+)":\s*http\.Status/g)) mapped.add(m[1])
    for (const m of src.matchAll(/\b(Code[A-Z]\w*):\s*http\.Status/g)) mapped.add(resolve1(m[1]))
  }
  if (mapped.size === 0) {
    console.error(`✗ check-error-status: ${prod} 有 httpx/errors.go 但一个状态码映射都没解析出来 ——`)
    console.error('  多半是写法没被识别，那会让这个产品完全逃过本检查')
    failed = true
    continue
  }
  checked++

  // 用到的错误码。两种调用形式都要认：Fail 与 FailKey
  const used = new Map() // code → 文件
  for (const f of walk(be)) {
    const src = readFileSync(f, 'utf8')
    for (const m of src.matchAll(/httpx\.Fail(?:Key)?\(\s*\w+\s*,\s*"([a-z_]+)"/g)) {
      if (!used.has(m[1])) used.set(m[1], f.replace(repo + '/', ''))
    }
    // 常量形式的调用：httpx.Fail(c, httpx.CodeNotFound, ...)
    for (const m of src.matchAll(/httpx\.Fail(?:Key)?\(\s*\w+\s*,\s*(?:httpx\.)?(Code[A-Z]\w*)/g)) {
      const lit = resolve1(m[1])
      if (!used.has(lit)) used.set(lit, f.replace(repo + '/', ''))
    }
  }

  const missing = [...used].filter(([code]) => !mapped.has(code))
  if (missing.length) {
    failed = true
    console.error(`✗ check-error-status: ${prod} 有错误码没登记 HTTP 状态码（会落到 500）：`)
    for (const [code, file] of missing) console.error(`    ${code}\n      ${file}`)
  }
}

if (failed) {
  console.error('\n在 internal/httpx/errors.go 的 statusOf 里补上映射。')
  console.error('「没买」「超限」这类是 403，不是 500 —— 500 会让监控误报服务故障。')
  process.exit(1)
}

// 🔴 一个产品都没检查到时**不能打勾**。
//
// 这条是刚写进 CONVENTIONS 又立刻违反的：ops-sso / ops-alert 没有
// internal/httpx/errors.go（它们的状态码映射在别处），于是被 continue 跳过，
// checked 停在 0，而脚本照常打印「✓ 0 个产品的错误码都登记了状态码」。
// 「0 个产品全都合格」在字面上没错，但它读起来就是「查过了，没问题」。
if (checked === 0) {
  console.error('✗ check-error-status: 一个产品都没检查到 ——')
  console.error('  没找到 internal/httpx/errors.go，多半是这些产品的状态码映射在别处。')
  console.error(`  被跳过的：${products.join(', ')}`)
  console.error('  要么补上解析逻辑，要么在这里显式登记「这个产品不适用及其原因」。')
  process.exit(1)
}

console.log(
  `✓ check-error-status: ${checked} 个产品的错误码都登记了状态码` +
    `${scopes.length ? `（范围 ${scopes.join(', ')}）` : '（范围 全部产品）'}`,
)
