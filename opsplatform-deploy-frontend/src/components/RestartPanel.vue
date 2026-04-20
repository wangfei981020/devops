<template>
  <div class="panel">
    <!-- 输入区 -->
    <div class="input-block">
      <div class="ib-label">
        输入模块名，每行一个（<span class="mute-text">不带 tag</span>）
      </div>
      <textarea
        v-model="text"
        class="ta"
        spellcheck="false"
        placeholder="atmosphere-frontend&#10;base-client-backend&#10;bet-client-backend"
        @keydown.ctrl.enter="onPreview"
      ></textarea>
      <div class="input-foot">
        <button class="btn ghost" @click="onPreview" :disabled="!text.trim() || previewing">
          <span v-if="!previewing">预览</span>
          <span v-else>分析中...</span>
        </button>
        <span class="hint">
          容错：粘贴 <code>module:tag</code> 自动忽略冒号后 ·
          <kbd>Ctrl</kbd>+<kbd>↵</kbd> 预览
        </span>
      </div>
    </div>

    <!-- 预览区（仅在有内容时显示） -->
    <div v-if="preview.length" class="preview">
      <div class="pv-head">
        <span class="pv-title">重启清单</span>
        <span class="pv-summary">
          <span class="ok">{{ validCount }} 存在</span>
          <span v-if="invalidCount" class="err"> · {{ invalidCount }} 找不到</span>
        </span>
      </div>
      <div class="pv-list">
        <div v-for="m in preview" :key="m.name" :class="['pv-row', !m.exists && 'is-missing']">
          <span class="pv-mod mono">{{ m.name }}</span>
          <div class="pv-detail">
            <template v-if="m.exists">
              <span class="mute-text">当前</span>
              <span class="tag-curr mono">{{ shortTag(m.currentTag) }}</span>
            </template>
            <span v-else class="missing">DB 里找不到 · 将跳过</span>
          </div>
        </div>
      </div>
    </div>

    <!-- 底部执行条 -->
    <div v-if="preview.length" class="exec-bar">
      <div class="exec-info">
        调用 ArgoCD <b>Deployment Restart</b> · 不改动 git
        <span v-if="validCount"> · 将重启 <b class="mono">{{ validCount }}</b> 个</span>
      </div>
      <button class="exec-btn primary" :disabled="!validCount || submitting" @click="onRestart">
        <span v-if="submitting">重启中...</span>
        <span v-else>重启 {{ validCount }} 个</span>
        <svg v-if="!submitting" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" width="14" height="14"><path d="m9 18 6-6-6-6"/></svg>
      </button>
    </div>
  </div>
</template>

<script setup>
import { ref, computed } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { restartModules } from '../api'

const props = defineProps(['projectEnv', 'modules'])
const emit = defineEmits(['done'])
const text = ref('')
const preview = ref([])
const previewing = ref(false)
const submitting = ref(false)

const validCount = computed(() => preview.value.filter(p => p.exists).length)
const invalidCount = computed(() => preview.value.filter(p => !p.exists).length)

function shortTag(t) {
  if (!t) return '—'
  return t.length > 12 ? '…' + t.slice(-10) : t
}

function onPreview() {
  previewing.value = true
  try {
    // 前端直接基于 props.modules 生成预览，不用后端 API（更快、更准确）
    const seen = new Set()
    const out = []
    text.value.split('\n').forEach(line => {
      line = line.trim()
      if (!line || line.startsWith('#')) return
      const mod = line.includes(':') ? line.split(':')[0].trim() : line
      if (!mod || seen.has(mod)) return
      seen.add(mod)
      const m = props.modules.find(x => x.name === mod)
      if (m) {
        out.push({ name: mod, exists: true, currentTag: m.current_tag, id: m.id })
      } else {
        out.push({ name: mod, exists: false })
      }
    })
    preview.value = out
  } finally {
    previewing.value = false
  }
}

async function onRestart() {
  const names = preview.value.filter(p => p.exists).map(p => p.name)
  if (!names.length) return
  try {
    await ElMessageBox.confirm(`确认重启 ${names.length} 个模块？`, '重启确认')
  } catch (_) { return }
  submitting.value = true
  try {
    const r = await restartModules({ project_env_id: props.projectEnv.id, module_names: names })
    ElMessage.success(`已触发 · #${r.deployment_id}`)
    emit('done', r.deployment_id)
    text.value = ''
    preview.value = []
  } finally { submitting.value = false }
}
</script>

<style scoped>
.panel { display: flex; flex-direction: column; gap: 16px; }
.input-block {}
.ib-label { font-size: 12px; color: #64748b; margin-bottom: 8px; }
.mute-text { color: #94a3b8; font-size: 11.5px; }

.ta {
  width: 100%; min-height: 180px;
  font-family: var(--mono); font-size: 13px; line-height: 1.8;
  color: #0f172a;
  background: #fafbfc; border: 1px solid #e5e7eb; border-radius: 6px;
  padding: 14px 16px; resize: vertical; transition: all .15s;
}
.ta:focus {
  outline: none; background: #fff;
  border-color: #0f172a;
  box-shadow: 0 0 0 3px rgba(15,23,42,.05);
}
.ta::placeholder { color: #cbd5e1; }

.input-foot { display: flex; align-items: center; gap: 14px; margin-top: 10px; }
.hint { color: #94a3b8; font-size: 11.5px; flex: 1; }
.hint code {
  font-family: var(--mono); background: #f1f5f9;
  padding: 1px 5px; border-radius: 3px; color: #475569; font-size: 10.5px;
}
.hint kbd {
  font-family: var(--mono); font-size: 10.5px;
  background: #fff; border: 1px solid #e5e7eb; border-bottom-width: 2px;
  padding: 0 5px; border-radius: 3px; color: #475569;
}

.btn {
  background: #fff; border: 1px solid #e5e7eb; border-radius: 6px;
  padding: 7px 14px; font-size: 12.5px; font-weight: 500;
  color: #334155; cursor: pointer; transition: all .12s;
  font-family: var(--body);
}
.btn:hover:not(:disabled) { border-color: #0f172a; color: #0f172a; }
.btn:disabled { opacity: .4; cursor: not-allowed; }
.btn.ghost { background: transparent; }

.preview { border-top: 1px solid #eef1f4; padding-top: 14px; }
.pv-head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 8px; }
.pv-title {
  font-size: 11px; text-transform: uppercase; letter-spacing: 1px;
  color: #94a3b8; font-weight: 600;
}
.pv-summary { font-size: 12px; font-family: var(--mono); }
.pv-summary .ok { color: #059669; font-weight: 600; }
.pv-summary .err { color: #dc2626; }

.pv-row {
  display: grid; grid-template-columns: 1fr auto;
  padding: 8px 0; align-items: center; gap: 16px;
  border-bottom: 1px solid #f1f5f9;
}
.pv-row:last-child { border-bottom: none; }
.pv-row.is-missing .pv-mod { color: #94a3b8; }

.pv-mod { font-size: 13px; color: #0f172a; font-weight: 500; }
.pv-detail { display: flex; align-items: center; gap: 6px; }
.tag-curr { color: #334155; font-size: 12px; background: #f1f5f9; padding: 1px 6px; border-radius: 3px; }
.missing { color: #dc2626; font-size: 12px; }

.exec-bar {
  display: flex; align-items: center; justify-content: space-between;
  padding: 14px 0 0; margin-top: 4px; border-top: 1px solid #eef1f4;
}
.exec-info { font-size: 12px; color: #64748b; }
.exec-info b { color: #0f172a; font-weight: 600; }

.exec-btn {
  display: inline-flex; align-items: center; gap: 8px;
  padding: 9px 18px; border-radius: 6px;
  font-size: 13px; font-weight: 600; color: #fff;
  border: none; cursor: pointer; transition: all .15s;
  font-family: var(--body);
}
.exec-btn:disabled { opacity: .35; cursor: not-allowed; }
.exec-btn.primary { background: #0f172a; }
.exec-btn.primary:hover:not(:disabled) { background: #1e293b; }
</style>
