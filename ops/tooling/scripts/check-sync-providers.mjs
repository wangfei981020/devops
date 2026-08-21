#!/usr/bin/env node
/**
 * 守卫：前端的「哪些注册商不能同步」必须和后端的 `SyncSupported` 一致。
 *
 * # 为什么
 *
 * 后端 `dnsource.SyncSupported` 是唯一的事实：只有它列出的厂商真的有同步实现。
 * 前端另有一份 `NO_SYNC_PROVIDERS` 常量，用来在选中厂商时提示
 * 「该厂商还没有同步实现」。
 *
 * 两份必然分叉，而分叉的表现**极其安静**：
 *
 *   前端少列一个 → 用户选了它，界面不提示，保存后显示「已启用」，
 *                  而域名到期日**永远不会更新** —— 没有任何报错，
 *                  只表现为"这个注册商下的域名看起来一切正常"
 *   前端多列一个 → 一个能同步的厂商被警告成不能同步，用户不敢用
 *
 * 前者正是这个产品反复在修的那类缺陷：失败伪装成正常。
 *
 * # ⚠️ 为什么必须是守卫
 *
 * `dnsource.go` 里那段注释已经写明白了：白名单一度有 5 个厂商而实现只有 1 个，
 * 选了其余几个会保存成功、显示「已启用」、同步时才找不到实现。
 * 那段风险**在注释里被明确警告过**，但没有做对齐校验，于是它照样发生了。
 *
 * 「警告不能替代校验」—— 这条守卫就是那次教训的落地。
 */

import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { ROOT } from './lib/products.mjs'

const backendFile = join(ROOT, 'ops-cmdb/backend/dnsource/dnsource.go')
const handlerFile = join(ROOT, 'ops-cmdb/backend/internal/api/inventory/registrar/handler.go')
const frontFile = join(ROOT, 'ops-cmdb/frontend/src/routes/registrars/queries.ts')

const fail = (msg) => {
  console.error(`✗ ${msg}`)
  process.exit(1)
}

let backendSrc
try {
  backendSrc = readFileSync(backendFile, 'utf8')
} catch {
  // 这个守卫只对 ops-cmdb 有意义。文件不在就安静跳过（别的产品跑构建时不该被它挡住）
  console.log('✓ check-sync-providers: 未找到 ops-cmdb 的 dnsource.go，跳过')
  process.exit(0)
}

// 取 SyncSupported 函数体里的 case 分支
const fnMatch = backendSrc.match(/func SyncSupported\(provider string\) bool \{([\s\S]*?)\n\}/)
if (!fnMatch) fail('后端找不到 SyncSupported —— 它改名或被删了，这条守卫要跟着改')
const supported = new Set(
  [...fnMatch[1].matchAll(/case\s+((?:"[a-z0-9_-]+"\s*,?\s*)+):/g)].flatMap((m) =>
    [...m[1].matchAll(/"([a-z0-9_-]+)"/g)].map((x) => x[1]),
  ),
)
if (supported.size === 0) fail('后端 SyncSupported 里一个 case 都没解析到 —— 解析规则失效了')

// 取白名单（界面下拉里能选到的全部厂商）
const handlerSrc = readFileSync(handlerFile, 'utf8')
const whitelistMatch = handlerSrc.match(/var Providers = map\[string\]string\{([\s\S]*?)\n\}/)
if (!whitelistMatch) fail('后端找不到 Providers 白名单')
const whitelist = [...whitelistMatch[1].matchAll(/"([a-z0-9_-]+)":/g)].map((m) => m[1])

// "other" 是「仅登记、不自动同步」的明确选择，本来就不该被警告成"还没实现"
const expectedNoSync = whitelist.filter((p) => p !== 'other' && !supported.has(p)).sort()

const frontSrc = readFileSync(frontFile, 'utf8')
const frontMatch = frontSrc.match(/NO_SYNC_PROVIDERS[^=]*=\s*\[([^\]]*)\]/)
if (!frontMatch) fail('前端找不到 NO_SYNC_PROVIDERS')
const actual = [...frontMatch[1].matchAll(/'([a-z0-9_-]+)'/g)].map((m) => m[1]).sort()

const missing = expectedNoSync.filter((p) => !actual.includes(p))
const extra = actual.filter((p) => !expectedNoSync.includes(p))

if (missing.length > 0 || extra.length > 0) {
  console.error('✗ 前端的 NO_SYNC_PROVIDERS 和后端的 SyncSupported 对不上：\n')
  if (missing.length > 0) {
    console.error(`  前端漏了：${missing.join(', ')}`)
    console.error(
      '  🔴 后果最严重的一种：用户选了它，界面不提示，保存后显示「已启用」，',
    )
    console.error('     而域名到期日**永远不会更新** —— 没有任何报错。\n')
  }
  if (extra.length > 0) {
    console.error(`  前端多了：${extra.join(', ')}`)
    console.error('  这几个后端其实支持同步，却被警告成"还没实现"，用户不敢用。\n')
  }
  console.error(`  后端能同步的：${[...supported].sort().join(', ')}`)
  console.error(`  下拉里能选的：${whitelist.sort().join(', ')}`)
  console.error(`\n改 ${'ops-cmdb/frontend/src/routes/registrars/queries.ts'} 里的 NO_SYNC_PROVIDERS。`)
  process.exit(1)
}

console.log(
  `✓ 注册商同步能力前后端一致（后端支持 ${[...supported].sort().join(', ')}，前端警告 ${
    actual.length ? actual.join(', ') : '无'
  }）`,
)
