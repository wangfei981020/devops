<template>
  <div class="batch">
    <div class="head">
      <div class="title">📝 批量更新镜像</div>
      <span class="sub">粘贴 模块名:tag 每行一个</span>
    </div>
    <div class="body">
      <el-input v-model="text" type="textarea" :rows="6"
        placeholder="atmosphere-frontend:20260416014126-83
base-client-backend:20260416020000-99" class="mono" />
      <div class="actions">
        <el-button @click="onPreview" :loading="previewing">预览变更</el-button>
        <span class="hint">空行忽略 · # 开头视为注释 · 同模块重复行取最后一条</span>
      </div>

      <el-table v-if="diff.length" :data="diff" size="small" class="diff-table">
        <el-table-column label="模块" prop="module" min-width="180">
          <template #default="{ row }">
            <span class="mono">{{ row.module }}</span>
            <el-tag v-if="row.is_new" type="warning" size="small" effect="plain" style="margin-left:6px;">NEW</el-tag>
            <el-tag v-if="row.skip" type="info" size="small" effect="plain" style="margin-left:6px;">NO-CHANGE</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="Tag 变化">
          <template #default="{ row }">
            <span class="tag-old">{{ row.from_tag || '—' }}</span>
            <span class="arrow">→</span>
            <span class="tag-new">{{ row.to_tag }}</span>
          </template>
        </el-table-column>
        <el-table-column label="values.yaml 路径" width="280">
          <template #default="{ row }"><span class="path">{{ row.path || '—' }}</span></template>
        </el-table-column>
      </el-table>
    </div>
    <div class="foot">
      <span class="info" v-if="diff.length">
        将提交到 <b>{{ projectEnv.git_repo }}</b> · 分支 <b>{{ projectEnv.git_branch }}</b>
      </span>
      <span v-else></span>
      <el-button :disabled="!diff.length" :type="isProd ? 'danger' : 'success'" size="default" @click="onSubmit" :loading="submitting">
        {{ isProd ? '提交 PROD · 需二次确认' : '提交并同步 UAT' }}
      </el-button>
    </div>
  </div>
</template>

<script setup>
import { ref, computed } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { previewImage, updateImage } from '../api'

const props = defineProps(['projectEnv', 'modules'])
const emit = defineEmits(['done'])
const text = ref('')
const diff = ref([])
const previewing = ref(false)
const submitting = ref(false)
const isProd = computed(() => props.projectEnv?.env_type === 'prod')

async function onPreview() {
  if (!text.value.trim()) { ElMessage.warning('请输入'); return }
  previewing.value = true
  try {
    const r = await previewImage({ project_env_id: props.projectEnv.id, text: text.value })
    diff.value = r.diff || []
  } finally { previewing.value = false }
}

async function onSubmit() {
  const changes = diff.value.filter(d => !d.skip && !d.is_new).map(d => ({ module: d.module, tag: d.to_tag }))
  if (!changes.length) { ElMessage.warning('没有有效的变更（NEW 模块还不支持创建、NO-CHANGE 自动跳过）'); return }
  if (isProd.value) {
    try {
      await ElMessageBox.confirm(
        `PROD 环境将提交 ${changes.length} 个模块到 GitLab，不可撤销。确认？`,
        '二次确认', { type: 'warning', confirmButtonText: '确认提交', cancelButtonText: '取消' }
      )
    } catch (_) { return }
  }
  submitting.value = true
  try {
    const r = await updateImage({ project_env_id: props.projectEnv.id, changes })
    ElMessage.success(`已提交 (deployment #${r.deployment_id})，同步中...`)
    emit('done', r.deployment_id)
    text.value = ''
    diff.value = []
  } finally { submitting.value = false }
}
</script>

<style scoped>
.head { padding: 12px 16px; border-bottom: 1px solid var(--border-soft); display: flex; align-items: center; justify-content: space-between; }
.title { font-weight: 600; font-size: 14px; }
.sub { color: var(--text-3); font-size: 11px; font-family: var(--mono); }
.body { padding: 14px 16px; }
.actions { margin-top: 10px; display: flex; gap: 10px; align-items: center; }
.hint { font-size: 11.5px; color: var(--text-3); }
.diff-table { margin-top: 12px; }
.tag-old { font-family: var(--mono); font-size: 11.5px; padding: 2px 6px; border-radius: 3px; background: #f3f4f6; color: var(--text-3); text-decoration: line-through; }
.tag-new { font-family: var(--mono); font-size: 11.5px; padding: 2px 6px; border-radius: 3px; background: #ecfdf5; color: #059669; font-weight: 500; }
.arrow { color: var(--text-3); margin: 0 6px; }
.path { font-family: var(--mono); font-size: 10.5px; color: var(--text-3); }
.foot { padding: 10px 16px; border-top: 1px solid var(--border-soft); background: #fafbfc; display: flex; align-items: center; justify-content: space-between; }
.info { font-size: 12px; color: var(--text-2); }
</style>
