#!/usr/bin/env node
/**
 * 后端不许把面向用户的中文句子写进响应。
 *
 * 约定本来就有（`internal/httpx/errors.go` 的 APIError 注释）：
 *	「后端不返回给用户看的句子，只返回 code + 参数，由前端翻译。
 *	 后端拼好中文句子发过去，英文界面就永远漏中文，
 *	 而且只在错误路径上出现 —— 正常测试根本走不到，能一直活到客户手里。」
 *
 * 391 处存量是没遵守这条约定的历史，用基线锁住：**只挡新增**。
 * 每迁移一批就把基线调低，差值不能留着当"白送的额度"。
 *
 * ⭐ **ops-cmdb 已经清零**（2026-08-21）。基线留在这里是为了别的产品，
 *	以及万一有人往回加。清零不代表"响应里没有中文了"——
 *	留给 MCP / AI 的中文原句仍然在，只是每一句都配了 `*_key`，
 *	或者所在的函数标了 `//ops:mcp-only`（那种输出只有 AI 会读）。
 *
 * ⚠️ 迁移不是"翻译几句"。中英混排比全中文更难读，所以粒度必须是**页面级**：
 *	一个页面涉及的所有文案一次迁完，那个页面才算英文可用（OPSCMDB-054）。
 *
 * 🔴 已知盲区：**先赋值给变量、再放进响应**的中文扫不到。
 *
 *	  note = fmt.Sprintf("有 %d 条从来没有探测过 …")   ← 这一句守卫看不见
 *	  return gin.H{"probe_note": note}
 *
 *	把判据放宽到"函数体里任何中文串"会把日志、注释、SQL 注释全算进来，
 *	那种噪声下没人会认真看这个守卫的输出 —— 所以这里**故意**只认
 *	`"key": "中文"` 这一种形态。
 *
 *	也就是说：这个守卫的绿色**不等于**"响应里没有中文了"，
 *	只等于"最常见的那种写法没有新增"。别拿它当验收依据。
 *
 * # 🔴 口径修正（2026-08-21）：只数**没有配对 key** 的
 *
 * 原来把所有中文一起数，于是这个数字**永远不可能归零** ——
 * 因为约定本身要求「界面读 key、MCP/AI 读中文原句」，两者要**同时**返回：
 *
 *	gin.H{"error_key": "pipeline.logFetchFailed", "error": "取构建日志失败：…"}
 *	                    ^^^ 界面翻译用            ^^^ 这一句是给 AI 的，**不该删**
 *
 * 一个数字同时装着「有意保留的」和「真缺口」，就没法回答"还差多少"，
 * 更糟的是它会引导人去删给 AI 看的那半边来把数字压下去。
 *
 * 所以判据改成：**同一个 gin.H 块里有配对的 `<key>_key` 就不算**。
 * 这样这个数字才真的表示"还有多少处界面翻不了"，也才有可能归零。
 */
import { readFileSync, readdirSync } from 'node:fs'
import { resolve, join } from 'node:path'

const CJK = /[一-鿿]/
// 会显示给终端用户的响应键
const KEYS = ['hint', 'error', 'msg', 'message', 'summary', 'note', 'reason', 'detail', 'issue', 'action', 'basis', 'warning', 'tip']

/**
 * 各产品的存量基线。**只减不增**。
 * 数字来自本守卫自己的口径 —— 换成别的数法就对不上了。
 */
const BASELINE = new Map([
  ['ops-cmdb', 0],
])

// ⚠️ 基线里包含两类**有意保留**的中文，加起来约 29 处：
//
//	1. 与 `msg_key` 成对的 `msg`（动作成功提示）
//	2. 与 `error_key` 成对的 `error`（HTTP 200 + {ok:false} 那类动作接口）
//
//	界面读 key（可翻译），**MCP / 直接调 API 的人读中文原句** ——
//	那一侧的读者是 AI 和运维，一句中文比一个 key 有用得多；
//	而且原句里往往带着技术细节（"HTTP 502"、原始错误），key 表达不了。
//
//	所以基线**不会归零**。归零反而说明有人把给 AI 看的那半边删掉了。

/**
 * 判断这个位置是不是在一次日志调用里。
 *
 * 判据：往前找最近的 `logx.` 或 `log.Printf(`，看它和当前位置之间
 * 有没有出现语句结束（`)\n` 后跟一个非缩进行）。简单但够用 ——
 * 日志调用都是一次性写完的，不会跨函数。
 */
/**
 * 这一处有没有配对的 `<key>_key`。
 *
 * ⚠️ 必须限定在**同一个 gin.H / map 块**里 —— 拿固定字符数开窗口的话，
 *	相邻的另一个响应里的 key 会被算成这一处的配对，
 *	于是真缺口被误判成"已配对"，守卫报绿而界面照样是中文。
 */
function hasPairedKey(src, idx, key) {
  // 🔴 往回**数括号**找外层未闭合的 `{`，不能只取"最近的 gin.H{"。
  //
  //	`"error_params": map[string]any{"reason": msg},` 这样的**兄弟**内联 map
  //	比外层 gin.H 更近，而它在当前位置之前就闭合了 ——
  //	按"最近的块起点"找会落到它上面，于是已配对的被判成未配对。
  //	（实测：4 处补了 key，守卫只认出 2 处。）
  let depth = 0
  let i = idx - 1
  while (i >= 0) {
    if (src[i] === '}') depth++
    else if (src[i] === '{') {
      if (depth === 0) break // 找到了：这是包住当前位置的那个 `{`
      depth--
    }
    i--
  }
  if (i < 0) return false
  // 从这个 `{` 配对到它的 `}`
  let j = i + 1
  let d = 1
  while (d > 0 && j < src.length) {
    if (src[j] === '{') d++
    else if (src[j] === '}') d--
    j++
  }
  const block = src.slice(i, j)
  // ⚠️ 走 httpx.Fail / FailKey / FailKeyWith 时，`message_key` 是**函数参数**，
  //	不是同块里的字面量 —— 按"块里有没有 message_key"找是找不到的，
  //	于是**最规范的那种写法**（结构化错误 + extra 里带中文给 MCP）
  //	反而被算成未迁移。这三个函数一定会发 message_key，直接认。
  if (key === 'error' || key === 'message') {
    const lead = src.slice(Math.max(0, i - 300), i)
    const at = lead.lastIndexOf('httpx.Fail')
    if (at >= 0 && !lead.slice(at).includes('c.JSON')) return true
  }
  // ⚠️ `error` / `message` 的配对键还有一个：结构化错误用的是 `message_key`
  //	（见 internal/httpx/errors.go 的 APIError）。只认 `error_key` 的话，
  //	用标准形态写的那些会被算成"未配对"，数字降不下来 —— 而它们恰恰是最规范的。
  if ((key === 'error' || key === 'message') && /"message_key"/.test(block)) return true
  return new RegExp(`"${key}_key"`).test(block)
}

/**
 * 这一处是不是落在标了 `//ops:mcp-only` 的函数里。
 *
 * 🔴 那种输出**只有 AI 会读**，翻译它没有意义，也不该占着"还差多少"的名额。
 *
 * ⚠️ 必须是**显式声明**，不能靠"前端调不调这个路由"去推断 ——
 *	实测那个推断在插值路径（`/api/cdn/accounts/${id}/verify`）上会误判成
 *	MCP-only，据此豁免等于悄悄放过真的界面文案。
 *	声明式的判据即使写错了，至少在代码里看得见。
 */
function inMCPOnlyFunc(src, idx) {
  const before = src.slice(0, idx)
  const at = before.lastIndexOf('\nfunc ')
  if (at < 0) return false
  const lines = before.slice(0, at + 1).split('\n')
  for (let i = lines.length - 1; i >= 0; i--) {
    const l = lines[i].trim()
    if (l === '') continue
    if (!l.startsWith('//')) break
    if (/^\/\/\s*ops:mcp-only\b/.test(l)) return true
  }
  return false
}

function inLogCall(src, idx) {
  const before = src.slice(Math.max(0, idx - 400), idx)
  const logAt = Math.max(before.lastIndexOf('logx.'), before.lastIndexOf('log.Printf('))
  if (logAt >= 0 && !before.slice(logAt).includes('c.JSON')) return true
  // 🔴 还有一种写法：**先把 map 建好、再交给日志**。
  //
  //	detail := map[string]any{"note": "库内排序规则不统一…"}
  //	…
  //	logx.J("db", "collation_mismatch", detail)      ← 日志调用在**后面**
  //
  //	只往前看的话，这种会被当成响应文案报出来 —— 而它根本不是响应
  //	（`CheckCollations` 连 gin.Context 都没有，是启动期自检）。
  //	往后看一段：这个 map 的变量名如果紧接着被喂给日志，就不算。
  const varDecl = /(\w+)\s*:?=\s*map\[string\]any\{[^{}]*$/.exec(before)
  if (varDecl) {
    const after = src.slice(idx, idx + 1200)
    if (new RegExp(`log(?:x)?\\.\\w+\\([^)]*\\b${varDecl[1]}\\b`).test(after)) return true
  }
  return false
}

const root = resolve(process.argv[2] ?? '.')
const re = new RegExp(`"(${KEYS.join('|')})"\\s*:\\s*("(?:[^"\\\\]|\\\\.)*"|fmt\\.Sprintf\\((?:[^()]|\\([^()]*\\))*\\))`, 'g')

let bad = false
for (const [product, baseline] of BASELINE) {
  const dir = resolve(root, `${product}/backend/handlers`)
  let files
  try { files = readdirSync(dir).filter((f) => f.endsWith('.go') && !f.endsWith('_test.go')) } catch { continue }
  let count = 0
  const hits = []
  for (const f of files) {
    const src = readFileSync(join(dir, f), 'utf8')
    for (const m of src.matchAll(re)) {
      if (!CJK.test(m[2])) continue
      // 🔴 日志不算。
      //
      //	`logx.J("host_sync", "...", map[string]any{"note": "另一个副本正在同步…"})`
      //	的读者是**运维和开发**，不是终端用户 —— 那句中文正是排障时最有用的东西。
      //	不排除的话这个守卫会引导人把日志也"翻译"掉（我自己就差点改了两处），
      //	结果是英文界面没变好，日志反而变成了一串看不懂的 key。
      if (inLogCall(src, m.index)) continue
      // 标了 ops:mcp-only 的函数：读者只有 AI，翻译它没有意义
      if (inMCPOnlyFunc(src, m.index)) continue
      // 🔴 有配对的 `<key>_key` 就不算：界面读 key（可翻译），
      //	中文那句是留给 MCP / 直接调 API 的人的，删掉反而是倒退。
      if (hasPairedKey(src, m.index, m[1])) continue
      count++
      hits.push(`${f}:${src.slice(0, m.index).split('\n').length}  ${m[2].slice(0, 56)}`)
    }
  }
  if (count > baseline) {
    bad = true
    console.error(`✗ ${product}: 响应里的中文文案从 ${baseline} 增到了 ${count} 处。\n`)
    console.error('  新增的用户可见文案必须走 httpx.Fail(c, code, cause, params) 或 FailKey，')
    console.error('  由前端按 message_key 翻译。参考 internal/httpx/errors.go 的 APIError 注释。\n')
    for (const h of hits.slice(-8)) console.error(`    ${h}`)
  } else if (count < baseline) {
    bad = true
    console.error(`✗ ${product}: 存量已降到 ${count} 处（基线还写着 ${baseline}）。`)
    console.error(`  把基线改成 ${count} —— 否则这 ${baseline - count} 的差值就是白送的额度。`)
  } else {
    console.log(`✓ check-response-chinese：${product} 存量未超基线（${count}）`)
  }
}
process.exit(bad ? 1 : 0)
