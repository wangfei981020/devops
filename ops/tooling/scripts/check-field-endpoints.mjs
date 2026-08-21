#!/usr/bin/env node
/**
 * 守卫：前端类型里的字段，**它调的那个接口**必须真的返回。
 *
 * # 为什么 check-field-names 不够（OPSCMDB-043 D2）
 *
 * 那个守卫查的是「这个字段名在后端出参**全集**里存不存在」。
 * 于是这种情况会被放过：字段名在别的接口里确实有，但**这个接口**没有。
 *
 * 实测栽过两次：
 *
 *   1. 诊断弹窗声明 `suggestions`，而那个接口返回的是 `solutions` ——
 *      `suggestions` 在别的接口里（命名空间归属建议）真的存在，守卫因此放行。
 *      结果「处置建议」整段从来没渲染过。
 *
 *   2. Pod 页声明 `cpu_req_m` / `mem_req_mi` / `cpu_lim_m` / `mem_lim_mi`。
 *      这四个名字在 `k8s_resources.go` 的 SQL 里有（那是另一个 handler），
 *      而真正服务 `/k8s/pod-list` 的 `pods_list.go` **一个都不返回**。
 *      结果 REQ·LIMIT 那一列在**每一行**都显示「未配资源」——
 *      一个很吓人的假陈述，而且不报错。
 *
 * **字段名在后端某处存在 ≠ 在这个接口存在。**
 *
 * # 判据
 *
 * 不做完整类型推导（那需要真正的类型信息，有了它就该直接生成类型）。
 * 利用一个现成的结构：**路由注册和它的 handler 通常在同一个后端文件里**。
 *
 *   前端  queries.ts 里写着它调哪些 URL
 *   后端  每个 URL 在哪个文件里注册
 *   比对  该前端文件声明的 snake_case 字段 ⊆ 那几个后端文件能产出的键
 *
 * 后端文件"能产出的键" = 文件里 gin.H 的字面量键
 *                      + 文件里**按名字引用到**的 struct 的 json tag（跨文件解析）
 *
 * # ⚠️ 为什么只报「疑似」而不直接拦
 *
 * 这个判据是启发式的：handler 可能从别处组装响应、可能经过通用包装。
 * 误报的代价不是烦人，是人开始学着忽略它 —— 所以：
 *   · 只对**能确定映射到单一后端文件**的前端文件生效
 *   · 拿不准的一律跳过并计数，最后如实说"有几个没查"
 *   · 已知的合法情形进 ALLOW 并写原因
 */
import { existsSync, readFileSync, readdirSync, statSync } from 'node:fs'
import { join, relative, basename } from 'node:path'
import { ROOT, products } from './lib/products.mjs'

/** 确认过的例外。每条写清为什么。 */
const ALLOW = new Map([
  // 形如 httpx.NewList 的通用包装，键不出现在 handler 文件里
  ['items', '通用列表包装 httpx.NewList 产出'],
  ['total', '同上'],
  ['facets', '同上'],
  ['page', '同上'],
  ['size', '同上'],
  ['ok', '大量接口的通用成功标记'],
  ['error', '通用错误字段'],
  ['msg', '通用消息字段'],
  ['hint', '通用提示字段'],
])

function walk(dir, re, out = []) {
  if (!existsSync(dir)) return out
  for (const name of readdirSync(dir)) {
    if (name === 'node_modules' || name === 'vendor') continue
    const p = join(dir, name)
    if (statSync(p).isDirectory()) walk(p, re, out)
    else if (re.test(p)) out.push(p)
  }
  return out
}

const problems = []
const skipped = []
let checked = 0

for (const prod of products()) {
  const beDir = join(ROOT, prod, 'backend')
  const feDir = join(ROOT, prod, 'frontend/src/routes')
  if (!existsSync(beDir) || !existsSync(feDir)) continue

  const goFiles = walk(beDir, /\.go$/).filter((f) => !f.endsWith('_test.go'))
  const goSrc = Object.fromEntries(goFiles.map((f) => [f, readFileSync(f, 'utf8')]))

  // struct 名 → json tag 键（整个后端范围，struct 可以定义在别的文件）
  const structKeys = new Map()
  for (const f of goFiles) {
    for (const m of goSrc[f].matchAll(/type\s+(\w+)\s+struct\s*\{([\s\S]*?)\n\}/g)) {
      const keys = new Set()
      for (const t of m[2].matchAll(/json:"([^",]+)/g)) if (t[1] !== '-') keys.add(t[1])
      if (keys.size > 0) structKeys.set(m[1], keys)
    }
  }

  // (接收者类型, 方法名) → 定义它的文件。
  //
  // ⚠️ **必须带接收者类型**。只按方法名匹配的话，`List` 是几十个 handler
  //	共有的名字，一个路由会把几十个文件都算成"服务它的文件"，
  //	允许集膨胀到接近全量 —— 守卫看着在跑，实际什么都抓不到。
  //	（第一版就是这样，输出里一条路由列出了 37 个文件。）
  const typeMethodFile = new Map()
  for (const f of goFiles) {
    for (const m of goSrc[f].matchAll(/func\s*\(\s*\w+\s+\*?(\w+)\s*\)\s*(\w+)\s*\(/g)) {
      typeMethodFile.set(m[1] + '.' + m[2], f)
    }
  }

  /** 找出某个位置所处的那个方法的接收者类型 */
  const receiverAt = (src, idx) => {
    let recv = null
    for (const m of src.matchAll(/func\s*\(\s*(\w+)\s+\*?(\w+)\s*\)/g)) {
      if (m.index > idx) break
      recv = { v: m[1], t: m[2] }
    }
    return recv
  }

  // URL 路径 → 服务它的后端文件集合（注册处 + handler 实现处）
  const routeFile = new Map()
  for (const f of goFiles) {
    const src = goSrc[f]
    for (const m of src.matchAll(/\br\.(GET|POST|PUT|DELETE)\(\s*"([^"]+)"\s*,\s*(\w+)\.(\w+)\s*\)/g)) {
      const path = m[2].replace(/^\/api/, '')
      const set = routeFile.get(path) ?? new Set()
      set.add(f)
      // `h.List` 里的 h 是**当前 Register 方法的接收者**，据此拿到类型
      const recv = receiverAt(src, m.index)
      if (recv && recv.v === m[3]) {
        const hit = typeMethodFile.get(recv.t + '.' + m[4])
        if (hit) set.add(hit)
      }
      routeFile.set(path, set)
    }
  }

  /** 一个后端文件能产出哪些键 */
  const keysOfFile = (f) => {
    const src = goSrc[f]
    const out = new Set()
    // ① 响应体字面量里的键。
    //
    //	⚠️ 原来是按 `gin.H{ … }` 块匹配的，用的**非贪婪** `[\s\S]*?` ——
    //	遇到嵌套就在第一个 `}` 处截断：
    //
    //	    gin.H{"list": []gin.H{}, "configured": false,
    //	          "hint_key": "…"}          ← hint_key 落在截断之外
    //
    //	于是一个**真的返回了**的字段被报成"接口不返回"（实测 alerts.go 的 hint_key）。
    //	误报比漏报更坏 —— 人会开始忽略这个守卫的输出。
    //
    //	改成整文件扫 `"key":`：允许集会宽一点（SQL、注释里的也收进来），
    //	但这个守卫的精度主要靠**文件范围**限定（只看服务这个路由的那几个文件），
    //	键收得宽一点不会让它失去作用，而截断会直接产生假问题。
    for (const k of src.matchAll(/"([a-z0-9_]+)"\s*:/g)) out.add(k[1])
    // ② map 下标赋值：`out["empty_reason"] = ...`
    //	⚠️ 这一形态漏掉的话会大批误报 —— alerts.go 的可选字段全是这么写的
    //	（只在真的筛掉了、真的截断了才加那个键）。实测第一版因此报了 5 条假的。
    // ⚠️ `[` 前面可以是 `)` —— `get(k)["cpu_pct"] = ...` 这种写法很常见。
    //	只认 `\w+[` 的话会漏掉一批，实测误报了 obs_query.go 的 cpu_pct/mem_pct。
    // ⚠️ 三种写法都要认，少认一种就误报一批：
    //	  out["k"] = v            普通赋值
    //	  get(k)["cpu_pct"] = v   `[` 前面是 `)`
    //	  r["a"], r["b"] = x, y   **多重赋值** —— `]` 后面跟的是逗号不是等号
    //	所以按行判：这一行有 `=` 才算写入，然后把行内所有 ["键"] 收进来。
    for (const line of src.split('\n')) {
      if (!line.includes('=')) continue
      for (const m of line.matchAll(/\[\s*"([a-z0-9_]+)"\s*\]/g)) out.add(m[1])
    }
    // ③ SQL 列名。
    //	本仓的 scanRows 把结果集直接转成 map[string]any，**键就是列名**
    //	（handlers/k8s_resources.go 的注释里写着）。也就是说这类接口的出参
    //	根本不经过 Go struct —— 不扫 SQL 的话，`/k8s/hpas`、`/k8s/node-pools`
    //	这些接口的字段会被整批误报（实测 7 条）。
    //	⚠️ 别名要取 AS 后面那个：`COALESCE(reason,'') AS reason` 给出的键是 reason。
    for (const m of src.matchAll(/SELECT\s+([\s\S]*?)\s+FROM\b/gi)) {
      for (const t of m[1].matchAll(/\bAS\s+(\w+)/gi)) out.add(t[1].toLowerCase())
      // ⚠️ 单词列名也要收（version / name / phase）——
      //	只收带下划线的话，`SELECT id,name,machine_type,node_count,version`
      //	里的 version 会被漏掉并误报（实测 NodePool.version）。
      //	顺带收进来的 SQL 关键字只是让允许集略宽，不会造成误报。
      for (const t of m[1].matchAll(/\b([a-z][a-z0-9_]*)\b/g)) out.add(t[1])
    }

    // ④ 文件里出现的**所有** json tag —— 同时覆盖函数内的**匿名内联 struct**
    //	（`var out struct{ NextRunAt string ... }`）。
    //	⚠️ 只认 `type X struct` 会漏掉它们，实测因此误报了
    //	next_run_at / vcpu_hour_usd 等一批。
    for (const t of src.matchAll(/json:"([^",]+)/g)) if (t[1] !== '-') out.add(t[1])
    // 文件里按名字用到的具名 struct，把它们的 json tag 也算上
    for (const [name, keys] of structKeys) {
      if (new RegExp(`\\b${name}\\b`).test(src)) for (const k of keys) out.add(k)
    }
    return out
  }

  for (const qf of walk(feDir, /queries\.ts$/)) {
    const src = readFileSync(qf, 'utf8')
    // 这个文件调了哪些 URL
    const urls = new Set()
    for (const m of src.matchAll(/api\.GET\(\s*'([^']+)'/g)) urls.add(m[1])
    for (const m of src.matchAll(/apiGet<[^>]*>\(\s*[`'"]([^`'"?$]+)/g)) urls.add(m[1].replace(/^\/api/, ''))
    // ⚠️ 写接口也要算：它们的**响应**同样有类型声明。
    //	只扫 GET 的话，`/k8s/ns-projects/auto`（POST）的 AutoResult 会被整批误报。
    for (const m of src.matchAll(/apiAction<[^>]*>\(\s*[`'"]([^`'"?$]+)/g)) urls.add(m[1].replace(/^\/api/, ''))
    for (const m of src.matchAll(/apiAction\(\s*[`'"]([^`'"?$]+)/g)) urls.add(m[1].replace(/^\/api/, ''))
    for (const m of src.matchAll(/apiSend[^(]*\(\s*[`'"]([^`'"?$]+)/g)) urls.add(m[1].replace(/^\/api/, ''))
    // ⚠️ 少数地方用**裸 fetch**（overview 那条要自己处理 401/403 的分支）。
    //	不认这一形态的话，那个文件的 URL 会解析成 0 个，字段被整批误报。
    for (const m of src.matchAll(/\bfetch\(\s*[`'"](\/api\/[^`'"?$]+)/g)) urls.add(m[1].replace(/^\/api/, ''))

    const files = new Set()
    let unresolved = 0
    for (const u of urls) {
      // 路由模板里的 :id 对应前端的 {id}
      const norm = u.replace(/\{[^}]+\}/g, ':x')
      let hit = routeFile.get(u) ?? routeFile.get(norm)
      if (!hit) {
        for (const [p, fs] of routeFile) {
          if (p.replace(/:[^/]+/g, ':x') === norm) { hit = fs; break }
        }
      }
      if (hit) for (const f of hit) files.add(f)
      else unresolved++
    }
    // 🔴 拿不准就跳过并**说出来**，不要假装查过了
    if (files.size === 0 || unresolved > 0) {
      skipped.push(`${relative(ROOT, qf)}（${urls.size} 个 URL，${unresolved} 个没对上路由）`)
      continue
    }

    const allowed = new Set()
    for (const f of files) for (const k of keysOfFile(f)) allowed.add(k)

    for (const m of src.matchAll(/(?:export\s+)?interface\s+(\w+)\s*\{([\s\S]*?)\n\}/g)) {
      const [, iface, body] = m
      // 请求体类型不参与（那是我们发出去的，不是后端返回的）
      if (/(Input|Params|Payload|Req|Body)$/.test(iface)) continue
      // 🔴 只查**后端原始形状**，不查前端自己的视图模型。
      //
      //	判据：接口里至少有一个 snake_case 字段。
      //	前端映射后的模型是纯驼峰（Host.cluster / Disk.role / Node.kubelet），
      //	它们的字段名本来就不必和后端一致 —— 拿它们去比会整批误报（实测 8 条）。
      //	而原始形状（RawPod / Alert / AlertListResult）总会带 snake_case。
      //
      // ⚠️ 单词字段（suggestions / risky）也要查：
      //	`suggestions` vs `solutions` 那个真缺陷就是单词字段，
      //	只查带下划线的名字会把整类漏掉。
      if (!/^\s{2}\w*_\w*\??:/m.test(body)) continue
      for (const fm of body.matchAll(/^\s{2}(\w+)\??:/gm)) {
        const field = fm[1]
        if (!/^[a-z][a-z0-9_]*$/.test(field)) continue // 全小写字段（含单词，见下）
        checked++
        if (ALLOW.has(field) || allowed.has(field)) continue
        problems.push({
          file: relative(ROOT, qf),
          iface,
          field,
          urls: [...urls].join(', '),
          serving: [...files].map((f) => basename(f)).join(', '),
        })
      }
    }
  }
}

if (skipped.length > 0) {
  console.log(`⊘ check-field-endpoints: ${skipped.length} 个前端文件没查（URL 对不上路由）：`)
  for (const s of skipped.slice(0, 8)) console.log(`    ${s}`)
  if (skipped.length > 8) console.log(`    …另外 ${skipped.length - 8} 个`)
}

if (problems.length > 0) {
  console.error('\n✗ check-field-endpoints: 前端声明的字段，它调的那个接口不返回：\n')
  for (const p of problems) {
    console.error(`    ${p.file}  ${p.iface}.${p.field}`)
    console.error(`      调的接口: ${p.urls}（由 ${p.serving} 提供）`)
  }
  console.error(`
**字段名在后端某处存在 ≠ 在这个接口存在。**
check-field-names 只查全集，正是这一层放过了两次真缺陷：
  · 诊断弹窗读 suggestions，那个接口返回的是 solutions —— 处置建议整段没渲染过
  · Pod 页读 cpu_req_m，服务 /k8s/pod-list 的文件一个都不返回 ——
    REQ·LIMIT 每一行都显示「未配资源」，一个不报错的假陈述

三条出路：
  · 后端本来就该返回 → 补上
  · 前端读错名字了   → 改成对的
  · 判据在这里不适用 → 加进 ALLOW 并写清原因`)
  process.exit(1)
}
console.log(`✓ check-field-endpoints: ${checked} 个字段都由它所调的接口返回`)
