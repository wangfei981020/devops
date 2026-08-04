/**
 * 构建前检查：Vue 组件里"用了但没 import"的标识符和组件。
 *
 * ## 为什么需要它
 *
 * Vite 打包这类错误**完全不报错**——漏 import 的名字要等到真正渲染到那一行
 * 才 ReferenceError，表现是整页白屏，控制台一条 `xxx is not defined`。
 * 也就是说：构建绿的、镜像推了、部署成功了，页面是坏的。
 *
 * 这不是假设。K8sClusters.vue 漏了 `useLoadState` 和 `LoadError`，
 * Notify.vue 漏了 `computed`，两个页面在生产上都是白屏，
 * 是用户点进去才发现的——我们自己的构建和部署流程全程没吭一声。
 *
 * 用 Node 手写而不是引 ESLint：不新增依赖（前端要求 npm audit 清零），
 * 而且只查这一类问题，规则简单到不会有人绕过它。
 *
 * 跑法：npm run check（build 前自动执行，见 package.json 的 prebuild）
 */
import { readFileSync, readdirSync, statSync } from 'node:fs'
import { join, relative } from 'node:path'

const ROOT = process.cwd()

// <script setup> 里的编译宏：由 Vue 编译器处理，无需 import
const MACROS = new Set(['defineProps', 'defineEmits', 'defineExpose', 'defineModel',
  'defineOptions', 'defineSlots', 'withDefaults'])

// 语言关键字 + 浏览器/JS 内置。写全一点，宁可漏报也别误报——
// 一个假警报就会让人开始忽略这个检查，那它就白做了。
const BUILTINS = new Set(`
if for while switch catch return typeof function await new do else super import
try finally throw yield delete void in of instanceof case break continue const let var
async get set static extends implements
console window document localStorage sessionStorage location history navigator
JSON Math Date Object Array String Number Boolean Promise Set Map WeakMap WeakSet
RegExp Error TypeError RangeError parseInt parseFloat isNaN isFinite
encodeURIComponent decodeURIComponent encodeURI decodeURI
setTimeout clearTimeout setInterval clearInterval requestAnimationFrame
cancelAnimationFrame fetch alert confirm prompt URL URLSearchParams Blob File
FileReader FormData Headers Request Response AbortController Intl Symbol BigInt
Proxy Reflect ArrayBuffer Uint8Array TextEncoder TextDecoder structuredClone
queueMicrotask performance crypto getComputedStyle matchMedia CustomEvent Event
`.trim().split(/\s+/))

// Vue 内置组件，模板里可直接用
const BUILTIN_COMPONENTS = new Set(['Transition', 'TransitionGroup', 'Teleport',
  'Suspense', 'KeepAlive', 'Component', 'Slot'])

function walk(dir, out = []) {
  for (const name of readdirSync(dir)) {
    if (name === 'node_modules' || name === 'dist' || name.startsWith('.')) continue
    const p = join(dir, name)
    if (statSync(p).isDirectory()) walk(p, out)
    else if (name.endsWith('.vue')) out.push(p)
  }
  return out
}

const stripComments = (s) =>
  s.replace(/\/\*[\s\S]*?\*\//g, '').replace(/^\s*\/\/.*$/gm, '')

/** 本文件里定义/引入了哪些名字 */
function collectDefined(script) {
  const d = new Set()
  // import 子句：具名、默认、命名空间、as 别名
  for (const m of script.matchAll(/import\s+([\s\S]+?)\s+from\s+['"]/g)) {
    const clause = m[1]
    for (const braced of clause.matchAll(/\{([^}]*)\}/g)) {
      for (let item of braced[1].split(',')) {
        item = item.trim()
        if (item) d.add(item.split(/\s+as\s+/).pop().trim())
      }
    }
    for (let item of clause.replace(/\{[^}]*\}/g, '').split(',')) {
      item = item.trim().replace(/^\*\s*as\s*/, '')
      if (/^[A-Za-z_$][\w$]*$/.test(item)) d.add(item)
    }
  }
  // 声明（含解构）
  for (const m of script.matchAll(/\b(?:const|let|var)\s+(\{[^}]*\}|\[[^\]]*\]|[A-Za-z_$][\w$]*)/g)) {
    const tok = m[1]
    if (tok[0] === '{' || tok[0] === '[') {
      // {a, b: c, ...rest} → 取真正绑定到作用域的那个名字
      for (const pair of tok.matchAll(/([A-Za-z_$][\w$]*)\s*(?::\s*([A-Za-z_$][\w$]*))?/g)) {
        d.add(pair[2] || pair[1])
      }
    } else d.add(tok)
  }
  for (const m of script.matchAll(/\bfunction\s*\*?\s*([A-Za-z_$][\w$]*)/g)) d.add(m[1])
  for (const m of script.matchAll(/\bclass\s+([A-Za-z_$][\w$]*)/g)) d.add(m[1])
  // 形参：括号内的简单标识符 + 单参箭头 + catch(e)
  for (const m of script.matchAll(/\(([^()]{0,300})\)\s*(?:=>|\{)/g)) {
    for (const id of m[1].matchAll(/[A-Za-z_$][\w$]*/g)) d.add(id[0])
  }
  for (const m of script.matchAll(/(?:^|[\s(,=])([A-Za-z_$][\w$]*)\s*=>/gm)) d.add(m[1])
  for (const m of script.matchAll(/\bcatch\s*\(\s*([A-Za-z_$][\w$]*)/g)) d.add(m[1])
  // 对象字面量的键、属性访问不算引用，这里不必收集（下面按调用形式过滤）
  return d
}

const problems = []

for (const file of walk(join(ROOT, 'src'))) {
  const src = readFileSync(file, 'utf8')
  const scriptM = src.match(/<script[^>]*\bsetup\b[^>]*>([\s\S]*?)<\/script>/)
  if (!scriptM) continue
  const scriptRaw = scriptM[1]
  const script = stripComments(scriptRaw)
  const defined = collectDefined(script)
  const rel = relative(ROOT, file)
  const seen = new Set()

  // A. 调用了但没定义的函数
  for (const m of script.matchAll(/(?<![.\w$?])([A-Za-z_$][\w$]*)\s*\(/g)) {
    const name = m[1]
    if (defined.has(name) || BUILTINS.has(name) || MACROS.has(name)) continue
    if (seen.has('f:' + name)) continue
    seen.add('f:' + name)
    const line = scriptRaw.slice(0, m.index).split('\n').length +
      src.slice(0, scriptM.index).split('\n').length - 1
    problems.push({ file: rel, kind: '调用了未 import 的函数', name, line })
  }

  // B. 模板里用了没 import 的组件（el-* 是 Element Plus 全局注册的，不在此列）
  const tplM = src.match(/<template>([\s\S]*)<\/template>/)
  if (tplM) {
    for (const m of tplM[1].matchAll(/<([A-Z][A-Za-z0-9]*)[\s/>]/g)) {
      const comp = m[1]
      if (defined.has(comp) || BUILTIN_COMPONENTS.has(comp)) continue
      if (seen.has('c:' + comp)) continue
      seen.add('c:' + comp)
      problems.push({ file: rel, kind: '模板用了未 import 的组件', name: comp, line: 0 })
    }
  }
}

if (problems.length === 0) {
  console.log('✅ 未定义引用检查通过')
  process.exit(0)
}

console.error(`\n❌ 发现 ${problems.length} 处"用了但没 import"，这些在浏览器里会让整页白屏：\n`)
for (const p of problems) {
  console.error(`   ${p.file}${p.line ? ':' + p.line : ''}  ${p.kind}: ${p.name}`)
}
console.error('\n构建已中止。补上 import 后重试。\n')
process.exit(1)
