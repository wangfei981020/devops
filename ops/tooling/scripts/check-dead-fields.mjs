#!/usr/bin/env node
/**
 * 守卫：`queries.ts` 里声明并映射了、却**从来没有人读**的字段。
 *
 * # 它抓的是哪一类缺陷
 *
 * `check-field-usage` 抓的是「后端返回了、前端源码里搜不到这个名字」。
 * 但本仓最常见的形态**躲得过它**：
 *
 *   queries.ts   podIp: string            ← 类型声明了
 *                podIp: raw.pod_ip ?? ''  ← 映射也取了
 *   columns.tsx  （零处引用）              ← 就是没人渲染
 *
 * 字段名在源码里明明存在，`check-field-usage` 一路绿灯。
 * 而界面上那一列**不存在** —— 旧版 CMDB 的 Pod 表有 IP 列，新版没有。
 *
 * 🔴 这是最坏的一种"接了一半"：**看代码像接好了，看界面才发现没有**。
 * 复查的人打开 queries.ts 看到字段在，就不会再往下查了。
 *
 * # 判据
 *
 * 对 `routes/​*​/queries.ts` 里每个导出 interface 的顶层字段，
 * 要求全产品源码里至少有一处**读**它：
 *
 *   属性访问   `x.podIp`
 *   解构       `const { podIp } = ...`
 *
 * ⚠️ 只算"读"，不算"写"。`podIp: raw.pod_ip` 是映射不是使用 ——
 * 把它算成使用，这个守卫就退化成 check-field-usage 了，一条都抓不到。
 *
 * # 三类必须容忍的合法不引用
 *
 * 1. **请求体类型**（`*Input` / `*Params` / `*Payload` / `*Req`）：
 *    它们由表单 state 整体展开送出去，字段天然不会被逐个读。
 * 2. **伴生文案已在显示**：后端同时给了数字和人话
 *    （`filtered_out` 与 `empty_reason`），界面显示人话那份是对的。
 *    ⚠️ 但这必须**逐条确认**，不能整类豁免 —— "有个字符串在显示"
 *    不等于"那个字符串包含了这个数字"。
 * 3. **纯排序/内部键**（`sort_order`、`src_ci_id`）：后端拿它排好序，
 *    前端照单渲染。
 *
 * 所有豁免写进 ALLOW，**必须带理由**。
 */

import { existsSync, readFileSync, readdirSync, statSync } from 'node:fs'
import { join, relative } from 'node:path'
import { ROOT, products } from './lib/products.mjs'

function walk(dir, out = []) {
  if (!existsSync(dir)) return out
  for (const name of readdirSync(dir)) {
    if (name === 'node_modules' || name === 'dist') continue
    const p = join(dir, name)
    if (statSync(p).isDirectory()) walk(p, out)
    else if (/\.tsx?$/.test(name)) out.push(p)
  }
  return out
}

/**
 * 已知的不引用。键是 `产品/模块  接口.字段`。
 *
 * 🔴 `→ 建档` 那一档修完必须删。一条修完却留在挂账里的记录，
 * 等于把那个位置永久豁免了（OPSCMDB-034 的教训）。
 */
const ALLOW = new Map(
  Object.entries({
    // ── 纯排序 / 内部键：后端排好序，前端照单渲染 ──
    'ops-cmdb/basic  Env.sort_order': '排序键，后端已按它排好',
    'ops-cmdb/basic  Project.sort_order': '排序键，后端已按它排好',
    'ops-cmdb/cdn  CdnVendor.sort_order': '排序键，后端已按它排好',
    'ops-cmdb/relations  Relation.src_ci_id': '内部主键，界面显示的是 src_name',
    'ops-cmdb/relations  Relation.dst_ci_id': '内部主键，界面显示的是 dst_name',

    // ── 后端同时给了「数字」和「含这个数字的整句话」，界面显示后者 ──
    // ⚠️ 判定这一类时必须**逐条确认那句话里真的有这个数字** ——
    //	"有个字符串在显示"不等于"那个字符串包含了这个数字"（OPSCMDB-038 的原话）。
    //	下面每一条都回去读过后端的拼串代码。
    'ops-cmdb/dns  DnsConsistency.unknown_ns_domains':
      'authority_incomplete 那句话开头就是这个数字（handlers/cloud_iam_dns.go:371 的 strconv.Itoa(unknownNS)），界面渲染整句',
    'ops-cmdb/domains  RenewOneResult.ledger_saved':
      '后端在 !ledger_saved 时**恰好且仅**设置 warning（handlers/domain_renew.go:106），而 warning 已在 DomainOpsDialog 渲染。⚠️ 这条是钱的问题（已扣费、台账没写上），改动后端那个条件时必须回来重判',
    'ops-cmdb/basic  LifecycleStatus.color':
      '生命周期状态的颜色。字典页用统一的 chip 渲染（code + 显示名），没有给每项单独上色的位置；而只为这一个字典加一套配色，会让四个字典块长得不一样、反而更难扫。⚠️ 与环境的 tag_type 不同：那个已经接了，因为「生产标红」有实际防误操作价值，这个没有',
    'ops-cmdb/pipelines  DevOpsProject.configmaps':
      'DevOps 项目下的 ConfigMap 数量。流水线页回答的是「构建跑成没跑成」，ConfigMap 数与那个问题无关；要看配置去命名空间页',
    'ops-cmdb/overview  DashboardCounts.domain_total':
      '首页家底盘点已显示域名/证书数，取自 /api/overview 的 inventory 且口径更准（排除 ignored）。两份口径不同的数字摆一起会让人算不平账 —— 已从前端类型里删掉，见 queries.ts 的注释',
    'ops-cmdb/audit  AuditChange.seq':
      '后端已按 seq 排好序返回，界面按顺序渲染即可；把序号也显示出来只是噪音',
    'ops-cmdb/cdn  CdnTrafficResult.realtime':
      '后端恒为 true（cdn_events.go 直接写死），是"这条走的是实时接口不是快照"的内部标记，不是给人看的状态',
    'ops-cmdb/cdn  CdnZone.zone_id':
      'CF 内部 id，界面用 zone 名定位；显示一串 32 位 hex 只会挤掉真正有用的列。要拿它去 CF 控制台时从接口响应里取',
    'ops-cmdb/cloudaccounts  CloudAccount.billing_export_dataset':
      'BigQuery 账单导出数据集名，是**采集配置**不是资产属性；配置在云账号编辑弹窗里改，列表页不显示',
    'ops-cmdb/pipelines  PipelineRun.end':
      '流水线结束时刻。界面显示的是耗时（更直接），而耗时就是由它算出来的',
    'ops-cmdb/obsendpoints  LabelNamesResult.all_count':
      'note 那句话里就有这个数（handlers/obs_endpoints.go:695 的 itoa(len(parsed.Data))），界面把 note 整句渲染成 labelHint',
    'ops-cmdb/eventcenter  EventCenterResult.raw_total':
      'raw_total 与 merged_away 是同一件事的两面（raw_total = total + merged_away），界面显示的是「合并掉了 N 条」那一句，人要的是后者',

    // ── 其余 50 条：2026-08-18「逐页面对比旧版」查出，已建档 OPSCMDB-038 ──
    // ⚠️ 这些**不是**豁免，是挂账。逐条判定与修复计划见档案。
  }),
)

/** 建档挂账：OPSCMDB-038 待逐条判定的那批。修一条删一条。 */
const FILED = new Set([
])

const scopes = process.argv.slice(2)
const ALL = products()
const TARGETS = scopes.length ? ALL.filter((p) => scopes.includes(p)) : ALL
if (scopes.length && TARGETS.length === 0) {
  console.error(`✗ check-dead-fields: 没有匹配的产品（传入 ${scopes.join(', ')}）`)
  process.exit(1)
}

const fresh = []
const stale = []
const notCovered = []
let checked = 0
let carried = 0

for (const prod of TARGETS) {
  const feDir = join(ROOT, prod, 'frontend/src')
  if (!existsSync(feDir)) continue
  const files = walk(feDir)
  const src = Object.fromEntries(files.map((f) => [f, readFileSync(f, 'utf8')]))
  const queryFiles = files.filter((f) => /queries\.ts$/.test(f))

  // ⚠️ 一个产品**一个 queries.ts 都没扫到**是可疑的，不能当正常情况跳过。
  //	扫源码的守卫必须能说出「我什么都没看到」，否则它的绿字毫无意义
  //	（check-read-coverage 就是因为缺这个兜底，让 ops-version 静默通过过）。
  //
  // 🔴 但"说出来"和"拦下来"要分开：
  //	被点名的产品（`check-dead-fields.mjs ops-cmdb`）扫不到 → 拦，那是它自己的构建
  //	顺带扫到的产品（不带参数的全量跑）扫不到 → 只报，别拦
  //	不分这一层的话，ops-alert 用别的取数写法就会把**别人的**构建打挂 ——
  //	而"守卫拦了跟自己无关的东西"是让人开始整体忽略守卫的最快方式。
  if (files.length > 0 && queryFiles.length === 0) {
    const msg =
      `${prod} 有 ${files.length} 个前端源文件，但一个 queries.ts 都没扫到 —— ` +
      `本守卫对它实际上没有生效（它可能用了别的取数写法）。`
    if (scopes.includes(prod)) {
      console.error(`✗ check-dead-fields: ${msg}\n  要么改判据，要么在这里显式说明为什么不适用。`)
      process.exit(1)
    }
    notCovered.push(msg)
    continue
  }

  const seen = new Set()

  /**
   * 字段名 → 声明了它的模块集合。
   *
   * 🔴 为什么需要这张表：下面判「有没有人读」时扫的是**全部**前端文件。
   *	前向判定（这个字段是不是死的）这样保守是对的 ——
   *	字段常常在共享组件里被读，只扫本模块会把活字段误报成死的。
   *
   *	但**反向**判定（豁免名单是不是过期了）会因此误报：
   *	`tasks/Task.notify_enabled` 至今无人读，却因为 `cron/SettingsDialog.tsx`
   *	里读了**另一个接口**的同名字段，被判成"已经接上了"。
   *	照着删掉豁免，下一轮它又会被报成新问题 —— 守卫开始自相矛盾。
   *
   *	所以：字段名只在一个模块里出现时，全局证据可信；
   *	重名时，反向证据必须来自**同一个模块**。两个方向都保持保守。
   */
  const fieldOwners = new Map()
  for (const qf of queryFiles) {
    const mod = relative(join(ROOT, prod, 'frontend/src/routes'), qf).replace(/\/queries\.ts$/, '')
    for (const m of src[qf].matchAll(/export interface (\w+)\s*\{([\s\S]*?)\n\}/g)) {
      if (/(Input|Params|Payload|Req)$/.test(m[1])) continue
      for (const fm of m[2].matchAll(/^\s{2}(\w+)\??:/gm)) {
        if (!fieldOwners.has(fm[1])) fieldOwners.set(fm[1], new Set())
        fieldOwners.get(fm[1]).add(mod)
      }
    }
  }

  for (const qf of queryFiles) {
    const s = src[qf]
    const mod = relative(join(ROOT, prod, 'frontend/src/routes'), qf).replace(/\/queries\.ts$/, '')
    for (const m of s.matchAll(/export interface (\w+)\s*\{([\s\S]*?)\n\}/g)) {
      const [full, iface, body] = m
      // 请求体类型：由表单 state 整体展开送出，字段天然不会被逐个读
      if (/(Input|Params|Payload|Req)$/.test(iface)) continue
      for (const fm of body.matchAll(/^\s{2}(\w+)\??:/gm)) {
        const field = fm[1]
        checked++
        const key = `${prod}/${mod}  ${iface}.${field}`
        seen.add(key)

        // 只算「读」：属性访问与解构。映射写入（`podIp: raw.pod_ip`）不算 ——
        // 算上它这个守卫就退化成 check-field-usage，一条都抓不到。
        //
        // ⚠️ 已知盲点（前向方向）：这里扫的是**全部**前端文件，
        //	所以字段名重名时，A 模块读了自己的字段，会让 B 模块的同名死字段
        //	也显得"有人读"。实测例子：`tasks/Task.notify_enabled` 至今无人读，
        //	但 `cron/SettingsDialog.tsx` 读了 `ScheduledTask.notify_enabled`，
        //	于是它永远不会被报出来。
        //
        //	为什么不收紧：跨模块读是**合法**的（hostrecords 页读 cdn 模块的
        //	CdnVendor.name），限制成同模块会把这类活字段误报成死的 ——
        //	而误报比漏报更坏，它让人开始整体忽略守卫。
        //	反向（豁免过期）那一侧已经按 fieldOwners 收紧了，那里保守的方向相反。
        const access = new RegExp(`\\.\\s*${field}\\b`)
        const destructure = new RegExp(`\\{[^{}]*\\b${field}\\b[^{}]*\\}\\s*(?::|=)`)
        let used = false
        let usedInOwnModule = false
        const modDir = join(ROOT, prod, 'frontend/src/routes', mod) + '/'
        for (const f of files) {
          // 自己这份 queries.ts 里，把本 interface 的声明块挖掉再看
          const text = f === qf ? s.slice(0, m.index) + s.slice(m.index + full.length) : src[f]
          if (access.test(text) || destructure.test(text)) {
            used = true
            if (f.startsWith(modDir)) {
              usedInOwnModule = true
              break
            }
          }
        }
        if (used) {
          // 反向判定收紧：字段名在多个模块里重名时，只认本模块内的证据。
          // 理由见上面 fieldOwners 的注释 —— 否则会让人去删一个仍然需要的豁免。
          const ambiguous = (fieldOwners.get(field)?.size ?? 1) > 1
          if ((ALLOW.has(key) || FILED.has(key)) && (!ambiguous || usedInOwnModule)) {
            stale.push(key)
            continue
          }
          if (ALLOW.has(key) || FILED.has(key)) {
            carried++
          }
          continue
        }
        if (ALLOW.has(key) || FILED.has(key)) {
          carried++
          continue
        }
        fresh.push({ key, file: relative(ROOT, qf), iface, field })
      }
    }
  }
}

if (stale.length > 0) {
  console.error('✗ check-dead-fields: 下面这些已经接上了，但还挂在名单里：\n')
  for (const k of stale) console.error(`    ${k}`)
  console.error(`
接上了就把它从 ALLOW / FILED 里删掉。
留着等于把那个位置**永久豁免** —— 下次它又被删掉时，守卫不会响。`)
  process.exit(1)
}

if (fresh.length > 0) {
  console.error('✗ check-dead-fields: 声明并映射了、却从来没有人读的字段：\n')
  for (const p of fresh) {
    console.error(`    ${p.file}  ${p.iface}.${p.field}`)
  }
  console.error(`
这类缺陷**看代码像接好了**：类型里有、映射里取了，
只有打开界面才发现那一列根本不存在（旧版 CMDB 的 Pod IP 列就是这么丢的）。

三条出路，选一条：
  · 真该显示 → 渲染它
  · 后端本来就不该给 → 从类型和映射里删掉，别留着骗人
  · 有正当理由不显示 → 加进 ALLOW，**写清楚理由**

⚠️ 「暂时没空做」不是理由。那种要先建档，再挂进 FILED 并注明档号。`)
  process.exit(1)
}

for (const m of notCovered) console.log(`⊘ check-dead-fields: ${m}`)
console.log(
  `✓ check-dead-fields: ${checked} 个字段都有人读` +
    `（挂账 ${carried} 条，范围 ${scopes.length ? scopes.join(', ') : '全部产品'}）`,
)
