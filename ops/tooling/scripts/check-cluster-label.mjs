#!/usr/bin/env node
/**
 * 守卫：集群名的展示口径只能有**一处**实现。
 *
 * # 为什么
 *
 * 只有部分集群配了中文别名。写成 `displayName || name` 时，
 * 配了别名的显示「开发环境集群」、其余显示原名 —— 四个选项混着两套命名，
 * 用户无从判断「开发环境集群」是哪一个（OPSCMDB-031 NEW-3/5）。
 *
 * 🔴 这类问题**按页面修是修不完的**：修完资源使用率页一处，
 * 随后又在 /platform/pipelines、/k8s/disruption 上冒出来。
 * 那不是"又漏了两处"，是这件事本来就该只有一处实现。
 * 实测一轮下来共 8 处各写各的（其中 2 处是手抄的同款三元表达式）。
 *
 * # 判据
 *
 * 前端源码里出现这两种写法即报：
 *   displayName || name            兜底式
 *   displayName !== name ? ... :   手抄的拼接
 * 正确写法是 `clusterLabel(displayName, name)`（lib/clusterLabel.ts）。
 *
 * ⚠️ 只管**集群**的 displayName。用户的 display_name（session/auth）不在此列 ——
 *	那是另一个概念，`display_name || username` 是对的。
 */
import { existsSync, readFileSync, readdirSync, statSync } from 'node:fs'
import { join, relative } from 'node:path'
import { ROOT, backendDirs, frontendDirs } from './lib/products.mjs'

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

function walk(dir, out = []) {
  if (!existsSync(dir)) return out
  for (const n of readdirSync(dir)) {
    const p = join(dir, n)
    if (statSync(p).isDirectory()) walk(p, out)
    else if (/\.tsx?$/.test(p)) out.push(p)
  }
  return out
}

// 兜底式：`c.displayName || c.name`
//
// ⚠️ 蛇形也要认：实测漏过 `detailFor.display_name || detailFor.name`（OPSCMDB-078）——
//	判据只写驼峰的话，同一种错误换个字段命名风格就完全扫不到。
const reFallback = /\.(?:displayName|display_name)\s*\|\|\s*\w+\.(?:name|cluster_name)\b/
// 手抄的拼接：`displayName !== ... name ? ... : ...`
const reHandRolled = /\.(?:displayName|display_name)\s*&&\s*\w+\.(?:displayName|display_name)\s*!==\s*\w+\.name/
// 光秃秃只显示别名：`label: c.displayName }` —— 原名丢了
const reAliasOnly = /label:\s*\w+\.displayName\s*[,}]/

const problems = []
let scanned = 0
for (const d of frontendDirs()) {
  for (const f of walk(join(ROOT, d))) {
    // 口径本体与集群管理页的表格（那里原名/别名分两行显示，是有意的）除外
    const rel = relative(ROOT, f)
    if (rel.endsWith('lib/clusterLabel.ts')) continue
    const src = readFileSync(f, 'utf8')
    scanned++
    const body = src.replace(/\/\*[\s\S]*?\*\//g, '').replace(/\/\/.*$/gm, '')
    body.split('\n').forEach((l, i) => {
      if (reFallback.test(l) || reHandRolled.test(l) || reAliasOnly.test(l)) {
        problems.push(`${rel}:${i + 1}  ${l.trim().slice(0, 90)}`)
      }
    })
  }
}

// ────────────────────────────────────────────────────────────────
// 判据二（契约层）：后端不许把别名拍进 `cluster_name`
//
// 🔴 上面三条判据全都要求源码里出现 displayName 这个词，因此**只能抓
//	「前端拿到了两个值却没用对」**。而实测漏掉的那一类是
//	「后端只给了一个值」：node-list 把 COALESCE(display_name, name)
//	装进 cluster_name，前端即使想调 clusterLabel 也拿不到技术名
//	（OPSCMDB-078，一次漏掉 7 个接口）。
//
// 判据锚在**形状**上，不在变量名上：
//	凡是 SQL 里出现 `COALESCE(<x>.display_name, <x>.name` 的行，
//	同一条语句里必须也取了裸的 `<x>.name` —— 两个值都传出去。
//
// ⚠️ 不要改成"检查 struct 里有没有 cluster_display_name 字段"：
//	字段声明了但 SQL 没取、或取了没 Scan，照样是空值，
//	而空值在界面上和"没有别名"长得一模一样。判据要落在取数那一步。
const reCoalesceAlias = /COALESCE\(\s*(\w+)\.display_name\s*,\s*\1\.name/
let scannedGo = 0
for (const d of backendDirs()) {
  for (const f of walkGo(join(ROOT, d))) {
    const rel = relative(ROOT, f)
    const src = readFileSync(f, 'utf8')
    scannedGo++
    // Go 的原始字符串里 SQL 常跨行，按语句块看：以 ` 包裹的整块 + 单行字符串
    for (const [i, line] of src.split('\n').entries()) {
      const m = reCoalesceAlias.exec(line)
      if (!m) continue
      const alias = m[1]
      // ⚠️ 只管**取出来给人看**的那种。用在比较里是合法的：
      //	`WHERE COALESCE(c.display_name, c.name) = ?` 是"按别名或技术名任一匹配"，
      //	那里本来就不该拆成两个值。判据要是不分上下文，
      //	就会把正确写法也报出来 —— 而误报的代价是人开始学着忽略这个守卫。
      const after = line.slice(m.index + m[0].length)
      if (/^\s*\)?\s*(?:=|<>|!=|\b(?:LIKE|IN)\b)/i.test(after)) continue
      // 同一行里是否也取了裸的 name（两个值都给）
      const bare = new RegExp(`COALESCE\\(\\s*${alias}\\.name\\b|(?<!display_name,\\s)\\b${alias}\\.name\\s*,`)
      if (bare.test(line.replace(reCoalesceAlias, ''))) continue
      problems.push(`${rel}:${i + 1}  ${line.trim().slice(0, 100)}`)
    }
  }
}

if (problems.length > 0) {
  console.error('✗ check-cluster-label: 集群名的展示口径又散开了：\n')
  for (const p of problems) console.error(`    ${p}`)
  console.error(`
用 clusterLabel(displayName, name)（frontend/src/lib/clusterLabel.ts）。

只有部分集群配了别名 —— 写成 \`displayName || name\` 的话，
配了别名的显示「开发环境集群」、其余显示原名，四个选项混着两套命名。
⚠️ 原名必须始终可见：它是接口、MCP、PromQL 标签里用的标识。

后端那几条（*.go）是另一种形态：把 COALESCE(display_name, name) 拍进一个
cluster_name 字段，前端就再也拿不到技术名了。两个值都要取：
  COALESCE(cl.name, ''), COALESCE(cl.display_name, cl.name, '')
并在 struct 里加 cluster_display_name。`)
  process.exit(1)
}
console.log(
  `✓ check-cluster-label: ${scanned} 个前端文件 + ${scannedGo} 个后端文件，集群名展示口径未散开`,
)
