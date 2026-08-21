#!/usr/bin/env node
/**
 * 找出「需要外部依赖、没有就静默跳过」的测试，并要求它们被显式登记。
 *
 * 🔴 为什么需要这条守卫：
 *
 * ops-video-manager 的抽样逻辑有 3 条测试，全部 `t.Skip("未设 TEST_DSN")`。
 * 于是 `go test ./...` 一直是绿的，而抽样路径的 SQL **从来没被执行过** ——
 * 里面藏着两个必然报错的 bug（列名重复、SELECT * 多带一列）。
 *
 * 本地候选数一直小于全探阈值，运行时也总走全探分支，
 * 所以线上线下都没暴露。而生产上千桌台必然走抽样 → CDN 层整个探不了。
 *
 * 跳过本身没问题（不是每台机器都有库），问题是**跳过不可见**：
 * 一片绿色里看不出哪些是"验过了"，哪些是"没验"。
 *
 * 判据：测试文件里出现 t.Skip 且理由涉及外部依赖时，
 * 必须在 tooling/skipped-tests.json 里登记，说明怎么才能真跑它。
 * 登记本身不解决问题，但它把"没验过"从隐形变成有清单可查。
 */
import { readFileSync, readdirSync, statSync, existsSync } from 'node:fs'
import { join, relative } from 'node:path'

const root = process.argv[2] || process.cwd()
const registryPath = join(root, 'tooling', 'skipped-tests.json')
const registry = existsSync(registryPath)
  ? JSON.parse(readFileSync(registryPath, 'utf8'))
  : {}

const found = []

function walk(dir) {
  for (const name of readdirSync(dir)) {
    if (name === 'node_modules' || name === 'dist' || name === 'vendor' || name.startsWith('.')) continue
    const full = join(dir, name)
    if (statSync(full).isDirectory()) walk(full)
    else if (name.endsWith('_test.go')) scan(full)
  }
}

function scan(file) {
  const src = readFileSync(file, 'utf8')
  const lines = src.split('\n')
  lines.forEach((line, i) => {
    // t.Skip / t.Skipf，且理由里提到环境变量或外部依赖
    if (!/\bt\.Skipf?\(/.test(line)) return
    if (!/(环境变量|未设|连不上|DSN|SKIP_|不可用|没有库)/.test(line)) return
    found.push({ file: relative(root, file), line: i + 1, text: line.trim().slice(0, 90) })
  })
}

walk(root)

const unregistered = found.filter((f) => !registry[f.file])

if (unregistered.length) {
  console.error('\n✖ 有依赖外部环境的测试未登记 —— 它们平时是静默跳过的\n')
  console.error('  静默跳过的测试在 `go test ./...` 里显示为绿色，')
  console.error('  但它覆盖的代码路径一次都没执行过。实测因此漏掉过两个必然报错的 SQL bug。\n')
  for (const f of unregistered) {
    console.error(`  ${f.file}:${f.line}`)
    console.error(`    ${f.text}`)
  }
  console.error(`\n  在 ${relative(root, registryPath)} 里登记，写明怎样才能真的跑它。\n`)
  process.exit(1)
}

const total = Object.keys(registry).length
console.log(`✓ check-skipped-tests: ${found.length} 处条件跳过均已登记（${total} 个文件在册）`)
