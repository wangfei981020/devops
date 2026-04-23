<template>
  <div class="up-panel">
    <div class="p-hd">
      <h3>
        <el-icon><Upload /></el-icon>
        批量更新镜像
      </h3>
      <span class="sub">空行忽略 · # 注释 · 同模块取最后一条</span>
    </div>

    <div class="ws-grid">
      <!-- 输入 -->
      <div class="ws-col in">
        <div class="ws-sub">输入</div>
        <textarea
          v-model="text"
          class="ta"
          spellcheck="false"
          placeholder="atmosphere-frontend:20260416014126-83
base-client-backend:20260416020000-99"
          @keydown.ctrl.enter="onPreview"
        ></textarea>
        <div class="ta-ft">
          <button class="btn ghost" @click="onPreview" :disabled="!text.trim() || previewing">
            <span v-if="!previewing">预览变更</span>
            <span v-else>分析中...</span>
          </button>
          <span class="hint">
            支持 <kbd>Ctrl</kbd>+<kbd>↵</kbd> · 粘贴 <code>模块:tag</code>
          </span>
        </div>
      </div>

      <!-- 预览 -->
      <div class="ws-col">
        <div class="ws-sub">变更预览</div>
        <div v-if="!diff.length" class="empty-pv">
          <div class="ep-t">未预览</div>
          <div class="ep-d">在左侧输入后点「预览变更」</div>
        </div>
        <template v-else>
          <div class="pv-hd">
            <span class="pv-total">{{ diff.length }} modules</span>
            <span class="pv-sum">
              <span class="ok">✓ {{ validCount }} ready</span>
              <span v-if="skipCount" class="mute"> · {{ skipCount }} 无变化</span>
              <span v-if="newCount" class="warn"> · {{ newCount }} 未知</span>
            </span>
          </div>
          <div class="pv-table-wrap">
            <table class="pv-table">
              <thead>
                <tr>
                  <th style="width:32%;">模块</th>
                  <th>Tag 变化</th>
                  <th style="width:32%;">values.yaml</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="d in sortedDiff" :key="d.module" :class="[d.skip && 'is-skip', d.is_new && 'is-new']">
                  <td class="pv-mod">{{ d.module }}</td>
                  <td>
                    <span v-if="d.is_new" class="warn-text">git 找不到 · 跳过</span>
                    <span v-else-if="d.skip" class="mute-text">无变化</span>
                    <span v-else class="pv-chg">
                      <span class="from">{{ shortTag(d.from_tag) }}</span>
                      <span class="arr">→</span>
                      <span class="to">{{ shortTag(d.to_tag) }}</span>
                    </span>
                  </td>
                  <td class="pv-path">{{ d.path || '—' }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </template>
      </div>
    </div>

    <!-- 提交条 -->
    <div class="exec" v-if="diff.length">
      <div class="exec-info">
        将提交到 <b>{{ repoShort }}</b> · 分支 <b>{{ projectEnv.git_branch }}</b>
        <span v-if="validCount"> · 涉及 <b>{{ validCount }}</b> 个模块</span>
      </div>
      <button
        v-if="!isProd || auth.isAdmin"
        :class="['cta', isProd ? 'danger' : 'success']"
        :disabled="!validCount || submitting"
        @click="onSubmit">
        <span v-if="submitting">提交中...</span>
        <span v-else-if="isProd">提交到 {{ projectEnv.name }} · 需二次确认</span>
        <span v-else>提交到 {{ projectEnv.name }} · {{ validCount }} 个</span>
        <el-icon v-if="!submitting"><ArrowRight /></el-icon>
      </button>
      <span v-else class="no-perm-hint">⚠ PROD 发布仅限管理员</span>
    </div>
  </div>
</template>

<script setup>
import { ref, computed } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Upload, ArrowRight } from '@element-plus/icons-vue'
import { previewImage, updateImage } from '../api'
import { useAuthStore } from '../stores/auth'
import { useDeploymentsStore } from '../stores/deployments'

const props = defineProps(['projectEnv', 'modules'])
const emit = defineEmits(['done'])
const auth = useAuthStore()
const deployments = useDeploymentsStore()
const text = ref('')
const diff = ref([])
const previewing = ref(false)
const submitting = ref(false)

const isProd = computed(() => props.projectEnv?.env_type === 'prod')
const validCount = computed(() => diff.value.filter(d => !d.skip && !d.is_new).length)
const skipCount = computed(() => diff.value.filter(d => d.skip).length)
const newCount = computed(() => diff.value.filter(d => d.is_new).length)
const repoShort = computed(() => (props.projectEnv?.git_repo || '').split('/').pop() || '?')

const sortedDiff = computed(() => {
  return [...diff.value].sort((a, b) => {
    const order = d => (d.is_new ? 2 : d.skip ? 1 : 0)
    return order(a) - order(b)
  })
})

function shortTag(t) {
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
    const env = props.projectEnv.name
    try {
      await ElMessageBox.confirm(
        `你正在向 【${env}】 提交 ${changes.length} 个模块到 GitLab，操作不可撤销。`,
        '⚠ 生产环境二次确认',
        {
          type: 'warning',
          confirmButtonText: `确认提交到 ${env}`,
          cancelButtonText: '取消',
          confirmButtonClass: 'el-button--danger',
        }
      )
    } catch (_) { return }
  }
  submitting.value = true
  try {
    const r = await updateImage({ project_env_id: props.projectEnv.id, changes })
    ElMessage.success(`已提交 · #${r.deployment_id} · 进度看右下角浮动条`)
    deployments.startTracking(r.deployment_id, {
      action: 'update_image',
      envName: props.projectEnv.name,
      envType: props.projectEnv.env_type,
      modules: changes.length,
      operator: auth.user?.username || '',
    })
    emit('done', r.deployment_id)
    text.value = ''
    diff.value = []
  } finally { submitting.value = false }
}
</script>

<style scoped>
.up-panel { display: flex; flex-direction: column; }

.p-hd { padding: 14px 20px; border-bottom: 1px solid var(--border-soft); display: flex; justify-content: space-between; align-items: center; }
.p-hd h3 { font: 600 14px/1 var(--body); color: var(--text); display: flex; align-items: center; gap: 8px; }
.p-hd h3 .el-icon { color: var(--primary); font-size: 16px; }
.p-hd .sub { font: 500 11.5px var(--mono); color: var(--text-3); }

.ws-grid { display: grid; grid-template-columns: 1.3fr 1fr; gap: 0; }
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
.ta:focus {
  outline: none; border-color: var(--primary);
  box-shadow: 0 0 0 3px rgba(59, 130, 246, .12);
  background: #fff;
}
.ta::placeholder { color: var(--text-3); }

.ta-ft { display: flex; justify-content: space-between; align-items: center; margin-top: 12px; }
.btn {
  background: #fff; border: 1px solid var(--border); color: var(--text);
  padding: 6px 14px; border-radius: 5px;
  font: 500 12.5px var(--body); cursor: pointer;
}
.btn.ghost:hover { border-color: var(--primary); color: var(--primary); }
.btn:disabled { opacity: .4; cursor: not-allowed; }

.hint { font-size: 11.5px; color: var(--text-3); }
.hint code { font-family: var(--mono); background: var(--bg-hover); padding: 1px 5px; border-radius: 3px; color: var(--text-2); font-size: 11px; }
.hint kbd { font-family: var(--mono); font-size: 10.5px; background: var(--bg-hover); border: 1px solid var(--border); padding: 0 5px; border-radius: 3px; color: var(--text-2); }

.empty-pv { padding: 40px 0; text-align: center; color: var(--text-3); }
.empty-pv .ep-t { font-size: 13px; color: var(--text-2); font-weight: 500; margin-bottom: 4px; }
.empty-pv .ep-d { font-size: 11.5px; }

.pv-hd { display: flex; justify-content: space-between; align-items: center; margin-bottom: 8px; margin-top: -4px; }
.pv-total { font-size: 11.5px; color: var(--text-3); font-family: var(--mono); }
.pv-sum { font-size: 11.5px; font-family: var(--mono); }
.pv-sum .ok { color: var(--success); font-weight: 600; }
.pv-sum .mute { color: var(--text-3); }
.pv-sum .warn { color: var(--warning); }

.pv-table-wrap { border: 1px solid var(--border); border-radius: 5px; overflow: auto; max-height: 380px; background: #fff; }
.pv-table { width: 100%; border-collapse: collapse; font-size: 12.5px; }
.pv-table thead { position: sticky; top: 0; z-index: 1; }
.pv-table th { background: #f9fafb; text-align: left; padding: 8px 10px; border-bottom: 1px solid var(--border); color: var(--text-3); font: 600 10.5px var(--body); text-transform: uppercase; letter-spacing: .5px; }
.pv-table td { padding: 9px 10px; border-bottom: 1px solid var(--border-soft); vertical-align: middle; }
.pv-table tr:last-child td { border-bottom: none; }
.pv-table tr:hover td { background: #fafbfc; }
.pv-table tr.is-skip .pv-mod, .pv-table tr.is-new .pv-mod { color: var(--text-3); }
.pv-mod { color: var(--text); font-size: 12.5px; font-weight: 500; font-family: var(--mono); }
.pv-path { font-family: var(--mono); font-size: 10.5px; color: var(--text-3); word-break: break-all; }
.pv-chg { font-family: var(--mono); font-size: 11.5px; display: inline-flex; gap: 6px; align-items: center; }
.from { color: var(--text-3); text-decoration: line-through; }
.arr { color: var(--text-3); }
.to { color: var(--success); font-weight: 600; }
.mute-text { color: var(--text-3); font-size: 11.5px; }
.warn-text { color: var(--warning); font-size: 11.5px; }

.exec {
  border-top: 1px solid var(--border-soft);
  background: #f0fdf4; padding: 14px 22px;
  display: flex; justify-content: space-between; align-items: center;
  border-bottom-left-radius: var(--radius); border-bottom-right-radius: var(--radius);
}
.exec-info { font-size: 12.5px; color: #166534; }
.exec-info b { color: #14532d; font-family: var(--mono); font-weight: 600; }

.cta {
  background: var(--success); color: #fff; border: none;
  padding: 10px 22px; border-radius: 5px;
  font: 600 13.5px var(--body); cursor: pointer;
  display: flex; gap: 8px; align-items: center;
}
.cta:hover:not(:disabled) { background: var(--success-dark); }
.cta:disabled { opacity: .4; cursor: not-allowed; }
.cta.danger { background: var(--danger); }
.cta.danger:hover:not(:disabled) { background: var(--danger-dark); }

.no-perm-hint {
  font-size: 12.5px; color: var(--warning);
  padding: 10px 16px; border: 1px dashed var(--warning);
  border-radius: 5px;
}
.cta .el-icon { font-size: 14px; }
</style>
