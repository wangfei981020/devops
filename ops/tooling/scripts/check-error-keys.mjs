#!/usr/bin/env node
/**
 * 后端错误码 ↔ 前端语言包的契约检查。
 *
 * 后端返回 `message_key`，前端拿它去语言包取文案。两边分处不同目录、
 * 不同语言、不同构建流程 —— 这是最容易漂移的一处：
 * 后端加了个错误码忘了通知前端，用户就会在界面上看到一个生的 key
 * （`error.capacityExceeded`），而这只在那个错误真的发生时才暴露。
 *
 * 本脚本从 Go 源码里抽出所有 messageKey，逐个确认前端语言包里有对应文案。
 *
 * 用法：node tooling/scripts/check-error-keys.mjs
 */

import { readFileSync, readdirSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const ROOT = join(dirname(fileURLToPath(import.meta.url)), '../..')
/**
 * 每个产品的错误码来源。
 *
 * ⚠️ **加产品必须加一行**。原先这里只写死了 CMDB 一个，于是 SSO 的 39 个错误码
 * 一个都没被校验 —— 前端漏文案时界面上直接显示生的 `errors.auth.bad_credential`，
 * 而检查器一路绿灯。这种"防线只覆盖了一半"比没有防线更危险，
 * 因为它给人一种已经被覆盖的错觉。
 *
 * pattern 是从源文件里抽错误码的正则，各产品的写法不同：
 *   CMDB  var messageKey = map[string]string{ CodeXxx: "error.yyy" }
 *   SSO   const CodeXxx = "auth.bad_credential"
 */
const SOURCES = [
  {
    product: 'ops-cmdb',
    file: 'ops-cmdb/backend/internal/httpx/errors.go',
    block: /var messageKey = map\[string\]string\{([\s\S]*?)\n\}/,
    prefix: '',
    // ⚠️ 光扫 messageKey 表是不够的：还有一类码是在调用点直接写死的
    //   httpx.FailKey(c, httpx.CodeBadRequest, "error.licenseInvalid", …)
    // 它们不进表，于是反向检查会把对应文案报成「死文案，可删」——
    // 差点按提示删掉一批还在用的（同一个坑第二次踩了，第一次是正则漏了驼峰）。
    // 所以这里额外全量扫一遍后端源码里的 "error.xxx" 字面量。
    alsoScanDir: 'ops-cmdb/backend',
    inlinePattern: /"(error\.[a-zA-Z][a-zA-Z0-9_]*)"/g,
  },
  {
    // ⚠️ 第三种写法：statusOf 表里是错误码字面量，message_key 由
    // toCamel(code) **自动推导**（httpx.Fail 内部做的），源码里根本不出现
    // "error.xxx" 这个字符串。所以既要扫码表、又要在这里模拟同一套推导 ——
    // 不登记的话，这个产品新加的错误码会被反向检查报成「死文案，可删」，
    // 而按提示删掉就等于让用户在那个错误发生时看到生的 key。
    product: 'ops-video-manager',
    file: 'ops-video-manager/backend/internal/httpx/errors.go',
    block: /var statusOf = map\[string\]int\{([\s\S]*?)\n\}/,
    prefix: 'error.',
    // 码 → messageKey 的推导必须与后端 toCamel 一字不差
    transform: (code) =>
      code.split('_').map((w, i) => (i === 0 ? w : w.charAt(0).toUpperCase() + w.slice(1))).join(''),
    codePattern: /"([a-z_]+)":\s*http\.Status/g,
    // 调用点直接指定 message_key 的情况（FailKey）
    alsoScanDir: 'ops-video-manager/backend',
    inlinePattern: /"(error\.[a-zA-Z][a-zA-Z0-9_]*)"/g,
  },
  {
    product: 'ops-sso',
    file: 'ops-sso/backend/internal/apierr/apierr.go',
    block: null, // 整个文件都扫
    // 前端把码放在 errors.* 下，所以校验时要加前缀
    prefix: 'errors.',
  },
]
/**
 * 按产品限定范围。**不传参数 = 扫全部**（CI 和手动跑都该是这个行为）。
 *
 * # 🔴 为什么必须支持限定
 *
 * 语言包是**全产品共享**的，而这个守卫挂在每个产品各自的 build 里。
 * 不限定的话，A 产品的一次在途改动（后端加了错误码、文案还没补）
 * 会让 **B 产品的构建直接失败** —— 而 B 的代码一个字都没动。
 *
 * 实际撞到过：ops-video-manager 加了 `probe_not_configured`，
 * 两分钟后 ops-cmdb 的构建就红了。
 *
 * ⚠️ 这和 check-write-coverage 记的是同一条教训（那里原话：
 * 「把它挂进任何一个产品的 build，都会因为别的产品的历史欠账而失败」）。
 * 同一个坑在第二个守卫上又踩了一次 —— 说明这是**所有共享资源守卫**
 * 都要考虑的事，不是某一个脚本的疏忽。
 *
 * ⚠️ 但默认绝不能变成只扫当前产品：一个只检查部分产品的守卫，
 * 它的绿色会被当成"全都查过了"。所以下面的输出里一定要打印范围。
 */
const scopes = process.argv.slice(2)
const SCOPED = scopes.length ? SOURCES.filter((s) => scopes.includes(s.product)) : SOURCES
if (scopes.length && SCOPED.length === 0) {
  console.error(`✗ check-error-keys: 没有匹配的产品（传入 ${scopes.join(', ')}）`)
  process.exit(1)
}

const LOCALES_DIR = join(ROOT, 'packages/i18n/locales')
const LOCALES = ['zh-CN', 'en-US']

/**
 * keys 是「要在语言包里查的完整路径」，已经带上各产品的前缀。
 *
 * 🔴 **两个方向用的集合不一样，混用会造成删数据的误导。**
 *
 *	正向（后端有码 → 语言包要有文案）：用 SCOPED，只管本次范围内的产品
 *	反向（语言包有文案 → 要有后端在用）：**必须用全部产品**
 *
 *	我第一版给这个守卫加范围限定时，只改了一处 —— 反向检查的"已知归属"
 *	集合跟着缩小了，于是 12 条属于 ops-sso / ops-video-manager 的**活文案**
 *	被报成「死文案，可删」。按那个提示删下去，会真的删掉在用的错误提示，
 *	而用户只在那个错误发生时才会看到生的 key。
 *
 *	⚠️ 守卫给出**破坏性建议**（"可删"）时，它的判据必须比别的检查更保守。
 */
function collectKeys(sources) {
  const keys = []
  for (const s of sources) {
  let src
  try {
    src = readFileSync(join(ROOT, s.file), 'utf8')
  } catch {
    // 产品还没建就跳过，不要因此让整个构建失败
    console.log(`  (跳过 ${s.product}：${s.file} 不存在)`)
    continue
  }
  let scope = src
  if (s.block) {
    const m = src.match(s.block)
    if (!m) {
      console.error(`✗ 没在 ${s.file} 里找到错误码表，脚本需要跟着改`)
      process.exit(1)
    }
    scope = m[1]
  }
  // 只认 `xxx.yyy` 形状的码，避免把别的字符串也扫进来。
  // 两种命名都要认：CMDB 是 error.badRequest（驼峰），SSO 是 auth.bad_credential（下划线）。
  // 早先只写了下划线那种，结果 CMDB 的码一个都没抽到，
  // 反向检查把它全部文案报成「死文案，可删」—— 差点按提示删掉一批还在用的。
  // 第三种：码是纯下划线串（不含点），messageKey 由后端 toCamel 推导。
  // 这类必须用 codePattern + transform，走上面那条「xxx.yyy 形状」的正则
  // 一个都抽不到，而抽不到会走下面的 exit(1)（这是对的：宁可炸，不能静默跳过）。
  const found = s.codePattern
    ? [...scope.matchAll(s.codePattern)].map((m) => s.prefix + s.transform(m[1]))
    : [...scope.matchAll(/"([a-zA-Z_]+\.[a-zA-Z_]+)"/g)].map((m) => s.prefix + m[1])
  if (found.length === 0) {
    console.error(`✗ ${s.file} 里一个错误码都没抽到`)
    process.exit(1)
  }
  keys.push(...new Set(found))

  if (s.alsoScanDir) {
    const inline = []
    for (const f of goFilesUnder(join(ROOT, s.alsoScanDir))) {
      // ⚠️ 必须先剥注释。注释里写「如 "error.clusterUnreachable"」这种举例很常见，
      // 扫进来就会要求给一个根本不存在的码补文案 —— 检查器提的要求必须能落地，
      // 提一个假的，下次真的那条也会被当成误报忽略掉。
      const text = stripGoComments(readFileSync(f, 'utf8'))
      for (const m of text.matchAll(s.inlinePattern)) inline.push(s.prefix + m[1])
    }
    keys.push(...new Set(inline))
    }
  }
  return keys
}

const keys = collectKeys(SCOPED)

/**
 * 去掉 Go 的注释。
 *
 * 只处理 // 和/* *​/ 两种，够用了：这里的目的不是解析 Go，
 * 只是别把注释里的举例当成真在用的错误码。
 * 字符串里出现 `//`（比如 URL "http://…"）会被误伤，但那种字符串里
 * 不会同时出现 "error.xxx" 形状的码，代价可以接受。
 */
function stripGoComments(src) {
  return src.replace(/\/\*[\s\S]*?\*\//g, '').replace(/^[ \t]*\/\/.*$/gm, '')
}

/** 递归收集 .go 文件。vendor/testdata 跳过：那里的字符串不是真在用的错误码。 */
function goFilesUnder(dir) {
  const out = []
  let entries
  try {
    entries = readdirSync(dir, { withFileTypes: true })
  } catch {
    return out
  }
  for (const e of entries) {
    if (e.name === 'vendor' || e.name === 'testdata' || e.name === 'node_modules') continue
    const p = join(dir, e.name)
    if (e.isDirectory()) out.push(...goFilesUnder(p))
    else if (e.name.endsWith('.go')) out.push(p)
  }
  return out
}
if (keys.length === 0) {
  console.error('✗ 没有任何错误码来源')
  process.exit(1)
}

/**
 * 取文案。两种结构都要认：
 *
 *   CMDB  { error: { badRequest: "…" } }        → 点号是**层级**
 *   SSO   { errors: { "auth.bad_credential": "…" } } → 点号是**键的一部分**
 *
 * 后者是有意的：错误码本身就是 `auth.bad_credential` 这个整体，
 * 拆成两层的话，i18next 取值时会把它当命名空间，而漏一个中间层就静默返回 key。
 * 先按扁平键直取，取不到再按层级下钻。
 */
function valueAt(obj, dotted) {
  const [head, ...rest] = dotted.split('.')
  const container = obj?.[head]
  if (container == null) return undefined
  const flat = rest.join('.')
  if (typeof container === 'object' && flat in container) return container[flat]
  return rest.reduce((acc, k) => (acc == null ? undefined : acc[k]), container)
}

const problems = []
for (const locale of LOCALES) {
  const common = JSON.parse(readFileSync(join(LOCALES_DIR, locale, 'common.json'), 'utf8'))
  for (const key of keys) {
    const v = valueAt(common, key)
    if (v === undefined) {
      problems.push(`${locale}: 缺 ${key}`)
    } else if (typeof v !== 'string' || v.trim() === '') {
      problems.push(`${locale}: ${key} 是空文案`)
    }
  }
}

/**
 * 前端自产的错误文案，不来自后端，反向检查要放行。
 *
 * malformed：请求成功返回了，但 body 里没有 data。这是请求库/网关层面的异常，
 * 后端根本没机会返回错误码 —— 而它又绝不能被当成空态放过去，
 * 所以由前端 fromQuery 自己造一个错误。
 */
const FRONTEND_ONLY = new Set([
  'error.malformed',
  // unreachable：连响应都没拿到（断网、DNS 挂、服务没起）。
  // 后端根本没机会返回错误码，只能由前端在 fetch 抛错时自己造。
  // 它和"服务端 5xx"必须分开——排查方向完全相反。
  'error.unreachable',
  // actionFailed：老接口的动作类请求（测连通、立即同步）成功失败都返回
  // HTTP 200，差别只在响应体的 ok 字段。前端把 ok:false 抛成这个错误码，
  // 后端不会返回它 —— 详见 CONVENTIONS §2.7.3。
  'error.actionFailed',
  // nonJsonResponse：HTTP 200 但 body 不是 JSON —— CDN/WAF 的挑战页
  // （200 + 一整页 HTML）、或响应在传输中被截断。
  // 从 HTTP 层看那是一次"成功"的请求，后端根本没参与，
  // 只有客户端解析那一步才发现不对，所以由客户端自己造这个码。
  // ⚠️ 不给它专属码的话会落进"按状态码兜底"，
  //	显示成「HTTP 200 · 服务端出错了」这种自相矛盾的话（实测过，OPSCMDB-052）。
  'error.nonJsonResponse',
])

/**
 * 反向检查：语言包里有、后端又不返回的，**可能**是死文案。
 *
 * ⚠️ 但「这个后端不返回」不等于「没人用」。
 *
 * `packages/i18n/locales/<lang>/common.json` 是**所有产品共享**的，
 * 而这个脚本只扫 ops-cmdb 的后端。于是另一个产品（如 ops-version）
 * 在自己的前端里 `t('common:error.network')` 造一条错误文案时，
 * 这里会把它判成死文案并**卡住 ops-cmdb 的构建** ——
 * 两个产品互相绊倒，而报错信息指向一个完全无辜的方向（"可删"）。
 *
 * 实测撞到过：ops-version 加了 error.unauthenticated / error.network，
 * ops-cmdb 的构建当场红了。
 *
 * 所以判死之前先看**有没有任何产品的前端在用它**。
 * 用到了就不是死的，无论是谁产的。
 */
function keysUsedByAnyFrontend() {
  const used = new Set()
  const scan = (dir) => {
    let entries
    try {
      entries = readdirSync(dir, { withFileTypes: true })
    } catch {
      return
    }
    for (const e of entries) {
      if (e.name === 'node_modules' || e.name === 'dist' || e.name === '.git') continue
      const p = join(dir, e.name)
      if (e.isDirectory()) {
        scan(p)
        continue
      }
      if (!/\.(ts|tsx)$/.test(e.name)) continue
      let src
      try {
        src = readFileSync(p, 'utf8')
      } catch {
        continue
      }
      // 只认 common 命名空间下的 error.*（别的命名空间不共享这份文件）
      for (const m of src.matchAll(/common:(error\.[A-Za-z0-9_]+)/g)) used.add(m[1])
    }
  }
  for (const e of readdirSync(ROOT, { withFileTypes: true })) {
    if (!e.isDirectory()) continue
    scan(join(ROOT, e.name, 'frontend', 'src'))
  }
  scan(join(ROOT, 'packages'))
  return used
}

// 🔴 反向检查用**全部产品**的码，不受 scopes 影响 —— 理由见 keys 的说明。
// 重新跑一遍收集逻辑（SOURCES 而不是 SCOPED），只为拿这个集合。
const declared = new Set(scopes.length ? collectKeys(SOURCES) : keys)
const usedByFrontends = keysUsedByAnyFrontend()
const zh = JSON.parse(readFileSync(join(LOCALES_DIR, 'zh-CN', 'common.json'), 'utf8'))
for (const k of Object.keys(zh.error ?? {})) {
  const full = `error.${k}`
  if (!declared.has(full) && !FRONTEND_ONLY.has(full) && !usedByFrontends.has(full)) {
    problems.push(
      `zh-CN: ${full} 后端不返回、没有任何产品的前端在用、也不在自产名单里（死文案，可删）`,
    )
  }
}

if (problems.length > 0) {
  console.error(`✗ 后端错误码与前端语言包不一致，共 ${problems.length} 处：\n`)
  for (const p of problems) console.error(`  ${p}`)
  console.error('\n后端加错误码时要同步在 packages/i18n/locales/*/common.json 的 error 下补文案。')
  console.error('漏了的话，用户会在界面上看到生的 key，而这只在那个错误真的发生时才暴露。')
  process.exit(1)
}

// ⚠️ 一定要打印范围：一个只检查部分产品的守卫，它的绿色会被当成"全都查过了"
console.log(
  `✓ 后端 ${keys.length} 个错误码在 ${LOCALES.length} 种语言里都有文案` +
    `（范围 ${scopes.length ? scopes.join(', ') : '全部产品'}）`,
)
