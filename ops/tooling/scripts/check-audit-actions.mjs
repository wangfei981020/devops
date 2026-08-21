#!/usr/bin/env node
/**
 * 审计动作码 ↔ 前端语言包的契约检查。
 *
 * 和 check-error-keys.mjs 是同一类问题，只是换了个地方漏：
 * 后端 `d.Audit.Write(… Action: "session.revoke" …)` 加了一个新动作，
 * 前端语言包没跟上 —— 审计页上就会出现一行生的 `session.revoke`。
 *
 * 而审计页恰恰是**出事那天**才有人认真看的页面，
 * 平时的点击测试根本走不到那条记录。加这个检查之前就已经漏过一次。
 *
 * 反向也查：语言包里有、代码里没有的属于死文案。
 * 那些多半是当初照着注释/测试文件扒下来的码 —— 扒的时候看着像真的。
 *
 * 用法：node tooling/scripts/check-audit-actions.mjs
 */

import { readFileSync, readdirSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const ROOT = join(dirname(fileURLToPath(import.meta.url)), '../..')
const BACKEND = join(ROOT, 'ops-sso/backend/internal')
const LOCALES = ['zh-CN', 'en-US']

/** 只认真正的写入点 `Action: "xxx.yyy"`，注释里的举例不算。 */
const ACTION_RE = /Action:\s*"([a-z_]+\.[a-z_]+)"/g

function goFiles(dir) {
  const out = []
  let entries
  try {
    entries = readdirSync(dir, { withFileTypes: true })
  } catch {
    return out
  }
  for (const e of entries) {
    if (e.name === 'vendor' || e.name === 'node_modules') continue
    const p = join(dir, e.name)
    if (e.isDirectory()) out.push(...goFiles(p))
    // 测试文件里的动作码是构造出来的假数据，不该要求补文案
    else if (e.name.endsWith('.go') && !e.name.endsWith('_test.go')) out.push(p)
  }
  return out
}

const used = new Set()
for (const f of goFiles(BACKEND)) {
  const src = readFileSync(f, 'utf8')
  for (const m of src.matchAll(ACTION_RE)) used.add(m[1])
}
if (used.size === 0) {
  console.error('✗ 一个审计动作码都没抽到 —— 写法变了，脚本要跟着改')
  process.exit(1)
}

const problems = []
for (const locale of LOCALES) {
  const sso = JSON.parse(readFileSync(join(ROOT, `packages/i18n/locales/${locale}/sso.json`), 'utf8'))
  const have = sso?.audit?.action ?? {}
  for (const a of used) {
    if (typeof have[a] !== 'string' || have[a].trim() === '') {
      problems.push(`${locale}: 缺 audit.action.${a}`)
    }
  }
  for (const k of Object.keys(have)) {
    if (!used.has(k)) {
      problems.push(`${locale}: audit.action.${k} 后端从不写入（死文案，可删）`)
    }
  }
}

if (problems.length > 0) {
  console.error(`✗ 审计动作码与语言包不一致，共 ${problems.length} 处：\n`)
  for (const p of problems) console.error(`  ${p}`)
  console.error('\n后端 d.Audit.Write 加新 Action 时，要同步在 sso.json 的 audit.action 下补文案。')
  console.error('漏了的话，审计页会直接显示生的动作码 —— 而那一页只有出事那天才有人细看。')
  process.exit(1)
}

console.log(`✓ 后端 ${used.size} 个审计动作码在 ${LOCALES.length} 种语言里都有文案`)
