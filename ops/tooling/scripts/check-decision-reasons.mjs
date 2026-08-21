#!/usr/bin/env node
/**
 * 判定原因码 ↔ 语言包的契约检查。
 *
 * 门户上每个进不去的应用都会显示"为什么进不去"，那句话是拿后端返回的
 * reason 去语言包里取的。少一条，界面上就**原样显示一个生 key**
 * （实际撞到过：卡片上写着 `reason.rule`）。
 *
 * 它只在"恰好有人被那条规则拒绝"时才暴露 —— 也就是最不该出错的时候。
 *
 * 反向也查：语言包里有、后端从不返回的，是死文案
 * （撞到过 `policy_deny` / `need_approval` 两条，后端根本没这两个值）。
 */

import { readFileSync, readdirSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const ROOT = join(dirname(fileURLToPath(import.meta.url)), '../..')
const SRC_DIR = join(ROOT, 'ops-sso/backend/internal/domain/access')
const LOCALES = ['zh-CN', 'en-US']

/** 只认 `Reason: "xxx"` 和 `Reason = "xxx"` 这两种赋值，注释里的举例不算。 */
const RE = /Reason\s*[:=]\s*"([a-z_]+)"/g

function stripGoComments(src) {
  return src.replace(/\/\*[\s\S]*?\*\//g, '').replace(/^[ \t]*\/\/.*$/gm, '')
}

const reasons = new Set()
let scanned = 0
for (const f of readdirSync(SRC_DIR)) {
  if (!f.endsWith('.go') || f.endsWith('_test.go')) continue
  scanned++
  const text = stripGoComments(readFileSync(join(SRC_DIR, f), 'utf8'))
  for (const m of text.matchAll(RE)) reasons.add(m[1])
}
if (scanned === 0 || reasons.size === 0) {
  console.error(`✗ 在 ${SRC_DIR} 里一个 reason 都没抽到 —— 正则或目录结构变了，
   这时候「检查通过」毫无意义`)
  process.exit(1)
}

const problems = []
for (const locale of LOCALES) {
  const sso = JSON.parse(readFileSync(join(ROOT, 'packages/i18n/locales', locale, 'sso.json'), 'utf8'))
  const declared = sso.reason ?? {}
  for (const r of reasons) {
    const v = declared[r]
    if (v === undefined) problems.push(`${locale}: 缺 reason.${r}`)
    else if (typeof v !== 'string' || v.trim() === '') problems.push(`${locale}: reason.${r} 是空文案`)
  }
  for (const k of Object.keys(declared)) {
    if (!reasons.has(k)) problems.push(`${locale}: reason.${k} 后端从不返回（死文案，可删）`)
  }
}

if (problems.length > 0) {
  console.error(`✗ 判定原因码与语言包不一致，共 ${problems.length} 处：\n`)
  for (const p of problems) console.error(`  ${p}`)
  console.error('\n后端加判定原因时要同步在 sso.json 的 reason 下补文案。')
  console.error('漏了的话，用户会在门户卡片上看到生的 key —— 而那只在他真被拒绝时才出现。')
  process.exit(1)
}
console.log(`✓ 后端 ${reasons.size} 个判定原因在 ${LOCALES.length} 种语言里都有文案`)
