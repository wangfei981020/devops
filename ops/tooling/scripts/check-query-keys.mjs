#!/usr/bin/env node
/**
 * 守卫：同一个 React Query 键，不能对应两种**不同形状**的数据。
 *
 * # 它抓的是哪一类缺陷
 *
 * React Query 按 key 缓存**数据**，queryFn 只在缓存缺失时跑。
 * 两个组件用同一个 key、却各自返回不同形状时：
 *
 *   谁先跑谁定形状，另一个组件读到的就是错的类型。
 *
 * 真事（ops-cmdb 云账号 / 集群发现弹窗）：
 *
 *   routes/cloudaccounts/queries.ts   ['cloud-accounts'] → { items: CloudAccount[] }
 *   routes/clusters/DiscoverDialog    ['cloud-accounts'] → CloudProject[]（拍平）
 *
 * 后者拿到对象后 `list.find(...)` 直接抛「list.find is not a function」，弹窗白屏。
 *
 * 🔴 触发条件极其隐蔽：在云账号页新增一个项目会 invalidate 这个键，
 * 之后切到集群页打开发现弹窗才崩；而**刷新一下页面又好了**
 * （刷新后是另一个组件先跑）。于是它表现成"偶发"，
 * 实际上是必然的 —— 只是取决于谁先跑。
 *
 * # 判据
 *
 * 同一个字面量 key 出现在多个文件的 `queryKey:` 上就报。
 * 真要共享缓存的话，两边必须共用同一个 queryFn（抽成一个 hook），
 * 而不是各写各的。
 *
 * ⚠️ 只看**字面量**首段。带变量的键（['pods', clusterId]）天然按参数分开，
 * 不在此列。
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
    else if (/\.tsx?$/.test(name)) out.push(p)
  }
  return out
}

/**
 * 确认可以共用的键。加进来必须写清楚**两边形状一致**的理由。
 *
 * ⚠️ 别把"暂时没出问题"塞进来 —— 这个缺陷只在特定先后顺序下暴露，
 * 没出问题不代表没有。
 */
const ALLOW = new Map([])

const scopes = process.argv.slice(2)
const ALL = products()
const TARGETS = scopes.length ? ALL.filter((p) => scopes.includes(p)) : ALL
if (scopes.length && TARGETS.length === 0) {
  console.error(`✗ check-query-keys: 没有匹配的产品（传入 ${scopes.join(', ')}）`)
  process.exit(1)
}

const problems = []
let checked = 0
const notCovered = []

for (const prod of TARGETS) {
  const feDir = join(ROOT, prod, 'frontend/src')
  if (!existsSync(feDir)) continue
  const files = walk(feDir)

  // key（完整字面量数组的文本）→ 出现过的文件
  const byKey = new Map()
  for (const f of files) {
    const src = readFileSync(f, 'utf8')
    // 只认**全字面量**的键：queryKey: ['a'] / ['a', 'b']
    // 带变量的（['pods', cid]）天然按参数分桶，不会互相覆盖
    for (const m of src.matchAll(/queryKey:\s*\[((?:\s*'[^']*'\s*,?)+)\]/g)) {
      // 🔴 必须区分「声明」和「失效」：
      //	  useQuery({ queryKey: [...], queryFn: ... })   ← 定义了这个键的数据形状
      //	  invalidateQueries({ queryKey: [...] })        ← 只是让缓存过期
      //
      //	从多处 invalidate 同一个键是**正常且正确**的写法（谁改了数据谁失效）。
      //	把它也算成冲突的话，第一次跑就误报了 3 组（domain-list、relations、
      //	cloud-accounts 的 invalidate 调用），而其中一个字面上还是我自己刚修的那条 ——
      //	守卫误报的代价不是烦人，是人开始学着忽略它。
      //
      //	判据：往后看一小段，有 queryFn 才算声明。
      const after = src.slice(m.index, m.index + 500)
      if (!/queryFn\s*:/.test(after)) continue
      const key = '[' + m[1].replace(/\s+/g, '') + ']'
      checked++
      if (!byKey.has(key)) byKey.set(key, new Set())
      byKey.get(key).add(relative(ROOT, f))
    }
  }

  for (const [key, filesUsing] of byKey) {
    if (filesUsing.size < 2) continue
    if (ALLOW.has(`${prod} ${key}`)) continue
    problems.push({ prod, key, files: [...filesUsing] })
  }

  if (files.length > 0 && checked === 0) {
    notCovered.push(`${prod} 有 ${files.length} 个前端源文件，但一个字面量 queryKey 都没扫到`)
  }
}

if (problems.length > 0) {
  console.error('✗ check-query-keys: 同一个缓存键被多处使用，形状可能不一致：\n')
  for (const p of problems) {
    console.error(`    ${p.key}`)
    for (const f of p.files) console.error(`      ${f}`)
  }
  console.error(`
React Query 按 key 缓存**数据**，queryFn 只在缓存缺失时跑 ——
两处返回的形状不同时，**谁先跑谁定形状**，另一处读到的就是错的类型。

🔴 这类崩溃表现成"偶发"：刷新一下就好了（刷新后换了谁先跑），
实际上是必然的。ops-cmdb 撞过一次：云账号页返回 { items: [...] }、
集群发现弹窗返回拍平的数组，后者 list.find(...) 直接白屏。

修法二选一：
  · 真要共享 → 抽成同一个 hook，两边共用同一个 queryFn
  · 形状本来就不同 → 换个键（如 ['cloud-accounts','flattened-projects']）
    ⚠️ invalidateQueries 是前缀匹配，加后缀不影响失效联动`)
  process.exit(1)
}

for (const m of notCovered) console.log(`⊘ check-query-keys: ${m}`)
console.log(
  `✓ check-query-keys: ${checked} 处字面量缓存键无冲突` +
    `（范围 ${scopes.length ? scopes.join(', ') : '全部产品'}）`,
)
