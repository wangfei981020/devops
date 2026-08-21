#!/usr/bin/env node
/**
 * 每一处 INSERT INTO cis 都必须对"这个类型允不允许重名"表过态。
 *
 * cis 表 (type, name) 上没有唯一索引，而且**不能**加：唯一性是按 CI 类型的
 * （host/domain 同名即重复；certificate 同一个 CN 天然有多张，续期轮换期间新旧并存），
 * MySQL 又没有部分唯一索引。所以数据库永远不会兜这个底。
 *
 * 🔴 漏掉的后果不是报错，是**静默产生重复 CI** ——
 *	所有按 name 关联的逻辑（成本归属、指标匹配、拓扑连边）从此随机命中其中一条。
 *	实测：POST /api/cis 和 POST /api/domains 都能造出重复（本轮验收发现）。
 *
 * 判据：INSERT INTO cis 之前 40 行内，要么调了 ciExists（挡重复），
 * 要么有 ALLOW_DUP 标记注释（明确声明这个类型允许重名，并写明理由）。
 */
import { readFileSync, readdirSync } from 'node:fs'
import { resolve, join, basename } from 'node:path'

const root = resolve(process.argv[2] ?? '.')
const dir = resolve(root, 'ops-cmdb/backend/handlers')
/**
 * 认得下面这几种"先查后插"的写法 —— 它们都是真实的查重，不是漏网：
 *   ciExists(…)                本文新加的 helper
 *   SELECT id/ci_id FROM cis   显式查一遍（domain_sync 的写法）
 *   existing[…]                批量场景预加载成 map 再查（relations_* 的写法）
 *   ALLOW_DUP                  明确声明这个类型允许重名，必须附理由
 *
 * ⚠️ 判据放宽到这个程度是有代价的：一处代码只要恰好提到 existing[ 就会被放过。
 *	但反过来"只认 ciExists"会把 5 处正确实现全报成问题 ——
 *	而守卫误报的代价不是烦人，是人开始学着忽略它。
 */
const DEDUPED = /ciExists\(|SELECT\s+(?:id|ci_id)\s+FROM cis|existing\[|ALLOW_DUP/

const bad = []
for (const f of readdirSync(dir).filter((f) => f.endsWith('.go') && !f.endsWith('_test.go'))) {
  const lines = readFileSync(join(dir, f), 'utf8').split('\n')
  lines.forEach((line, i) => {
    if (!/INSERT INTO cis\b/.test(line)) return
    // 注释里提到这张表不算（本文件自己的说明就会命中）
    if (/^\s*(?:\/\/|\*)/.test(line)) return
    const ctx = lines.slice(Math.max(0, i - 40), i).join('\n')
    if (DEDUPED.test(ctx)) return
    bad.push(`${basename(f)}:${i + 1}`)
  })
}

if (bad.length > 0) {
  console.error(`✗ 有 ${bad.length} 处 INSERT INTO cis 既没查重也没声明允许重名：\n`)
  for (const b of bad) console.error(`  ${b}`)
  console.error('\n这个类型不允许重名 → 插入前调 ciExists（事务里要传 tx，不是 h.DB）。')
  console.error('允许重名（如证书：同一 CN 续期轮换会并存）→ 在插入上方写一条带 ALLOW_DUP 的注释说明理由。')
  process.exit(1)
}
console.log('✓ 每处 INSERT INTO cis 都已就"是否允许重名"表态')
