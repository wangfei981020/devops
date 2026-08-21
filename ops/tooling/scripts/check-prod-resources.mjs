#!/usr/bin/env node
/**
 * 生产 values 必须显式声明 resources，并且 HPA 的扩容阈值必须够得着。
 *
 * # 为什么要查「显式声明」
 *
 * resources 写在 values.yaml 里、values-prod.yaml 不覆盖，helm 渲染是对的 ——
 * 但上线前人读的是 values-prod.yaml，那里看不到任何资源配置。
 * 于是「生产给了多少资源」这个问题，要翻到另一个文件、还要知道它会被继承才答得出来。
 * 这类配置迟早会被人按错误的假设改动。
 *
 * # 为什么要查 HPA 阈值
 *
 * HPA 的扩容阈值 = cpu request × targetCPUUtilizationPercentage。
 * request 给得极小时（比如 10m × 70% = 7m），阈值会低到**任何真实负载都够不着**，
 * 或者反过来低到一有突发就抖动。
 *
 * ops-cmdb 前端就踩过：request 10m、阈值 7m，而生产实测 CPU 是 0.037m ——
 * 差 190 倍，这个 HPA 数学上不可能触发。
 * 它不提供任何保护，只提供「我们配了自动扩容」的错觉，
 * 而错觉比没有更危险：真出容量问题时，没人会去查一个「已经配了 HPA」的服务。
 *
 * 所以规则是：开 HPA 就必须给一个像样的 cpu request（≥ 50m）。
 * 达不到就别开 —— 固定副本数至少是诚实的。
 */
import { readFileSync, readdirSync, existsSync } from 'node:fs'
import { join, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'

const ROOT = join(dirname(fileURLToPath(import.meta.url)), '../..')

/** HPA 开启时 cpu request 的下限。低于它，扩容阈值就没有意义了 */
const MIN_CPU_REQUEST_FOR_HPA_M = 50

/** 把 100m / 1 / 1500m 归一成毫核 */
function toMilli(v) {
  if (v == null) return null
  const s = String(v).trim()
  if (s.endsWith('m')) return Number.parseInt(s, 10)
  const n = Number.parseFloat(s)
  return Number.isNaN(n) ? null : Math.round(n * 1000)
}

/**
 * 极简 YAML 取值：只处理本仓库 values 文件的形态（两空格缩进、无锚点、无流式映射）。
 * 引第三方 YAML 解析器不值得 —— 守卫脚本必须零依赖才能在任何环境下跑起来。
 */
// ⚠️ 返回的块必须**去掉缩进**再返回。
//
// 第一版没去缩进，于是 section(section(src,'frontend'),'resources') 永远返回 null：
// 外层按 0 缩进找到了 frontend，内层还是按 0 缩进去找 resources，
// 而它在原文里是 2 缩进 —— 找不到。
// 后果不是报错，是守卫**对每个产品都报"没声明 resources"**，包括刚刚明明声明了的那个。
// 一个总是失败的守卫和一个总是通过的守卫一样没用，人只会把它关掉。
function section(src, path) {
  let lines = src.split('\n')
  for (const key of path.split('.')) {
    const i = lines.findIndex(
      (l) => new RegExp(`^${key}:`).test(l) && !l.trimStart().startsWith('#'),
    )
    if (i === -1) return null
    const body = []
    for (const l of lines.slice(i + 1)) {
      if (l.trim() === '' || l.trimStart().startsWith('#')) continue
      const ind = l.length - l.trimStart().length
      if (ind === 0) break
      body.push(l.slice(2)) // 去掉一层缩进，让下一轮还能按 0 缩进匹配
    }
    lines = body
  }
  return lines.join('\n')
}

function scalar(src, key) {
  const m = src?.match(new RegExp(`^\\s*${key}:\\s*(\\S+)`, 'm'))
  return m ? m[1] : null
}

const products = readdirSync(ROOT, { withFileTypes: true })
  .filter((d) => d.isDirectory() && d.name.startsWith('ops-'))
  .map((d) => d.name)

const problems = []

// 镜像仓库必须走 global.imageRegistry，组件里只写镜像名。
//
// ⚠️ 各组件写全路径的话，换仓库要改多处，而**只改一处不会报错**：
// 改了的正常拉，没改的还在拉旧仓库 —— 两个镜像都能起来，只是版本对不上，
// 表现为"前端升了后端没升"这类最难复现的错配。
for (const p of products) {
  for (const which of ['values.yaml', 'values-prod.yaml', 'values-local.yaml']) {
    const f = join(ROOT, p, 'deploy/helm', which)
    if (!existsSync(f)) continue
    for (const line of readFileSync(f, 'utf8').split('\n')) {
      const m = line.match(/^\s+repository:\s*(\S+)/)
      // 带 / 或 : 的是完整仓库路径；只写镜像名的不含这两个字符
      if (m && /[/:]/.test(m[1])) {
        problems.push(
          `${p}/${which}: image.repository 写了完整路径 ${m[1]}` +
            `\n    仓库前缀应放在 global.imageRegistry，组件里只写镜像名 ——` +
            `\n    否则换仓库要改多处，而只改一处不会报错（另一个还在拉旧仓库）。`,
        )
      }
    }
  }
}

for (const p of products) {
  const file = join(ROOT, p, 'deploy/helm/values-prod.yaml')
  if (!existsSync(file)) continue
  const src = readFileSync(file, 'utf8')

  for (const comp of ['frontend', 'backend']) {
    const compSrc = section(src, comp)
    if (compSrc == null) continue

    // ⚠️ 只有本产品**真的部署**这个组件时才要求它声明资源。
    //
    // ops-cmdb 已经去掉了 backend.enabled（后端一律由 chart 部署），
    // 但 ops-alert / ops-sso 还留着这个开关、且在 values-prod 里靠继承。
    // 我一度直接删掉这条判断，结果把那两个产品的后端也拖进来报错 ——
    // 而它们的配置本身没问题，只是形态还没统一。
    // 判据：显式 enabled: false 跳过；没有 enabled 键 = 一律部署。
    if (scalar(compSrc, 'enabled') === 'false') continue

    const res = section(compSrc, 'resources')
    if (res == null) {
      problems.push(
        `${p}/values-prod.yaml: ${comp} 没有显式声明 resources。` +
          `\n    渲染出来是对的（继承 values.yaml），但上线前读这个文件的人看不到生产给了多少资源。`,
      )
      continue
    }
    for (const kind of ['requests', 'limits']) {
      const blk = section(res, kind)
      const miss = ['cpu', 'memory'].filter((k) => !blk || !scalar(blk, k))
      if (miss.length > 0) {
        problems.push(
          `${p}/values-prod.yaml: ${comp}.resources.${kind} 缺 ${miss.join(' / ')}` +
            (kind === 'requests'
              ? `\n    request 同时是 HPA 算利用率的分母，缺了等于那个指标不生效。`
              : `\n    没有 limit 的容器可以吃满整个节点，把同节点的其它服务一起拖垮。`),
        )
      }
    }
    const reqs = section(res, 'requests')
    if (reqs == null || !scalar(reqs, 'cpu') || !scalar(reqs, 'memory')) continue

    // HPA 开着就得给够 request，否则扩容阈值没有意义
    const hpa = section(compSrc, 'autoscaling')
    if (hpa && scalar(hpa, 'enabled') === 'true') {
      // ⚠️ 只挂 CPU 指标的 HPA 对静态服务等于没有保护：
      // nginx 几乎不吃 CPU，先撑不住的一定是内存
      if (!scalar(hpa, 'targetMemoryUtilizationPercentage')) {
        problems.push(
          `${p}/values-prod.yaml: ${comp} 的 HPA 只配了 CPU 指标，缺 targetMemoryUtilizationPercentage` +
            `\n    静态服务几乎不吃 CPU，只看 CPU 的 HPA 在内存吃紧时不会扩容。`,
        )
      }
      // ⚠️ 开了 HPA 的 Deployment 不能同时写死 replicas。
      // 两者都在时，每次 helm upgrade 会把副本数按回 replicaCount、
      // 砍掉 HPA 扩出来的实例，HPA 再扩回去 ——
      // 表现是"每次发布后容量掉一截然后慢慢恢复"，而没有任何一处会报错。
      const tpl = readFileSync(join(ROOT, p, `deploy/helm/templates/${comp}.yaml`), 'utf8')
      const hasGuardedReplicas = new RegExp(
        `if not \\.Values\\.${comp}\\.autoscaling\\.enabled[\\s\\S]{0,400}?replicas:`,
      ).test(tpl)
      if (/^\s{2}replicas:/m.test(tpl) && !hasGuardedReplicas) {
        problems.push(
          `${p}/templates/${comp}.yaml: 开了 HPA 却无条件写死 replicas` +
            `\n    每次 helm upgrade 会把副本数按回 replicaCount，砍掉 HPA 扩出来的实例。` +
            `\n    用 {{- if not .Values.${comp}.autoscaling.enabled }} 包起来。`,
        )
      }
      // ⚠️ 开了 HPA 时，凡是"按副本数决定要不要生成"的对象（PDB 是典型），
      // 判据必须看 autoscaling.minReplicas，不能只看 replicaCount ——
      // 后者开了 HPA 之后只在第一次创建时生效。
      //
      // 生产真踩过：replicaCount=1 + minReplicas=2，实际跑 2 副本却
      // **一个 PDB 都没生成**，节点排水时两个副本可以被同时驱逐，
      // 而 helm 一切正常、kubectl get pdb 是空的，没有任何地方提示少了这层保护。
      if (/podDisruptionBudget\.enabled[\s\S]{0,80}?\.replicaCount\) 1\)/.test(tpl)) {
        problems.push(
          `${p}/templates/${comp}.yaml: PDB 的生成条件只看 replicaCount` +
            `\n    开了 HPA 时实际副本数由 minReplicas 决定，会出现"跑着多副本却没有 PDB"。` +
            `\n    改用 ternary 取实际生效值，见 ops-cmdb 的写法。`,
        )
      }
      const cpuM = toMilli(scalar(reqs, 'cpu'))
      const target = Number.parseInt(scalar(hpa, 'targetCPUUtilizationPercentage') ?? '80', 10)
      if (cpuM != null && cpuM < MIN_CPU_REQUEST_FOR_HPA_M) {
        problems.push(
          `${p}/values-prod.yaml: ${comp} 开了 HPA 但 cpu request 只有 ${cpuM}m` +
            `\n    扩容阈值 = ${cpuM}m × ${target}% = ${Math.round((cpuM * target) / 100)}m，低到真实负载够不着，` +
            `\n    这个 HPA 实际不会触发，只会造成"已配自动扩容"的错觉。` +
            `\n    要么把 cpu request 提到 ≥${MIN_CPU_REQUEST_FOR_HPA_M}m，要么关掉 HPA 用固定副本数。`,
        )
      }
    }
  }
}

if (problems.length > 0) {
  console.error('✗ 生产资源配置检查未通过：\n')
  for (const m of problems) console.error(`  - ${m}\n`)
  process.exit(1)
}
console.log(`✓ 生产资源配置：${products.length} 个产品，requests/limits 均已显式声明`)
