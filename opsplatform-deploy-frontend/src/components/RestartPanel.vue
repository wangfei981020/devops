<template>
  <div class="restart">
    <div class="head">
      <div class="title">🔄 重启服务</div>
      <span class="sub">调用 ArgoCD Deployment Restart · 已选 <b class="mono" style="color:var(--primary);">{{ selected.length }}</b> 个</span>
    </div>
    <div class="body">
      <el-input v-model="filter" placeholder="筛选模块名..." size="small" />
      <el-table :data="filtered" @selection-change="onSel" size="small" max-height="440" style="margin-top:8px;">
        <el-table-column type="selection" width="40" />
        <el-table-column label="模块" prop="name" min-width="220">
          <template #default="{ row }"><span class="mono">{{ row.name }}</span></template>
        </el-table-column>
        <el-table-column label="当前 tag" width="200">
          <template #default="{ row }"><span class="mono" style="color:var(--text-2);font-size:11.5px;">{{ row.current_tag }}</span></template>
        </el-table-column>
        <el-table-column label="操作" width="90">
          <template #default="{ row }">
            <el-dropdown size="small" trigger="click"
              @visible-change="v => v && loadHistoryOnDemand(row)"
              @command="(t) => onHistorySelect(row, t)">
              <el-button link type="primary" size="small">历史▾</el-button>
              <template #dropdown>
                <el-dropdown-menu>
                  <el-dropdown-item v-if="row._loading" disabled>加载中...</el-dropdown-item>
                  <el-dropdown-item v-else-if="!row._history || !row._history.length" disabled>无历史</el-dropdown-item>
                  <el-dropdown-item v-for="h in (row._history || [])" :key="h.tag" :command="h.tag">
                    <span class="mono" style="font-size:11px;">{{ h.tag }}</span>
                  </el-dropdown-item>
                </el-dropdown-menu>
              </template>
            </el-dropdown>
          </template>
        </el-table-column>
      </el-table>
    </div>
    <div class="foot">
      <span class="info">将对选中的模块调用 ArgoCD restart action · 不改动 git</span>
      <el-button type="primary" :disabled="!selected.length" :loading="running" @click="onRestart">
        重启选中 {{ selected.length }} 个
      </el-button>
    </div>
  </div>
</template>

<script setup>
import { ref, computed } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { restartModules, getModuleTagHistory, updateImage } from '../api'

const props = defineProps(['projectEnv', 'modules'])
const filter = ref('')
const selected = ref([])
const running = ref(false)

const filtered = computed(() => {
  const q = filter.value.trim().toLowerCase()
  if (!q) return props.modules
  return props.modules.filter(m => m.name.toLowerCase().includes(q))
})

function onSel(rows) { selected.value = rows }

async function onRestart() {
  const names = selected.value.map(s => s.name)
  try {
    await ElMessageBox.confirm(`确认重启 ${names.length} 个模块？`, '重启确认')
  } catch (_) { return }
  running.value = true
  try {
    const r = await restartModules({ project_env_id: props.projectEnv.id, module_names: names })
    ElMessage.success(`已触发 (deployment #${r.deployment_id})`)
  } finally { running.value = false }
}

async function loadHistoryOnDemand(row) {
  if (row._history || row._loading) return
  row._loading = true
  try {
    row._history = (await getModuleTagHistory(row.id, 10)) || []
  } finally { row._loading = false }
}

async function onHistorySelect(row, tag) {
  try {
    await ElMessageBox.confirm(
      `将把 ${row.name} 回滚到 tag ${tag}，继续？`,
      '单模块回滚', { type: 'warning' }
    )
  } catch (_) { return }
  await updateImage({
    project_env_id: props.projectEnv.id,
    changes: [{ module: row.name, tag }]
  })
  ElMessage.success('已提交')
}
</script>

<style scoped>
.head { padding: 12px 16px; border-bottom: 1px solid var(--border-soft); display: flex; justify-content: space-between; align-items: center; }
.title { font-weight: 600; font-size: 14px; }
.sub { color: var(--text-3); font-size: 11.5px; font-family: var(--mono); }
.body { padding: 10px 12px; }
.foot { padding: 10px 16px; border-top: 1px solid var(--border-soft); background: #fafbfc; display: flex; justify-content: space-between; align-items: center; }
.info { font-size: 12px; color: var(--text-2); }
</style>
