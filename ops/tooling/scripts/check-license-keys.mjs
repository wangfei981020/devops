#!/usr/bin/env node
/**
 * license 状态 ↔ 前端文案的契约检查。
 *
 * 拦的是刚刚真实发生过的一件事：授权规范新增了 `lapsed` 状态，
 * 共享库实现了，而前端语言包没跟上 —— 没有任何东西会报警，
 * 直到某个客户欠费满 30 天，界面上出现一个生的 key。
 *
 * 和 check-error-keys.mjs 是同一个思路：跨语言、跨目录、跨构建流程的约定
 * 必须有机器来盯，靠人记不住。
 *
 * 用法：node tooling/scripts/check-license-keys.mjs
 */

import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const ROOT = join(dirname(fileURLToPath(import.meta.url)), '../..')
const PAYLOAD_GO = join(ROOT, 'ops-kit/licensekit/payload.go')
const LOCALES_DIR = join(ROOT, 'packages/i18n/locales')
const LOCALES = ['zh-CN', 'en-US']

/**
 * 状态 → 前端应有的文案 key。
 *
 * 不是每个状态都需要横幅：
 *   active        一切正常，不打扰用户
 *   not_activated 社区版是**正常状态不是错误**，只在「关于」页显示版本名，
 *                 顶部不挂横幅（挂了等于天天催客户买）
 * 其余每个状态都必须能对用户解释清楚"发生了什么、该做什么"。
 */
const REQUIRED = {
  active: null,
  not_activated: 'license.community',
  grace: 'license.graceBanner',
  expired: 'license.expiredBanner',
  lapsed: 'license.lapsedBanner',
  finger_mismat: 'license.fingerprintBanner',
  not_licensed: 'license.notLicensedBanner',
}

const src = readFileSync(PAYLOAD_GO, 'utf8')
const block = src.match(/const \(\n((?:\s+Status\w+\s+Status = "[^"]+".*\n)+)\)/)
if (!block) {
  console.error('✗ 没在 payload.go 里找到 Status 常量块，脚本需要跟着改')
  process.exit(1)
}
const states = [...block[1].matchAll(/Status = "([^"]+)"/g)].map((m) => m[1])

function valueAt(obj, dotted) {
  return dotted.split('.').reduce((acc, k) => (acc == null ? undefined : acc[k]), obj)
}

const problems = []

// 共享库有、映射表里没有 —— 说明新增了状态却没决定它要不要文案
for (const s of states) {
  if (!(s in REQUIRED)) {
    problems.push(
      `状态 "${s}" 是新增的，但本脚本的 REQUIRED 表里没有它。` +
        `请决定它需不需要向用户解释，然后补进映射表。`,
    )
  }
}
// 映射表里有、共享库没有 —— 状态被删了，文案是死的
for (const s of Object.keys(REQUIRED)) {
  if (!states.includes(s)) {
    problems.push(`状态 "${s}" 已从共享库移除，REQUIRED 表和对应文案可以清掉`)
  }
}

for (const locale of LOCALES) {
  const common = JSON.parse(readFileSync(join(LOCALES_DIR, locale, 'common.json'), 'utf8'))
  for (const [state, key] of Object.entries(REQUIRED)) {
    if (!key || !states.includes(state)) continue
    const v = valueAt(common, key)
    if (v === undefined) {
      problems.push(`${locale}: 状态 "${state}" 缺文案 ${key}`)
    } else if (typeof v !== 'string' || v.trim() === '') {
      problems.push(`${locale}: ${key} 是空文案`)
    }
  }
}

if (problems.length > 0) {
  console.error(`✗ license 状态与前端文案不一致，共 ${problems.length} 处：\n`)
  for (const p of problems) console.error(`  ${p}`)
  console.error('\n授权状态是最晚才会出现的一类界面（lapsed 要欠费满 30 天），')
  console.error('漏了不会有人发现，直到它出现在客户屏幕上。')
  process.exit(1)
}

const covered = Object.entries(REQUIRED).filter(([s, k]) => k && states.includes(s)).length
console.log(`✓ ${states.length} 个授权状态，其中 ${covered} 个需要文案，在 ${LOCALES.length} 种语言里都齐全`)
