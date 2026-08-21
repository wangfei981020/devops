#!/usr/bin/env node
/**
 * 写路由不许只挂"菜单可见"权限。
 *
 * 权限码分两类：
 *   menu:xxx   —— 这个账号**能不能看见**这一页
 *   cmdb:xxx   —— 这个账号**能不能执行**这个动作
 *
 * 把 menu: 挂在写路由上，等于用"看得见"当成"改得动"的判据。
 * 只读角色天然拥有一堆 menu: 权限，于是它就能写。
 *
 * 🔴 实测撞到过：`POST /api/license` 挂的是 menu:cmdb_basic，
 *	cmdb_viewer（只读角色）打过去进到了激活流程，返回"激活码无效"而不是 403 ——
 *	一个只读账号可以换掉整个系统的授权。
 *
 * 而这类错误在界面上完全看不出来：只读账号平时也看不到那个按钮，
 * 要直接打接口才会暴露。所以只能靠这里挡。
 *
 * 例外：语义上是只读、仅仅因为要传 body 才用 POST 的接口（预览、查询、导出）。
 * 加进 READ_ONLY_POST 时必须在这里写清"它为什么不产生副作用"。
 */
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

const PERM_FILE = 'ops-cmdb/backend/handlers/perm.go'

/** 语义只读、仅因需要 body 才用 POST 的接口 —— 每条都要写明凭什么断定它无副作用 */
const READ_ONLY_POST = new Map([
  // 干跑：只算"续费这批域名要花多少钱、哪些不能续"，不落库、不调注册商。
  // 审计表里也标成 none（无变更）。真正扣费的是 /domains/renew-batch。
  ['POST /api/domains/renew-batch/preview', '纯干跑，不落库不外呼'],
])

const root = resolve(process.argv[2] ?? '.')
const src = readFileSync(resolve(root, PERM_FILE), 'utf8')

const bad = []
const re = /"(POST|PUT|DELETE|PATCH) (\/api\/[^"]*)":\s*"([^"]*)"/g
for (const m of src.matchAll(re)) {
  const [, method, path, code] = m
  const key = `${method} ${path}`
  if (READ_ONLY_POST.has(key)) continue
  const codes = code.split(',').map((s) => s.trim()).filter(Boolean)
  if (codes.length > 0 && codes.every((c) => c.startsWith('menu:'))) {
    bad.push({ key, code })
  }
}

if (bad.length > 0) {
  console.error(`✗ 有 ${bad.length} 条写路由只挂了菜单可见权限（menu:），只读角色能执行：\n`)
  for (const b of bad) console.error(`  ${b.key}\n    当前: ${b.code}  → 应改成对应的 cmdb:manage_* 操作权限`)
  console.error('\n若它其实是只读语义（预览/查询/导出），加进 check-write-perm-kind.mjs 的')
  console.error('READ_ONLY_POST，并写明凭什么断定它没有副作用。')
  process.exit(1)
}
console.log(`✓ 写路由权限类型正确（${READ_ONLY_POST.size} 条只读 POST 已登记豁免）`)
