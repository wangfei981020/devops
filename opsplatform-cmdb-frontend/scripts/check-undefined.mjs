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

// 模板表达式里可以直接调、但不在 <script setup> 里定义的东西：
// Vue 暴露的、$ 开头的实例属性、以及作用域插槽/v-for 变量上的方法调用。
// 这份名单宁可宽一点——检查 C 的价值在于抓 suggest() 这种"整个名字都不存在"的，
// 为了多抓几个边角而误报，会让人开始忽略整个脚本。
const TEMPLATE_GLOBALS = new Set(['$emit', '$slots', '$attrs', '$refs', '$router', '$route',
  '$t', '$nextTick', '$forceUpdate', 'emit', 'slots', 'attrs',
  'Boolean', 'Number', 'String', 'Array', 'Object'])

function walk(dir, out = []) {
  for (const name of readdirSync(dir)) {
    if (name === 'node_modules' || name === 'dist' || name.startsWith('.')) continue
    const p = join(dir, name)
    if (statSync(p).isDirectory()) walk(p, out)
    else if (name.endsWith('.vue')) out.push(p)
  }
  return out
}

// 去掉注释、字符串和**正则字面量**。
//
//	⚠️ 正则那一条是补的：`.replace(/\B(?=(\d{3})+(?!\d))/g, ',')`（千分位分隔）
//	里的 `\B(` 会被下面的"函数调用"正则当成调用 `B(`，于是报一个不存在的问题。
//	误报比不报更糟——防线一旦开始喊狼来了，人就会绕过它，
//	而这道脚本的存在意义正是拦住那些"构建全绿、运行期才炸"的错。
const stripNoise = (s) =>
  s
    .replace(/\/\*[\s\S]*?\*\//g, '')
    .replace(/^\s*\/\/.*$/gm, '')
    // 正则字面量：前面是运算符/分隔符才算（避免把除法当正则）
    .replace(/([=(,:[!&|?{};]\s*)\/(?![/*])(?:\\.|\[(?:\\.|[^\]])*\]|[^/\\\n])+\/[gimsuy]*/g, '$1/RE/')
    .replace(/`(?:\\.|[^`\\])*`/g, '``')
    .replace(/'(?:\\.|[^'\\])*'/g, "''")
    .replace(/"(?:\\.|[^"\\])*"/g, '""')

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
  const script = stripNoise(scriptRaw)
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

  // C. 模板里调了 setup 根本没定义的方法
  //
  //	这是"运行期才炸"的第三种变体，前两条都盖不住它，而它最难发现：
  //	K8sNsProject.vue 的模板调 `suggest(row.name)`，而 setup 里的 suggest()
  //	在归属逻辑搬到后端时被删了，模板这一处漏改。异常发生在表格 slot 内、
  //	被 Vue 捕获，**其它列正常、页面不白屏，只有那一列静默空白**——
  //	比整页崩还难发现，生产上每打开一次刷 40 条 error 也没人注意到（CMDB-050）。
  if (tplM) {
    // ⚠️ 只能在**表达式**里找调用，不能扫整个模板。
    //	模板里的纯文本和 HTML 注释同样长得像调用：
    //	  正文 `覆盖 K8s(Pod/工作负载/节点)` → 会被当成调用 K8s()
    //	  注释里举例写的 `suggest()`        → 会被当成真的在调
    //	两者都误报过。所以先去掉 HTML 注释，再只取插值 {{ }} 和
    //	指令/绑定/事件（v-* : @）的属性值。
    const tplNoComment = tplM[1].replace(/<!--[\s\S]*?-->/g, '')
    const exprs = []
    const push = (text, idx) => exprs.push({ text: stripNoise(text), idx })
    for (const m of tplNoComment.matchAll(/\{\{([\s\S]*?)\}\}/g)) push(m[1], m.index)
    for (const m of tplNoComment.matchAll(/(?::|v-|@)[\w:.\-[\]]*\s*=\s*"([^"]*)"/g)) push(m[1], m.index)
    for (const m of tplNoComment.matchAll(/(?::|v-|@)[\w:.\-[\]]*\s*=\s*'([^']*)'/g)) push(m[1], m.index)

    for (const { text, idx } of exprs) {
      for (const m of text.matchAll(/(?<![.\w$?])([A-Za-z_$][\w$]*)\s*\(/g)) {
        const name = m[1]
        if (defined.has(name) || BUILTINS.has(name) || MACROS.has(name)) continue
        if (TEMPLATE_GLOBALS.has(name)) continue
        if (seen.has('t:' + name)) continue
        seen.add('t:' + name)
        const line = src.slice(0, tplM.index).split('\n').length +
          tplNoComment.slice(0, idx).split('\n').length - 1
        problems.push({ file: rel, kind: '模板调用了 setup 未定义的方法', name, line })
      }
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
