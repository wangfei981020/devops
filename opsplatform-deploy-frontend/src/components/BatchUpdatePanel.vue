<template>
  <div class="panel">
    <!-- 输入区 -->
    <div class="input-block">
      <div class="ib-label">
        输入 <code>模块名:tag</code>，每行一个
      </div>
      <textarea
        ref="taRef"
        v-model="text"
        class="ta"
        spellcheck="false"
        placeholder="atmosphere-frontend:20260416014126-83&#10;base-client-backend:20260416020000-99"
        @keydown.ctrl.enter="onPreview"
      ></textarea>
      <div class="input-foot">
        <button class="btn ghost" @click="onPreview" :disabled="!text.trim() || previewing">
          <span v-if="!previewing">预览变更</span>
          <span v-else>分析中...</span>
        </button>
        <span class="hint">空行忽略 · <code>#</code> 注释 · 同模块取最后一条 · <kbd>Ctrl</kbd>+<kbd>↵</kbd> 预览</span>
      </div>
    </div>

    <!-- 预览区（空状态不显示，不占位） -->
    <div v-if="diff.length" class="preview">
      <div class="pv-head">
        <span class="pv-title">变更预览</span>
        <span class="pv-summary">
          <span class="ok">{{ diff.filter(d => !d.skip && !d.is_new).length }} 改动</span>
          <span v-if="diff.filter(d => d.skip).length" class="mute"> · {{ diff.filter(d => d.skip).length }} 无变化</span>
          <span v-if="diff.filter(d => d.is_new).length" class="warn"> · {{ diff.filter(d => d.is_new).length }} 未知</span>
        </span>
      </div>
      <div class="pv-list">
        <div v-for="d in sortedDiff" :key="d.module" :class="['pv-row', d.skip && 'is-skip', d.is_new && 'is-new']">
          <span class="pv-mod mono">{{ d.module }}</span>
          <div class="pv-change">
            <template v-if="d.is_new">
              <span class="warn-text">git 里找不到 · 将跳过</span>
            </template>
            <template v-else-if="d.skip">
              <span class="mute-text mono">{{ d.from_tag }}  ·  无变化</span>
            </template>
            <template v-else>
              <span class="from mono">{{ shortTag(d.from_tag) }}</span>
              <span class="arrow">→</span>
              <span class="to mono">{{ shortTag(d.to_tag) }}</span>
            </template>
          </div>
        </div>
      </div>
    </div>

    <!-- 底部执行条 -->
    <div v-if="diff.length" class="exec-bar">
      <div class="exec-info">
        将提交到 <b class="mono">{{ projectEnv.git_repo.split('/').pop() }}</b> ·
        分支 <b class="mono">{{ projectEnv.git_branch }}</b> ·
        <span v-if="validCount">{{ validCount }} 个模块</span>
        <span v-else class="mute-text">无有效变更</span>
      </div>
      <button
        :class="['exec-btn', isProd ? 'danger' : 'success']"
        :disabled="!validCount || submitting"
        @click="onSubmit">
        <span v-if="submitting">提交中...</span>
        <span v-else-if="isProd">提交 PROD ·需二次确认</span>
        <span v-else>提交并同步 UAT</span>
        <svg v-if="!submitting" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" width="14" height="14"><path d="m9 18 6-6-6-6"/></svg>
      </button>
    </div>
  </div>
</template>

<script setup>
import { ref, computed } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { previewImage, updateImage } from '../api'

const props = defineProps(['projectEnv', 'modules'])
const emit = defineEmits(['done'])
const taRef = ref(null)
const text = ref('')
const diff = ref([])
const previewing = ref(false)
const submitting = ref(false)
const isProd = computed(() => props.projectEnv?.env_type === 'prod')
const validCount = computed(() => diff.value.filter(d => !d.skip && !d.is_new).length)
const sortedDiff = computed(() => {
  // 有效改动在前，无变化其次，未知最后
  return [...diff.value].sort((a, b) => {
    const order = (d) => (d.is_new ? 2 : d.skip ? 1 : 0)
    return order(a) - order(b)
  })
})

function shortTag(t) {
  // 长 tag (如 20260416014126-83) 保留尾部 8 位
  if (!t) return '—'
  return t.length > 12 ? '…' + t.slice(-10) : t
}

async function onPreview() {
  if (!text.value.trim()) return
  previewing.value = true
  try {
    const r = await previewImage({ project_env_id: props.projectEnv.id, text: text.value })
    diff.value = r.diff || []
  } finally { previewing.value = false }
}

async function onSubmit() {
  const changes = diff.value.filter(d => !d.skip && !d.is_new).map(d => ({ module: d.module, tag: d.to_tag }))
  if (!changes.length) return
  if (isProd.value) {
    try {
      await ElMessageBox.confirm(
        `PROD 将提交 ${changes.length} 个模块到 GitLab，不可撤销。`,
        '二次确认', { type: 'warning', confirmButtonText: '确认提交', cancelButtonText: '取消' }
      )
    } catch (_) { return }
  }
  submitting.value = true
  try {
    const r = await updateImage({ project_env_id: props.projectEnv.id, changes })
    ElMessage.success(`已提交 · #${r.deployment_id}`)
    emit('done', r.deployment_id)
    text.value = ''
    diff.value = []
  } finally { submitting.value = false }
}
</script>

<style scoped>
.panel { display: flex; flex-direction: column; gap: 16px; }

/* 输入区 */
.input-block {}
.ib-label { font-size: 12px; color: #64748b; margin-bottom: 8px; }
.ib-label code {
  font-family: var(--mono); background: #f1f5f9;
  padding: 1px 6px; border-radius: 3px; color: #475569; font-size: 11px;
}
.ta {
  width: 100%; min-height: 180px;
  font-family: var(--mono); font-size: 13px; line-height: 1.8;
  color: #0f172a;
  background: #fafbfc; border: 1px solid #e5e7eb; border-radius: 6px;
  padding: 14px 16px;
  resize: vertical;
  transition: all .15s;
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

/* 按钮 */
.btn {
  background: #fff; border: 1px solid #e5e7eb; border-radius: 6px;
  padding: 7px 14px; font-size: 12.5px; font-weight: 500;
  color: #334155; cursor: pointer; transition: all .12s;
  font-family: var(--body);
}
.btn:hover:not(:disabled) { border-color: #0f172a; color: #0f172a; }
.btn:disabled { opacity: .4; cursor: not-allowed; }
.btn.ghost { background: transparent; }

/* 预览 */
.preview { border-top: 1px solid #eef1f4; padding-top: 14px; }
.pv-head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 8px; }
.pv-title {
  font-size: 11px; text-transform: uppercase; letter-spacing: 1px;
  color: #94a3b8; font-weight: 600;
}
.pv-summary { font-size: 12px; font-family: var(--mono); }
.pv-summary .ok { color: #059669; font-weight: 600; }
.pv-summary .mute { color: #94a3b8; }
.pv-summary .warn { color: #d97706; }

.pv-list { }
.pv-row {
  display: grid; grid-template-columns: 1fr auto;
  padding: 8px 0; align-items: center; gap: 16px;
  border-bottom: 1px solid #f1f5f9;
}
.pv-row:last-child { border-bottom: none; }
.pv-row.is-skip .pv-mod { color: #94a3b8; }
.pv-row.is-new .pv-mod { color: #94a3b8; }
.pv-mod { font-size: 13px; color: #0f172a; font-weight: 500; }

.pv-change { display: flex; align-items: center; gap: 8px; }
.from { color: #94a3b8; text-decoration: line-through; font-size: 12px; }
.arrow { color: #cbd5e1; font-size: 12px; }
.to { color: #059669; font-weight: 600; font-size: 12px; }
.mute-text { color: #94a3b8; font-size: 11.5px; }
.warn-text { color: #d97706; font-size: 12px; }

/* 底部执行条 */
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
.exec-btn.success { background: #059669; }
.exec-btn.success:hover:not(:disabled) { background: #047857; }
.exec-btn.danger { background: #dc2626; }
.exec-btn.danger:hover:not(:disabled) { background: #b91c1c; }
</style>
