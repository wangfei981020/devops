#!/usr/bin/env node
/**
 * 从后端 Go 源码生成前端 API 类型。
 *
 *   swag（Go 注解）  →  swagger.json (2.0)
 *                    →  swagger2openapi  →  openapi.json (3.0)
 *                    →  openapi-typescript  →  generated/cmdb.d.ts
 *
 * 中间那步转换是必须的：swag v1 出的是 Swagger 2.0，
 * 而 openapi-typescript 只吃 OpenAPI 3.x。
 *
 * ⚠️ generated/ 下的文件**不要手改**。手改的结果是：
 * 后端真的改了字段时，生成器会把你的修改冲掉；而在被冲掉之前，
 * 前端类型和后端实际返回已经对不上了，编译还是绿的。
 *
 * 用法：pnpm --filter @ops/api gen
 */

import { execFileSync } from 'node:child_process'
import { existsSync, mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const PKG = join(dirname(fileURLToPath(import.meta.url)), '..')
const OPS = join(PKG, '../..')
const BACKEND = join(OPS, 'ops-cmdb/backend')
const SWAGGER_2 = join(BACKEND, 'docs/swagger.json')
const OPENAPI_3 = join(PKG, 'src/generated/openapi.json')
// 用 .ts 而非 .d.ts：.d.ts 不会被 tsc 编译进 dist，消费方就解析不到类型
const OUT_DTS = join(PKG, 'src/generated/cmdb.ts')

function run(cmd, args, cwd) {
  return execFileSync(cmd, args, { cwd, encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] })
}

mkdirSync(join(PKG, 'src/generated'), { recursive: true })

// ---- 1. Go 注解 → Swagger 2.0 ----
const goBin = run('go', ['env', 'GOPATH']).trim()
const swagBin = join(goBin, 'bin/swag')
if (!existsSync(swagBin)) {
  console.error(`✗ 找不到 swag: ${swagBin}`)
  console.error('  先装：go install github.com/swaggo/swag/cmd/swag@latest')
  process.exit(1)
}
console.log('→ swag: 从 Go 注解生成 Swagger 2.0')
run(swagBin, ['init', '-g', 'main.go', '-o', 'docs', '--parseInternal', '--parseDependency=false'], BACKEND)

// swag 会顺带生成 docs.go —— 那是给 gin-swagger 提供在线 UI 用的，
// 它 import 了 github.com/swaggo/swag，会给后端凭空加一个运行时依赖，
// 还多一处生产上要记得关掉的暴露面。我们只要 spec 做类型生成，删掉它。
const docsGo = join(BACKEND, 'docs/docs.go')
if (existsSync(docsGo)) rmSync(docsGo)

// ---- 2. Swagger 2.0 → OpenAPI 3.0 ----
console.log('→ swagger2openapi: 2.0 → 3.0')
const { default: converter } = await import('swagger2openapi')
const swagger2 = JSON.parse(readFileSync(SWAGGER_2, 'utf8'))
const converted = await new Promise((resolve, reject) => {
  converter.convertObj(swagger2, { patch: true, warnOnly: true }, (err, out) =>
    err ? reject(err) : resolve(out.openapi),
  )
})
writeFileSync(OPENAPI_3, `${JSON.stringify(converted, null, 2)}\n`)

// ---- 3. OpenAPI 3.0 → TypeScript ----
console.log('→ openapi-typescript: 生成 .d.ts')
const { default: openapiTS, astToString } = await import('openapi-typescript')
const ast = await openapiTS(converted)
const banner = `/**
 * 自动生成，请勿手改。
 *
 * 来源：ops-cmdb/backend 的 swag 注解
 * 重新生成：pnpm --filter @ops/api gen
 *
 * 手改的结果是：后端真的改了字段时生成器会把改动冲掉；
 * 而在被冲掉之前，前端类型和后端实际返回已经对不上了，编译还是绿的。
 */

`
writeFileSync(OUT_DTS, banner + astToString(ast))

const paths = Object.keys(converted.paths ?? {})
console.log(`\n✓ 已生成 ${paths.length} 个路径的类型 → src/generated/cmdb.ts`)
for (const p of paths) console.log(`    ${p}`)
