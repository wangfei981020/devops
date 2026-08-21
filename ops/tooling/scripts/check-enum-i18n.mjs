#!/usr/bin/env node
/**
 * 后端枚举值必须在语言包里有对应文案。
 *
 * 前端常用 t(`ns:status.${d.status}`) 这种**动态拼接**取文案，
 * 而 check-i18n-usage 明确跳过含 ${} 的 key —— 静态查不了。
 * 于是「后端加了个枚举值、前端没补文案」不会被任何守卫抓到，
 * 界面上直接显示 "status.not_activated" 这串字母。
 *
 * 实测撞到过：licensekit 有 7 个 Status，我按 4 个写文案且键名还写错了
 * （inactive vs not_activated），授权页上直接露出两串 key。
 *
 * ⚠️ 为什么不用 Go 测试写这条：
 * 试过，逻辑没问题，但 **go test 的缓存只感知代码变化，不知道外部 JSON 变了** ——
 * 删掉一条文案再跑，它照常返回 ok（要 -count=1 才会真跑）。
 * 一条会骗人的绿色比没有这条测试更糟，所以挪到构建守卫里，每次都真跑。
 */
import { existsSync, readFileSync } from 'node:fs'
import { resolve } from 'node:path'

const repo = resolve(new URL('../..', import.meta.url).pathname)

/**
 * 要对齐的枚举。
 *
 * source 是**权威定义**所在（Go 常量），target 是语言包里的位置。
 * 加新枚举时两边都要登记 —— 只登记一边不会报错，只会漏检。
 */
const CHECKS = [
  {
    product: 'ops-video-manager',
    name: 'license status',
    // licensekit 的 Status 常量
    source: 'ops-kit/licensekit/payload.go',
    pattern: /Status\w+\s+Status\s*=\s*"([a-z_]+)"/g,
    ns: 'license',
    paths: ['status', 'statusHint'],
    // 除了语言包，前端还有一张 tone 映射表。漏一个不会报错，
    // 只会让那个状态落到默认色 —— `lapsed`（续期已救不回来）
    // 看起来和「社区版」一个颜色，紧迫程度差着十万八千里。
    toneMap: {
      file: 'frontend/src/routes/License.tsx',
      varName: 'STATUS_TONE',
    },
  },
]

let failed = false

for (const c of CHECKS) {
  const src = resolve(repo, c.source)
  if (!existsSync(src)) {
    console.error(`✗ check-enum-i18n: 找不到枚举定义 ${c.source}`)
    failed = true
    continue
  }
  const values = [...readFileSync(src, 'utf8').matchAll(c.pattern)].map((m) => m[1])
  if (values.length === 0) {
    // 🔴 解析出 0 个必须报错，不能当"没问题"跳过 —— 那正是假绿色的来源
    console.error(`✗ check-enum-i18n: 从 ${c.source} 一个 ${c.name} 都没解析出来，多半是写法变了`)
    failed = true
    continue
  }

  // tone 映射表
  if (c.toneMap) {
    const tp = resolve(repo, c.product, c.toneMap.file)
    if (!existsSync(tp)) {
      console.error(`✗ check-enum-i18n: 找不到 ${c.toneMap.file}`)
      failed = true
    } else {
      const src2 = readFileSync(tp, 'utf8')
      const block = src2.slice(src2.indexOf(`const ${c.toneMap.varName}`))
      const body = block.slice(0, block.indexOf('\n}'))
      const missing = values.filter((v) => !new RegExp(`\\b${v}\\s*:`).test(body))
      if (missing.length) {
        failed = true
        console.error(
          `✗ check-enum-i18n: ${c.product} 的 ${c.toneMap.varName} 缺 ${missing.length} 个 ${c.name}（会落到默认色）：`,
        )
        for (const m of missing) console.error(`    ${m}`)
      }
    }
  }

  for (const loc of ['zh-CN', 'en-US']) {
    const p = resolve(repo, c.product, `frontend/src/locales/${loc}/${c.ns}.json`)
    if (!existsSync(p)) {
      console.error(`✗ check-enum-i18n: 找不到语言包 ${p.replace(repo + '/', '')}`)
      failed = true
      continue
    }
    const bundle = JSON.parse(readFileSync(p, 'utf8'))
    for (const path of c.paths) {
      const node = bundle[path] ?? {}
      const missing = values.filter((v) => typeof node[v] !== 'string')
      if (missing.length) {
        failed = true
        console.error(
          `✗ check-enum-i18n: ${c.product} ${loc} 的 ${c.ns}.${path} 缺 ${missing.length} 个 ${c.name}：`,
        )
        for (const m of missing) console.error(`    ${m}`)
      }
    }
  }
}

if (failed) {
  console.error('\n界面会把缺失的 key 原样渲染出来。补上文案，或确认那个枚举值不会出现在界面上。')
  process.exit(1)
}

console.log(`✓ check-enum-i18n: ${CHECKS.length} 组后端枚举都有中英文案`)
