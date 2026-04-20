<template>
  <div class="rs-panel">
    <div class="p-hd">
      <h3>
        <el-icon><RefreshRight /></el-icon>
        批量重启服务
      </h3>
      <span class="sub">每行一个模块名 · 带 :tag 自动取前半段</span>
    </div>

    <div class="ws-grid">
      <div class="ws-col in">
        <div class="ws-sub">输入模块名</div>
        <textarea
          v-model="text"
          class="ta"
          spellcheck="false"
          placeholder="atmosphere-frontend
base-client-backend
bet-client-backend"
          @keydown.ctrl.enter="onPreview"
        ></textarea>
        <div class="ta-ft">
          <button class="btn ghost" @click="onPreview" :disabled="!text.trim()">预览</button>
          <span class="hint">
            支持 <kbd>Ctrl</kbd>+<kbd>↵</kbd> · 不带 tag
          </span>
        </div>
      </div>

      <div class="ws-col">
        <div class="ws-sub">重启清单</div>
        <div v-if="!preview.length" class="empty-pv">
          <div class="ep-t">未预览</div>
          <div class="ep-d">在左侧输入模块名后点「预览」</div>
        </div>
        <template v-else>
          <div class="pv-hd">
            <span class="pv-total">{{ preview.length }} modules</span>
            <span class="pv-sum">
              <span class="ok">✓ {{ validCount }} 存在</span>
              <span v-if="invalidCount" class="err"> · ✗ {{ invalidCount }} 找不到</span>
            </span>
          </div>
          <div class="pv-list">
            <div v-for="m in preview" :key="m.name" :class="['pv-row', !m.exists && 'is-missing']">
              <span class="pv-mod">{{ m.name }}</span>
              <span class="pv-detail" v-if="m.exists">
                <span class="mute-text">当前</span>
                <span class="tag-curr">{{ shortTag(m.currentTag) }}</span>
              </span>
              <span class="pv-detail missing" v-else>DB 里找不到 · 跳过</span>
            </div>
          </div>
        </template>
      </div>
    </div>

    <div class="exec" v-if="preview.length">
      <div class="exec-info">
        调用 ArgoCD <b>Deployment Restart</b> · 不改动 git
        <span v-if="validCount"> · 将重启 <b>{{ validCount }}</b> 个</span>
      </div>
      <button class="cta primary" :disabled="!validCount || submitting" @click="onRestart">
        <span v-if="submitting">重启中...</span>
        <span v-else>重启 {{ validCount }} 个</span>
        <el-icon v-if="!submitting"><ArrowRight /></el-icon>
      </button>
    </div>
  </div>
</template>

<script setup>
import { ref, computed } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { RefreshRight, ArrowRight } from '@element-plus/icons-vue'
import { restartModules } from '../api'

const props = defineProps(['projectEnv', 'modules'])
const emit = defineEmits(['done'])
const text = ref('')
const preview = ref([])
const submitting = ref(false)

const validCount = computed(() => preview.value.filter(p => p.exists).length)
const invalidCount = computed(() => preview.value.filter(p => !p.exists).length)

function shortTag(t) {
  if (!t) return '—'
  return t.length > 12 ? '…' + t.slice(-10) : t
}

function onPreview() {
  const seen = new Set()
  const out = []
  text.value.split('\n').forEach(line => {
    line = line.trim()
    if (!line || line.startsWith('#')) return
    const mod = line.includes(':') ? line.split(':')[0].trim() : line
    if (!mod || seen.has(mod)) return
    seen.add(mod)
    const m = props.modules.find(x => x.name === mod)
    if (m) out.push({ name: mod, exists: true, currentTag: m.current_tag, id: m.id })
    else out.push({ name: mod, exists: false })
  })
  preview.value = out
}

async function onRestart() {
  const names = preview.value.filter(p => p.exists).map(p => p.name)
  if (!names.length) return
  try { await ElMessageBox.confirm(`确认重启 ${names.length} 个模块？`, '重启确认') } catch (_) { return }
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
.rs-panel { display: flex; flex-direction: column; }

.p-hd { padding: 14px 20px; border-bottom: 1px solid var(--border-soft); display: flex; justify-content: space-between; align-items: center; }
.p-hd h3 { font: 600 14px/1 var(--body); color: var(--text); display: flex; align-items: center; gap: 8px; }
.p-hd h3 .el-icon { color: var(--primary); font-size: 16px; }
.p-hd .sub { font: 500 11.5px var(--mono); color: var(--text-3); }

.ws-grid { display: grid; grid-template-columns: 1.3fr 1fr; }
.ws-col { padding: 16px 20px; }
.ws-col.in { border-right: 1px solid var(--border-soft); }
.ws-sub { font-size: 11px; color: var(--text-3); text-transform: uppercase; letter-spacing: .8px; font-weight: 600; margin-bottom: 10px; }

.ta {
  width: 100%; min-height: 180px;
  background: var(--bg-input); border: 1px solid var(--border); border-radius: 5px;
  padding: 12px 14px; color: var(--text);
  font: 500 13px/1.85 var(--mono);
  resize: vertical; transition: all .15s;
}
.ta:focus { outline: none; border-color: var(--primary); box-shadow: 0 0 0 3px rgba(59,130,246,.12); background: #fff; }
.ta::placeholder { color: var(--text-3); }

.ta-ft { display: flex; justify-content: space-between; align-items: center; margin-top: 12px; }
.btn { background: #fff; border: 1px solid var(--border); color: var(--text); padding: 6px 14px; border-radius: 5px; font: 500 12.5px var(--body); cursor: pointer; }
.btn.ghost:hover { border-color: var(--primary); color: var(--primary); }
.btn:disabled { opacity: .4; cursor: not-allowed; }

.hint { font-size: 11.5px; color: var(--text-3); }
.hint kbd { font-family: var(--mono); font-size: 10.5px; background: var(--bg-hover); border: 1px solid var(--border); padding: 0 5px; border-radius: 3px; color: var(--text-2); }

.empty-pv { padding: 40px 0; text-align: center; color: var(--text-3); }
.empty-pv .ep-t { font-size: 13px; color: var(--text-2); font-weight: 500; margin-bottom: 4px; }
.empty-pv .ep-d { font-size: 11.5px; }

.pv-hd { display: flex; justify-content: space-between; align-items: center; margin-bottom: 8px; margin-top: -4px; }
.pv-total { font-size: 11.5px; color: var(--text-3); font-family: var(--mono); }
.pv-sum { font-size: 11.5px; font-family: var(--mono); }
.pv-sum .ok { color: var(--success); font-weight: 600; }
.pv-sum .err { color: var(--danger); }

.pv-row { padding: 9px 0; border-bottom: 1px solid var(--border-soft); display: grid; grid-template-columns: 1fr auto; gap: 12px; align-items: center; }
.pv-row:last-child { border-bottom: none; }
.pv-row.is-missing .pv-mod { color: var(--text-3); }
.pv-mod { color: var(--text); font-size: 12.5px; font-weight: 500; }
.pv-detail { font-family: var(--mono); font-size: 11.5px; display: flex; gap: 6px; align-items: center; }
.mute-text { color: var(--text-3); }
.tag-curr { color: var(--text-2); background: var(--bg-hover); padding: 1px 6px; border-radius: 3px; }
.missing { color: var(--danger); }

.exec {
  border-top: 1px solid var(--border-soft);
  background: var(--primary-bg); padding: 14px 22px;
  display: flex; justify-content: space-between; align-items: center;
  border-bottom-left-radius: var(--radius); border-bottom-right-radius: var(--radius);
}
.exec-info { font-size: 12.5px; color: #1e40af; }
.exec-info b { color: #1e3a8a; font-family: var(--mono); font-weight: 600; }

.cta {
  background: var(--primary); color: #fff; border: none;
  padding: 10px 22px; border-radius: 5px;
  font: 600 13.5px var(--body); cursor: pointer;
  display: flex; gap: 8px; align-items: center;
}
.cta:hover:not(:disabled) { background: var(--primary-dark); }
.cta:disabled { opacity: .4; cursor: not-allowed; }
.cta .el-icon { font-size: 14px; }
</style>
