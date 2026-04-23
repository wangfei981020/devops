<template>
  <el-dialog v-model="vis" title="回滚发布" width="720px" @open="onOpen">
    <div v-loading="loading">
      <div class="hint">勾选需要回滚的模块。默认全选（仅可回滚的），请取消已验证通过的。</div>
      <el-table :data="rows" size="small" @selection-change="onSel" ref="tbl">
        <el-table-column type="selection" width="40" :selectable="r => r.can_rollback" />
        <el-table-column label="模块" prop="module" min-width="200">
          <template #default="{ row }">
            <span class="mono">{{ row.module }}</span>
            <el-tag v-if="!row.can_rollback" size="small" type="warning" effect="plain" style="margin-left:6px;">
              已被后续发布修改
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="当前 tag">
          <template #default="{ row }"><span class="mono" style="font-size:11.5px;">{{ row.from_tag || '—' }}</span></template>
        </el-table-column>
        <el-table-column label="回滚到">
          <template #default="{ row }"><span class="mono" style="font-size:11.5px;color:var(--success);">{{ row.to_tag }}</span></template>
        </el-table-column>
      </el-table>
      <div class="summary">已选 <b>{{ selected.length }} / {{ rows.length }}</b> · 将生成一条 rollback 发布记录</div>
    </div>
    <template #footer>
      <el-button @click="vis = false">取消</el-button>
      <el-button type="primary" :disabled="!selected.length" :loading="submitting" @click="onRollback">
        回滚选中的 {{ selected.length }} 个
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { computed, ref, nextTick } from 'vue'
import { ElMessage } from 'element-plus'
import { getRollbackPreview, rollback } from '../api'
import { useDeploymentsStore } from '../stores/deployments'

const deployments = useDeploymentsStore()

const props = defineProps(['modelValue', 'deploymentId'])
const emit = defineEmits(['update:modelValue', 'done'])
const vis = computed({
  get: () => props.modelValue,
  set: v => emit('update:modelValue', v)
})
const rows = ref([])
const selected = ref([])
const loading = ref(false)
const submitting = ref(false)
const tbl = ref(null)

async function onOpen() {
  if (!props.deploymentId) return
  loading.value = true
  try {
    const r = await getRollbackPreview(props.deploymentId)
    rows.value = r.modules || []
    await nextTick()
    rows.value.forEach(row => {
      if (row.can_rollback) tbl.value?.toggleRowSelection(row, true)
    })
  } finally { loading.value = false }
}
function onSel(rs) { selected.value = rs }

async function onRollback() {
  submitting.value = true
  try {
    const r = await rollback({
      ref_deployment_id: props.deploymentId,
      selected_modules: selected.value.map(s => s.module)
    })
    ElMessage.success(`回滚已提交 · #${r.deployment_id} · 进度看右下角浮动条`)
    if (r?.deployment_id) {
      deployments.startTracking(r.deployment_id, {
        action: 'rollback',
        modules: selected.value.length,
      })
    }
    vis.value = false
    emit('done')
  } finally { submitting.value = false }
}
</script>

<style scoped>
.hint { font-size: 12.5px; color: var(--text-2); margin-bottom: 10px; }
.summary { margin-top: 12px; padding: 8px 12px; background: #f9fafb; border-radius: 4px; font-size: 12px; color: var(--text-2); }
.summary b { color: var(--text); font-family: var(--mono); }
</style>
