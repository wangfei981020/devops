#!/usr/bin/env node
/**
 * 守卫：后端发的语言包 key 必须**真的能取到文案**。
 *
 * # 为什么
 *
 * 后端往响应里放 `hint_key` / `msg_key` / `note_key` / `error_key`，前端拿去 `t(key)`。
 * 取不到时 i18next 的 `parseMissingKeyHandler` 会**把 key 原样显示出来** ——
 * 界面上就是一行 `pipelines.pickProjectForRuns`（实测撞到，OPSCMDB-054 迁移过程中）。
 *
 * 🔴 最容易写错的是**命名空间分隔符**：
 *
 *	error.xxx                ✅ common 是 defaultNS，`error` 只是它下面的一个对象
 *	pipelines.pickProject…   ❌ pipelines 是**另一个命名空间**，要写成 `pipelines:pickProject…`
 *
 *	两种写法长得几乎一样，而错的那种**不报错**：它安静地把 key 印在界面上，
 *	只在那条分支真的走到时才暴露 —— 正常测试根本走不到。
 *
 * # 判据
 *
 * 后端源码里所有 `"*_key": "..."` 的值，逐个到语言包里查：
 *   `ns:a.b`  → locales/<lang>/<ns>.json 里的 a.b
 *   `a.b`     → locales/<lang>/common.json 里的 a.b
 * 两种语言都要有。
 *
 * ⚠️ 只认字面量。变量拼出来的 key 查不了，也不猜 —— 那种写法本身就该避免。
 */
import { existsSync, readFileSync, readdirSync, statSync } from 'node:fs'
import { join, relative } from 'node:path'
import { ROOT, backendDirs } from './lib/products.mjs'

const LOCALES = join(ROOT, 'packages/i18n/locales')
const LANGS = existsSync(LOCALES)
  ? readdirSync(LOCALES).filter((d) => statSync(join(LOCALES, d)).isDirectory())
  : []

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

const cache = new Map()
function bundle(lang, ns) {
  const k = `${lang}/${ns}`
  if (!cache.has(k)) {
    const f = join(LOCALES, lang, `${ns}.json`)
    cache.set(k, existsSync(f) ? JSON.parse(readFileSync(f, 'utf8')) : null)
  }
  return cache.get(k)
}

/** 语言包里能不能取到这个 key */
function resolves(lang, key) {
  const i = key.indexOf(':')
  const ns = i > 0 ? key.slice(0, i) : 'common'
  const path = i > 0 ? key.slice(i + 1) : key
  const b = bundle(lang, ns)
  if (!b) return false
  let cur = b
  for (const seg of path.split('.')) {
    if (cur === null || typeof cur !== 'object' || !(seg in cur)) return false
    cur = cur[seg]
  }
  return typeof cur === 'string'
}

const problems = []
let checked = 0
for (const d of backendDirs()) {
  for (const f of walkGo(join(ROOT, d))) {
    const src = readFileSync(f, 'utf8')
    for (const m of src.matchAll(/"(\w*_key)"\s*:\s*"([^"]+)"/g)) {
      const key = m[2]
      checked++
      const missing = LANGS.filter((l) => !resolves(l, key))
      if (missing.length > 0) {
        problems.push(
          `${relative(ROOT, f)}:${src.slice(0, m.index).split('\n').length}  ${key}  （${missing.join('/')} 取不到）`,
        )
      }
    }
  }
}

if (problems.length > 0) {
  console.error('✗ check-backend-i18n-keys: 后端发的 key 在语言包里取不到，界面会把它原样印出来：\n')
  for (const p of problems) console.error(`    ${p}`)
  console.error(`
⚠️ 最常见的原因是分隔符写错了：
    error.xxx              common 是 defaultNS，这样写对
    pipelines.xxx          ✗ 应为 pipelines:xxx（冒号）—— pipelines 是另一个命名空间

这类错误**不会报错**，只会安静地把 key 印在界面上。`)
  process.exit(1)
}
console.log(`✓ check-backend-i18n-keys: ${checked} 个后端下发的 key 在 ${LANGS.length} 种语言里都取得到`)
