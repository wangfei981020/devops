#!/usr/bin/env node
/**
 * 审计变更 diff 的键名，前后端必须一致。
 *
 * # 为什么专门守这一处
 *
 * 后端 `handlers/audit_record.go` 产出的是 `{"old": …, "new": …}`，
 * 而前端读的是 `v.before` / `v.after`。类型上两边都是可选字段，
 * TypeScript 不报错、运行时不抛异常 —— 只是每次取值都得到 undefined。
 *
 * 结果：**每一条字段变更都渲染成「— → —」**。
 * 数据在库里好好躺着，界面上却把「把项目名从 A 改成 B」显示成「从无到无」。
 *
 * ⚠️ 这比"没有详情"更坏：没有详情时人会去查库，
 * 显示成「— → —」时人会以为这条记录本来就没值。
 *
 * 而审计的全部意义就是"事后能说清当时改了什么"。
 *
 * # 判据
 *
 * 后端：找 diff map 里出现的键名字面量。
 * 前端：找 AuditChange.diff 的类型声明里出现的键名。
 * 两边取到的集合必须相同。
 */
import { existsSync, readFileSync, readdirSync, statSync } from 'node:fs'
import { join, relative } from 'node:path'
import { ROOT } from './lib/products.mjs'

/** 递归收集 ts/tsx。⚠️ 必须扫全前端，理由见 FRONTEND_DIR 的说明。 */
function walk(dir, out = []) {
  if (!existsSync(dir)) return out
  for (const name of readdirSync(dir)) {
    if (name === 'node_modules' || name === 'dist') continue
    const p = join(dir, name)
    if (statSync(p).isDirectory()) walk(p, out)
    else if (/\.(ts|tsx)$/.test(name)) out.push(p)
  }
  return out
}

const BACKEND = join(ROOT, 'ops-cmdb/backend/handlers/audit_record.go')
/**
 * 🔴 扫**整个前端**，不是钉死某一个文件。
 *
 * 这个守卫原本只盯 `routes/audit/queries.ts` —— 而 diff 的类型声明
 * 完全可以出现在别的文件里。实测：新建的 `components/ObjectHistory.tsx`
 * 里又写了一遍 `{ before, after }`，**守卫一声没吭**，
 * 界面上每条字段变更再次渲染成「— → —」。
 *
 * ⚠️ 同一个 bug，同一个守卫，第二次发生 —— 差别只是换了个文件。
 * 一个只看固定路径的守卫，防的是"改坏这一处"，防不住"在别处再写一份"。
 * 而"在别处再写一份"恰恰是这个仓库里最常见的引入方式。
 */
const FRONTEND_DIR = join(ROOT, 'ops-cmdb/frontend/src')

if (!existsSync(BACKEND) || !existsSync(FRONTEND_DIR)) {
  console.log('✓ 审计 diff 键名：跳过（文件不存在）')
  process.exit(0)
}

// 后端：out[k] = map[string]interface{}{"old": …, "new": …}
const beSrc = readFileSync(BACKEND, 'utf8')
const beKeys = new Set()
for (const m of beSrc.matchAll(/map\[string\]interface\{\}\{([^}]*)\}/g)) {
  for (const k of m[1].matchAll(/"([a-z_]+)"\s*:/g)) beKeys.add(k[1])
}

// 前端：diff: Record<string, { old?: unknown; new?: unknown }> | null
// 全前端逐个文件找，**每一处声明都要比对**
const decls = []
for (const f of walk(FRONTEND_DIR)) {
  const src = readFileSync(f, 'utf8')
  for (const m of src.matchAll(/diff\??:\s*Record<string,\s*\{([^}]*)\}/g)) {
    decls.push({ file: relative(ROOT, f), body: m[1] })
  }
}
if (decls.length === 0) {
  console.error('✗ 审计 diff 键名：前端找不到任何 diff 的类型声明')
  console.error('  这个守卫靠它比对，声明改形状了就要同步改这里的正则。')
  process.exit(1)
}

let bad = false
for (const d of decls) {
  const feKeys = new Set([...d.body.matchAll(/([a-zA-Z_]+)\??\s*:/g)].map((m) => m[1]))
  // ⚠️ 不要给任何键开豁免。
  //	我第一版在这里写了 `feKeys.delete('changed')`，理由是"它是特殊形状"——
  //	但后端**确实产出**它（敏感字段：只说改过、不给值），
  //	前端不读就等于把「凭据被改过」渲染成「—→—」，看起来像没改。
  //	给守卫开豁免之前先问：后端到底产不产这个键。产就得读。
  const missingInFe = [...beKeys].filter((k) => !feKeys.has(k))
  const extraInFe = [...feKeys].filter((k) => !beKeys.has(k))
  if (missingInFe.length === 0 && extraInFe.length === 0) continue
  if (!bad) console.error('✗ 审计变更 diff 的键名前后端对不上：\n')
  bad = true
  console.error(`  ${d.file}`)
  console.error(`    后端产出：{ ${[...beKeys].sort().join(', ')} }   (audit_record.go)`)
  console.error(`    前端读取：{ ${[...feKeys].sort().join(', ')} }`)
  if (missingInFe.length > 0) console.error(`    前端没读：${missingInFe.join(', ')}`)
  if (extraInFe.length > 0) console.error(`    前端多读（永远是 undefined）：${extraInFe.join(', ')}`)
}

if (bad) {
  console.error(
    '\n  ⚠️ 这类错不会报错也不会白屏：取到 undefined，' +
      '\n  于是每条字段变更都渲染成「— → —」——「改了名字」被显示成「从无到无」。' +
      '\n  而审计的全部意义就是事后能说清当时改了什么。',
  )
  process.exit(1)
}

console.log(
  `✓ 审计 diff 键名一致：{ ${[...beKeys].sort().join(', ')} }（比对了 ${decls.length} 处声明）`,
)
