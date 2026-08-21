#!/usr/bin/env node
/**
 * 各环境的 values 文件必须有**完全相同的键集合**。
 *
 * # 背景
 *
 * chart 自带的 `values.yaml` 被清空成一个哨兵，每个环境一个完整自包含的
 * values 文件（values-local.yaml / values-prod.yaml），不从 base 继承。
 *
 * 这么做是因为：继承在 helm 里是对的，但**读环境文件的人看不到继承来的值**。
 * 「生产给了多少资源」「镜像完整地址是什么」这类问题，要翻到另一个文件、
 * 还要知道它会被继承才答得出来 —— 而上线前人读的就是那一个环境文件。
 *
 * # 但代价必须有人兜
 *
 * 自包含 = 两个文件有大量重复。加新配置项时只改一个、忘了另一个，
 * 那个环境就会拿到 nil：
 *
 *   - 好一点的情况是模板报 `nil pointer evaluating interface {}.enabled`，
 *     而这个错误完全不提"你少配了一个键"。
 *   - 差一点的情况是模板里写了 `| default`，于是**静默用了个默认值** ——
 *     那正是我们想消灭的"看起来正常"。
 *
 * 所以这个守卫比对两边的键集合。差一个就构建失败，不会漂移。
 *
 * ⚠️ 只比**键**不比**值** —— 值本来就该不同（副本数、镜像仓库、入口形态）。
 */
import { readFileSync, existsSync, readdirSync } from 'node:fs'
import { join } from 'node:path'
import { ROOT, products } from './lib/products.mjs'

/**
 * 自由格式的值：内容本来就该按环境不同，只比"这个键在不在"，不往里比。
 *
 * ⚠️ 第一版没有这个白名单，于是把 `metrics.labels.release` 当成缺失的键报出来 ——
 * 而 labels 的**内容**正是两个环境该不一样的地方（生产要 release: kube-prometheus-stack，
 * 本地是空）。一个把正常差异报成错误的守卫，会被人直接关掉。
 */
const OPAQUE = new Set([
  'labels', 'annotations', 'podAnnotations', 'nodeSelector', 'tolerations',
  'env', 'envFrom', 'hosts', 'tls', 'relabelings', 'gateways', 'matchPrefix',
  'imagePullSecrets', 'paths',
])

/** 收集键路径。跳过列表项与自由格式子树。 */
function keyPaths(text) {
  const out = new Set()
  const stack = []
  for (const raw of text.split('\n')) {
    if (!raw.trim() || raw.trimStart().startsWith('#')) continue
    // 列表项不比：元素个数与顺序本来就该按环境不同
    if (raw.trimStart().startsWith('-')) continue
    const m = raw.match(/^(\s*)([\w.-]+):/)
    if (!m) continue
    const depth = Math.floor(m[1].length / 2)
    stack.length = Math.min(stack.length, depth)
    stack[depth] = m[2]
    const path = stack.slice(0, depth + 1)
    // 自由格式子树：记它本身，不记它下面的
    if (path.slice(0, -1).some((seg) => OPAQUE.has(seg))) continue
    out.add(path.join('.'))
  }
  return out
}

const problems = []

for (const p of products()) {
  const dir = join(ROOT, p, 'deploy/helm')
  if (!existsSync(dir)) continue
  // ⚠️ 只检查已经做过「自包含」改造的产品。
  // 判据是 values.yaml 里有哨兵 requireEnvValues —— 还在用继承的产品
  // 本来就允许环境文件只写差异项，对它们比键集合会得到一堆假阳性。
  const base = join(dir, 'values.yaml')
  if (!existsSync(base) || !readFileSync(base, 'utf8').includes('requireEnvValues')) continue

  const envFiles = readdirSync(dir).filter((f) => /^values-[\w-]+\.yaml$/.test(f))
  if (envFiles.length < 2) continue

  const sets = envFiles.map((f) => ({ f, keys: keyPaths(readFileSync(join(dir, f), 'utf8')) }))
  const union = new Set(sets.flatMap((s) => [...s.keys]))

  for (const { f, keys } of sets) {
    const missing = [...union].filter((k) => !keys.has(k)).sort()
    if (missing.length > 0) {
      problems.push(
        `${p}/${f} 缺 ${missing.length} 个键：\n      ${missing.join('\n      ')}` +
          `\n    环境 values 是自包含的，不从 values.yaml 继承 —— 缺键的那个环境会拿到 nil，` +
          `\n    要么报一个不提"少配了键"的 nil pointer，要么被 | default 静默兜掉。`,
      )
    }
  }
}

if (problems.length > 0) {
  console.error('✗ 环境 values 键集合不一致：\n')
  for (const m of problems) console.error(`  - ${m}\n`)
  process.exit(1)
}
console.log('✓ 环境 values 键集合一致')
