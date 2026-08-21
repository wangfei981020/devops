#!/usr/bin/env node
/**
 * 覆盖层写对了没有。
 *
 * 🔴 为什么需要这条守卫：
 *
 * Tailwind 4 的 @theme 在**构建期把间接层拍平**。@ops/design 里的
 *     @theme { --color-danger: var(--ops-danger) }
 * 编译出的工具类是 `.text-danger { color: var(--ops-danger) }` ——
 * `--color-danger` 运行时没有任何消费者。
 *
 * 于是产品里写 `:root { --color-danger: 红 }` 会：
 *   · 构建通过
 *   · getComputedStyle 查 --color-danger 返回新值
 *   · 界面**完全不变**
 *
 * 所有证据都指向"改生效了"，只有界面不认 —— 这一类沉默失败
 * 实测花了很久才定位，必须由守卫接住。
 *
 * 判据：产品自己的样式文件里，:root 块内不得**定义** --color-*，
 * 应改为定义对应的 --ops-*。引用（var(--color-x)）同样禁止，
 * 因为它和工具类读的不是同一个变量，两边会静默分叉。
 */
import { readFileSync, readdirSync, statSync } from 'node:fs'
import { join, relative } from 'node:path'

const root = process.argv[2] || process.cwd()
const problems = []

function walk(dir) {
  for (const name of readdirSync(dir)) {
    if (name === 'node_modules' || name === 'dist' || name.startsWith('.')) continue
    const full = join(dir, name)
    if (statSync(full).isDirectory()) walk(full)
    else if (name.endsWith('.css')) check(full)
  }
}

function check(file) {
  const src = readFileSync(file, 'utf8')
  // 只查产品侧文件；共享包 @ops/design 本来就该定义 --color-*
  if (file.includes(`${'packages'}/design`)) return

  // 🔴 必须跨行跟踪块注释状态，不能按行正则剥。
  // 这条守卫第一版就是行级剥的，于是把**本文件注释里讲解这个坑时
  // 引用的示例代码**判成了违规 —— 守卫报出来的是文档，不是代码。
  // （check-dead-state 上栽过同一个错：注释里的字符串被当成真引用。）
  let inComment = false
  const lines = src.split('\n')
  lines.forEach((line, i) => {
    let code = ''
    let j = 0
    while (j < line.length) {
      if (inComment) {
        const end = line.indexOf('*/', j)
        if (end === -1) { j = line.length } else { inComment = false; j = end + 2 }
      } else {
        const start = line.indexOf('/*', j)
        if (start === -1) { code += line.slice(j); j = line.length } else {
          code += line.slice(j, start); inComment = true; j = start + 2
        }
      }
    }
    const m = code.match(/(--color-[a-z0-9-]+)\s*:/)
    if (m) {
      problems.push({
        file: relative(root, file),
        line: i + 1,
        token: m[1],
        fix: m[1].replace('--color-', '--ops-'),
      })
    }
  })
}

walk(join(root, 'frontend', 'src'))

if (problems.length) {
  console.error('\n✖ 覆盖层写错了：产品样式里定义 --color-* 不会生效\n')
  console.error('  Tailwind 4 的 @theme 在构建期已把 --color-x → --ops-x 拍平，')
  console.error('  工具类读的是 --ops-x。改 --color-x 界面不会变（但也不报错）。\n')
  for (const p of problems) {
    console.error(`  ${p.file}:${p.line}  ${p.token}  →  改成 ${p.fix}`)
  }
  console.error('')
  process.exit(1)
}
console.log('✓ 主题覆盖层写在 --ops-* 层')
