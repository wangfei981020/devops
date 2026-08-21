#!/usr/bin/env node
/**
 * 前端传的 query 参数，后端必须真的读。
 *
 * 🔴 抓的是**静默失效**：参数名对不上时 Gin 不会报错，handler 拿默认值继续跑，
 *	界面照常显示一个数字 —— 只是那个数字答的是另一个问题。
 *
 *	实测（OPSCMDB-049）：成本报表前端传 `month=2026-07`，而 handler 只读 `anchor`。
 *	于是选哪个月都返回当前月：界面上 7 月显示 842.65，7 月真实是 2705.7，差 3 倍。
 *	不报错、不空、不崩 —— 这类缺陷只能靠机器逐个参数对。
 *
 * ⚠️ 方向很重要，只查这一个方向：
 *	**前端传了、后端不读** → 一定是错的（传了个没人看的东西）。
 *	反过来"后端读了、前端没传"会大量漏报（参数可能只给 MCP 用、
 *	可能由默认值覆盖），那一层不能上守卫 —— 见 OPSCMDB-049 档案。
 */
import { readFileSync, readdirSync, statSync } from 'node:fs'
import { resolve, join, relative } from 'node:path'

const root = resolve(process.argv[2] ?? '.')
const BACKEND = 'ops-cmdb/backend'
const FRONTEND = 'ops-cmdb/frontend/src'

/** 由框架/中间件消费，不出现在 handler 的 c.Query 里 */
const FRAMEWORK_PARAMS = new Set([
  // httpx.BindPage 统一解析的分页参数
  'page', 'size', 'page_size', 'sort', 'order', 'q',
  // 前端自用，不发给后端语义层
  '_t', 'detail', 'view',
])

function walk(dir, re, out = []) {
  let entries
  try { entries = readdirSync(dir) } catch { return out }
  for (const e of entries) {
    const p = join(dir, e)
    if (statSync(p).isDirectory()) { if (e !== 'node_modules') walk(p, re, out) }
    else if (re.test(p)) out.push(p)
  }
  return out
}

const goFiles = walk(resolve(root, BACKEND), /\.go$/).filter((f) => !f.endsWith('_test.go'))
const goSrc = Object.fromEntries(goFiles.map((f) => [f, readFileSync(f, 'utf8')]))

// (接收者类型, 方法名) → 文件。必须带接收者类型，否则 List 之类的名字会命中几十个文件。
const typeMethodFile = new Map()
for (const f of goFiles) {
  for (const m of goSrc[f].matchAll(/func\s*\(\s*\w+\s+\*?(\w+)\s*\)\s*(\w+)\s*\(/g)) {
    typeMethodFile.set(m[1] + '.' + m[2], f)
  }
}
const receiverAt = (src, idx) => {
  let recv = null
  for (const m of src.matchAll(/func\s*\(\s*(\w+)\s+\*?(\w+)\s*\)/g)) {
    if (m.index > idx) break
    recv = { v: m[1], t: m[2] }
  }
  return recv
}

// 包级函数名 → 源码体。
// ⚠️ 参数解析常常收口在这类共用函数里（requireCluster(c, h.DB) 读 cluster_id），
//	不认它就会把一批正常接口报成"参数没人读"（实测 health/detail、ns-projects/auto）。
const pkgFuncBody = new Map()
for (const f of goFiles) {
  const src = goSrc[f]
  for (const m of src.matchAll(/^func\s+(\w+)\s*\(/gm)) {
    const next = src.indexOf('\nfunc ', m.index + 1)
    pkgFuncBody.set(m[1], src.slice(m.index, next < 0 ? undefined : next))
  }
}

/** 取 (接收者类型, 方法名) 的源码体；找不到返回 null */
function methodBody(recvType, method) {
  const file = typeMethodFile.get(recvType + '.' + method)
  if (!file) return null
  const src = goSrc[file]
  const start = src.search(new RegExp(`func\\s*\\(\\s*\\w+\\s+\\*?${recvType}\\s*\\)\\s*${method}\\s*\\(`))
  if (start < 0) return null
  const next = src.indexOf('\nfunc ', start + 1)
  return src.slice(start, next < 0 ? undefined : next)
}

// 路由路径 → 服务它的 handler 方法体
const routeBody = new Map()
for (const f of goFiles) {
  const src = goSrc[f]
  for (const m of src.matchAll(/\br\.(GET|POST|PUT|DELETE)\(\s*"([^"]+)"\s*,\s*(\w+)\.(\w+)\s*\)/g)) {
    const path = m[2].replace(/^\/api/, '')
    const recv = receiverAt(src, m.index)
    if (!recv || recv.v !== m[3]) continue
    const file = typeMethodFile.get(recv.t + '.' + m[4])
    if (!file) continue
    const own = methodBody(recv.t, m[4])
    if (own === null) continue
    // ⚠️ 必须跟进被委托的方法。
    //	不少 handler 把参数解析交给同一个接收者上的辅助函数：
    //	`cl, ok := h.client(c)` —— registry_id 是在 client() 里读的，
    //	只看 handler 自己的方法体会把三个 Harbor 接口全报成"参数没人读"（实测）。
    //	跟一层就够：再深的委托很少见，而层数越多误判越多。
    let text = own
    for (const call of own.matchAll(/\b\w+\.(\w+)\(/g)) {
      const sub = methodBody(recv.t, call[1])
      if (sub) text += '\n' + sub
    }
    // 包级共用函数：只认**把 gin.Context 传进去**的那些 —— 拿不到 c 就读不了 query，
    // 把无关的工具函数也拼进来只会让判据变松
    for (const call of own.matchAll(/\b(\w+)\(\s*c\s*[,)]/g)) {
      const sub = pkgFuncBody.get(call[1])
      if (sub) text += '\n' + sub
    }
    routeBody.set(path, text)
  }
}

// 前端：抓 `/api/<path>?a=..&b=..` 形式的调用
const tsFiles = walk(resolve(root, FRONTEND), /\.(ts|tsx)$/).filter((f) => !/\.test\./.test(f))
const bad = []
for (const f of tsFiles) {
  const src = readFileSync(f, 'utf8')
  for (const m of src.matchAll(/['"`]\/api(\/[a-z0-9\-/_]*)\?([^'"`]*)['"`]/gi)) {
    const path = m[1].replace(/\/$/, '')
    const body = routeBody.get(path)
    if (!body) continue // 路由没解析出来（路径参数等）——不猜
    // 参数名：`a=` 或 `${x}` 插值前的 `a=`
    for (const pm of m[2].matchAll(/(?:^|&)([a-z0-9_]+)=/gi)) {
      const name = pm[1]
      if (FRAMEWORK_PARAMS.has(name)) continue
      // ⚠️ 判据必须限定在 Query 调用里，不能只搜 `"name"` 这个字符串 ——
      //	响应体里的 `gin.H{"dim": dimOut}` 也含 `"dim"`，
      //	那样一个**回显了但没读**的参数会被判成"读了"（变异测试抓到过）。
      if (new RegExp(`(?:Default)?Query(?:Array)?\\(\\s*"${name}"`).test(body)) continue
      // ShouldBindQuery 的结构体绑定：form tag
      if (new RegExp(`form:"${name}[",]`).test(body)) continue
      // 通用列表封装：参数名写在调用处的字面量里（`[]filter{{"cluster_id", …}}`），
      // 由 h.list 内部以变量形式 c.Query(f.param) 读走。
      // ⚠️ 必须排除**后面跟冒号**的那种 —— 那是响应体的 JSON 键（`gin.H{"dim": …}`），
      //	回显不等于读取（变异测试抓到过这个洞）。
      if (new RegExp(`"${name}"\\s*(?!:)`).test(body)) continue
      const line = src.slice(0, m.index).split('\n').length
      bad.push(`${relative(root, f)}:${line}  /api${path} 传了 ${name}=，但 handler 不读它`)
    }
  }
}

if (bad.length > 0) {
  console.error(`✗ 有 ${bad.length} 处 query 参数后端根本不读（会被静默忽略）：\n`)
  for (const b of [...new Set(bad)]) console.error(`  ${b}`)
  console.error('\n改成 handler 真正读的那个参数名。')
  console.error('⚠️ 别只改前端就算完：确认后端读的是不是你要的语义，')
  console.error('   并让界面显示后端回显的实际取值（如 anchor），否则同类问题还会再藏一次。')
  process.exit(1)
}
console.log(`✓ 前端传的 query 参数后端都读（覆盖 ${routeBody.size} 条路由）`)
