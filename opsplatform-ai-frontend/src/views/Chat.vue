<script setup>
import { ref, watch, nextTick, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { useChatStore } from '../stores/chat'
import http from '../api/http'

const chat = useChatStore()
const busy = ref(false)
const input = ref('')
const streamEl = ref(null)

// 模型选择(顶栏可直接切,写回后端配置)
const model = ref('')
const models = ref([])
async function loadModels() {
  try {
    const { data } = await http.get('/config')
    model.value = data.model
    models.value = data.models || []
  } catch (e) { /* 忽略 */ }
}
async function changeModel(m) {
  try {
    await http.post('/config', { model: m })
    ElMessage.success('已切换模型: ' + m)
  } catch (e) {
    ElMessage.error('切换失败: ' + (e.response?.data || e.message))
    loadModels()
  }
}
onMounted(loadModels)

const intro =
  '你好，我是运维 AI 助手。问我集群/主机/域名/证书/成本，或让我排障' +
  '（例：<b>central.k8s-g32-uat.com 关联到哪个模块、跑在哪些 Pod 上</b>）。我只读、给命令，你自己执行。'

function newIntro() {
  return [{ role: 'ai', tools: [], html: intro, error: '' }]
}
const messages = ref(newIntro())

// 侧栏「新建对话」→ sessionId 变化 → 清空
watch(() => chat.sessionId, () => { messages.value = newIntro() })

function esc(s) {
  return s.replace(/[&<>]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;' }[c]))
}
// 极简 markdown：```代码块``` + `行内` + 换行
function render(t) {
  let out = ''
  const parts = t.split(/```/)
  for (let i = 0; i < parts.length; i++) {
    if (i % 2 === 1) {
      out += '<pre><code>' + esc(parts[i].replace(/^[a-z]*\n/, '')) + '</code></pre>'
    } else {
      out += esc(parts[i]).replace(/`([^`]+)`/g, '<code>$1</code>').replace(/\n/g, '<br>')
    }
  }
  return out
}

async function scrollBottom() {
  await nextTick()
  const el = streamEl.value
  if (el) el.scrollTop = el.scrollHeight
}

function onKey(e) {
  if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); send() }
}

async function send() {
  const text = input.value.trim()
  if (!text || busy.value) return
  messages.value.push({ role: 'user', tools: [], html: esc(text), error: '' })
  input.value = ''
  busy.value = true

  const ai = { role: 'ai', tools: [], html: '', error: '', typing: true, raw: '' }
  messages.value.push(ai)
  scrollBottom()

  try {
    const resp = await fetch('/api/chat', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ session: chat.sessionId, message: text }),
    })
    if (!resp.ok || !resp.body) throw new Error('HTTP ' + resp.status)
    const reader = resp.body.getReader()
    const dec = new TextDecoder()
    let buf = ''
    while (true) {
      const { done, value } = await reader.read()
      if (done) break
      buf += dec.decode(value, { stream: true })
      let idx
      while ((idx = buf.indexOf('\n\n')) >= 0) {
        const line = buf.slice(0, idx)
        buf = buf.slice(idx + 2)
        if (!line.startsWith('data: ')) continue
        const ev = JSON.parse(line.slice(6))
        if (ev.type === 'tool') {
          ai.typing = false
          ai.tools.push({ name: ev.name, done: false })
        } else if (ev.type === 'tool_done') {
          const t = [...ai.tools].reverse().find((x) => x.name === ev.name && !x.done)
          if (t) t.done = true
        } else if (ev.type === 'text') {
          ai.typing = false
          ai.raw += ev.text
          ai.html = render(ai.raw)
        } else if (ev.type === 'error') {
          ai.typing = false
          ai.error = ev.text
        }
        scrollBottom()
      }
    }
    if (!ai.raw && !ai.tools.length && !ai.error) ai.html = '（无内容）'
    ai.typing = false
  } catch (e) {
    ai.typing = false
    ai.error = '请求失败: ' + e.message
  } finally {
    busy.value = false
    scrollBottom()
  }
}
</script>

<template>
  <div class="top">
    <span class="title">对话</span>
    <span style="flex: 1"></span>
    <el-select
      v-model="model"
      size="small"
      filterable
      allow-create
      default-first-option
      style="width: 190px"
      @change="changeModel"
    >
      <el-option v-for="m in models" :key="m" :label="m" :value="m" />
    </el-select>
    <span class="chip ro">🛡 只读</span>
  </div>

  <div class="stream" ref="streamEl">
    <div class="wrap">
      <div v-for="(m, i) in messages" :key="i" class="msg" :class="{ u: m.role === 'user' }">
        <div class="who" :class="m.role">{{ m.role === 'user' ? '你' : 'AI' }}</div>
        <div class="bubble">
          <div v-if="m.tools.length" class="tools">
            <span v-for="(t, j) in m.tools" :key="j" class="tool" :class="{ done: t.done }">
              {{ t.done ? '✓ ' : '⚙ ' }}{{ t.name }}
            </span>
          </div>
          <span v-if="m.typing" class="typing">思考中…</span>
          <div v-if="m.html" class="body" v-html="m.html"></div>
          <div v-if="m.error" class="err">{{ m.error }}</div>
        </div>
      </div>
    </div>
  </div>

  <div class="compose">
    <div class="box">
      <textarea
        v-model="input"
        rows="1"
        placeholder="问点什么…（Enter 发送，Shift+Enter 换行）"
        @keydown="onKey"
      ></textarea>
      <button class="send" :disabled="busy" @click="send">➤</button>
    </div>
  </div>
</template>

<style scoped>
.top {
  display: flex; align-items: center; gap: 10px; padding: 12px 20px;
  border-bottom: 1px solid var(--line); background: var(--panel);
}
.top .title { font-weight: 600; }
.chip { font-size: 12px; padding: 5px 11px; border: 1px solid var(--line); border-radius: 20px; background: #fff; color: var(--ink2); }
.ro { background: var(--accent-soft); border-color: #a7ddd5; color: #0f766e; font-weight: 600; }
.stream { flex: 1; overflow: auto; padding: 24px 20px; }
.wrap { max-width: 780px; margin: 0 auto; display: flex; flex-direction: column; gap: 20px; }
.msg { display: flex; gap: 12px; }
.who { width: 30px; height: 30px; border-radius: 8px; flex: 0 0 30px; display: grid; place-items: center; font-weight: 700; font-size: 12px; }
.who.user { background: var(--gold-soft); color: var(--gold); }
.who.ai { background: linear-gradient(135deg, var(--accent), #0f766e); color: #fff; }
.bubble { flex: 1; min-width: 0; font-size: 14px; line-height: 1.65; }
.msg.u .bubble { background: #fff; border: 1px solid var(--line); border-radius: 12px; padding: 11px 14px; display: inline-block; flex: 0 1 auto; }
.tools { display: flex; flex-wrap: wrap; gap: 7px; margin: 2px 0 10px; }
.tool { display: inline-flex; align-items: center; gap: 6px; font-family: var(--mono); font-size: 11.5px; padding: 4px 9px; border-radius: 7px; background: var(--gold-soft); color: #8a6608; border: 1px solid #ecd9a8; }
.tool.done { background: var(--accent-soft); color: #0f766e; border-color: #a7ddd5; }
.bubble :deep(pre) { background: #1c1917; color: #e7e1d8; border-radius: 9px; padding: 11px 13px; overflow-x: auto; font-family: var(--mono); font-size: 12.5px; line-height: 1.6; }
.bubble :deep(code) { font-family: var(--mono); font-size: 12.5px; background: #f0ebe2; padding: 1px 5px; border-radius: 4px; }
.bubble :deep(pre code) { background: none; padding: 0; }
.err { color: var(--bad); background: var(--bad-soft); border: 1px solid #f2d6d1; padding: 9px 12px; border-radius: 9px; font-size: 13px; margin-top: 6px; }
.compose { padding: 14px 20px 20px; }
.box { max-width: 780px; margin: 0 auto; border: 1px solid var(--line); border-radius: 14px; background: #fff; padding: 10px 12px; display: flex; gap: 10px; align-items: flex-end; box-shadow: 0 6px 20px -14px rgba(0, 0, 0, .15); }
.box textarea { flex: 1; border: 0; outline: 0; resize: none; font-family: inherit; font-size: 14px; max-height: 140px; background: none; color: var(--ink); line-height: 1.5; }
.send { width: 34px; height: 34px; border: 0; border-radius: 9px; background: var(--accent); color: #fff; cursor: pointer; font-size: 16px; flex: 0 0 34px; }
.send:disabled { opacity: .5; cursor: not-allowed; }
.typing { display: inline-block; color: var(--ink3); font-size: 13px; }
</style>
