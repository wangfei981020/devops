#!/usr/bin/env node
/**
 * Prometheus 选择器的两个"不报错、只是答错"的写法。
 *
 * # 为什么要有这一道
 *
 * 这两个错都不会让任何东西变红。它们让查询**返回一个看起来很正常的数字**，
 * 或者让整条链路静默失败，而界面上一切如常 —— 这是本项目里最难发现的一类缺陷。
 * 两个都是被复制粘贴扩散开的：第一个复制了四份，各错各的。
 *
 * ## 一、mountpoint="/" 不是"节点磁盘"
 *
 * GKE 的 COS 节点上 `/` 是**只读的启动镜像**（/dev/root，ext2，仅 1.93 GB）。
 * 真正会被容器镜像 / 可写层 / emptyDir 撑满的是 /dev/sda1（980 GB），
 * 它挂在 /var/lib/kubelet、/var/lib/containerd 等十几个点上。
 *
 * 实测 g32-prod：按 `/` 算，35 个节点**全部是 74.0656%**，
 * 一模一样到小数点后 14 位、而且永远不变。也就是说
 * 磁盘水位和磁盘告警在三个 GKE 集群上**从来没有真正生效过**。
 * DEV 那次磁盘打满能发现，纯粹因为 DEV 是 k3s，`/` 恰好就是可写分区。
 *
 * 口径统一在 backend/handlers/nodefs.go（里面写了三个踩过的坑）。
 *
 * ## 二、clusterLabel 是标签名，不是选择器
 *
 * resolveEndpointFull 返回的 clusterLabel 只是标签**名**（如 `cluster`）。
 * 曾经有人写 `sel = clusterLabel + "," + sel`，拼出 `{cluster,mountpoint="/"}` ——
 * PromQL 语法错误，实测 HTTP 422。于是满盘预测告警在**所有配了 cluster_label
 * 的集群**（UAT / g32-prod / infra-01）上从来没产出过一条，
 * 只有没配 cluster_label 的 DEV 跑得动，看着像"这个功能只在 DEV 有数据"。
 *
 * 必须经 clusterSelector(db, clusterLabel, cid) 拼成 `label="value"`。
 *
 * # 判据
 *
 * 扫后端 .go：
 *   1. 出现字面量 mountpoint="/"（含 =~ 里只写 "/" 的）→ 报，除非在 nodefs.go 或 SKIP 里
 *   2. clusterLabel 参与字符串拼接 / 进 Sprintf 而不是传给 clusterSelector → 报
 */
import { existsSync, readFileSync, readdirSync, statSync } from 'node:fs'
import { join, relative, basename } from 'node:path'
import { ROOT, products } from './lib/products.mjs'

/** 允许直接写 mountpoint="/" 的地方，每条写清楚为什么。 */
const MOUNT_SKIP = [
  { file: 'nodefs.go', why: '口径本身就定义在这里' },
]

function walk(dir, out = []) {
  if (!existsSync(dir)) return out
  for (const name of readdirSync(dir)) {
    if (name === 'node_modules' || name === 'vendor') continue
    const p = join(dir, name)
    if (statSync(p).isDirectory()) walk(p, out)
    else if (p.endsWith('.go') && !p.endsWith('_test.go')) out.push(p)
  }
  return out
}

/** 去掉行尾注释和整行注释——注释里讲这两个坑是好事，不该被自己的守卫拦下。 */
function stripComments(src) {
  return src
    .replace(/\/\*[\s\S]*?\*\//g, '')
    .split('\n')
    .map((l) => {
      // 只切在字符串外的 //。简单状态机足够：Go 里 // 出现在反引号串中很罕见，
      // 但确实有（PromQL 里没有），所以还是老实扫一遍。
      let inS = null
      for (let i = 0; i < l.length; i++) {
        const c = l[i]
        if (inS) {
          if (c === '\\' && inS !== '`') i++
          else if (c === inS) inS = null
        } else if (c === '"' || c === '`') inS = c
        else if (c === '/' && l[i + 1] === '/') return l.slice(0, i)
      }
      return l
    })
    .join('\n')
}

const problems = []
let scanned = 0

for (const prod of products()) {
  const beDir = join(ROOT, prod, 'backend')
  if (!existsSync(beDir)) continue

  for (const f of walk(beDir)) {
    const rel = relative(ROOT, f)
    const raw = readFileSync(f, 'utf8')
    const src = stripComments(raw)
    scanned++

    // ---- 一、写死根挂载点 ----
    if (!MOUNT_SKIP.some((s) => basename(f) === s.file)) {
      // mountpoint="/"  或  mountpoint=~"/"  （后者只列了 / 一个，等价于前者）
      const re = /mountpoint\s*=~?\s*\\?"\/\\?"/g
      for (const m of src.matchAll(re)) {
        const line = src.slice(0, m.index).split('\n').length
        problems.push(
          `${rel}:${line} 写死了 ${m[0]}\n` +
            `      在 GKE COS 节点上 "/" 是**只读启动镜像**（1.93 GB），不是会满的那块盘。\n` +
            `      实测 g32-prod 35 个节点按它算全是 74.0656%，永不变化 —— 判定形同虚设。\n` +
            `      改用 backend/handlers/nodefs.go 的 nodeFsUsage() / nodeFsLabels()。`,
        )
      }
    }

    // ---- 二、clusterLabel 当选择器用 ----
    if (src.includes('clusterLabel')) {
      const lines = src.split('\n')
      lines.forEach((l, i) => {
        if (!l.includes('clusterLabel')) return
        // 传给 clusterSelector / clusterSelectorParts / verifyClusterValue 是正确用法；
        // 从 resolveEndpointFull 接收、以及 Sprintf("%s=%q", clusterLabel, ...) 也是对的
        if (/cluster(Selector|SelectorParts)\s*\(/.test(l)) return
        if (/verifyClusterValue\s*\(/.test(l)) return
        if (/resolveEndpointFull/.test(l)) return
        if (/%s=%q"\s*,\s*clusterLabel/.test(l)) return
        if (/nodeFsLabels\s*\(/.test(l)) return
        // 只认**字符串拼接**——这正是实际 bug 的形状（sel = clusterLabel + "," + sel）。
        // ⚠️ 第一版还写了 /clusterLabel\s*,/，结果把 `return clusterLabel, name`、
        //	多返回值赋值、map 字面量全抓了进来，一次 4 条误报。
        //	守卫误报的代价不是烦人，是人开始学着忽略它 —— 宁可窄。
        if (/clusterLabel\s*\+|\+\s*clusterLabel/.test(l)) {
          problems.push(
            `${rel}:${i + 1} 把 clusterLabel 当选择器拼进了查询\n` +
              `      ${l.trim()}\n` +
              `      clusterLabel 只是标签**名**（如 cluster），拼出来是 {cluster,...} —— PromQL 语法错误（HTTP 422）。\n` +
              `      用 clusterSelector(db, clusterLabel, cid) 得到 label="value"。`,
          )
        }
      })
    }
  }
}

if (problems.length > 0) {
  console.error('✗ Prometheus 选择器检查未通过：\n')
  for (const m of problems) console.error(`  - ${m}\n`)
  process.exit(1)
}
console.log(`✓ Prometheus 选择器：${scanned} 个 Go 文件，无写死根挂载点、无 clusterLabel 误拼`)
