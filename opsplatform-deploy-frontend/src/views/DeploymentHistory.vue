<template>
  <div>
    <div class="kpi-row">
      <div class="kpi" v-for="k in kpis" :key="k.label">
        <div class="l">{{ k.label }}</div>
        <div class="v mono" :style="{ color: k.color }">{{ k.value }}</div>
      </div>
    </div>

    <div class="filters">
      <el-select v-model="filters.project_env_id" placeholder="项目环境" clearable size="small" style="width:200px;">
        <el-option v-for="e in envs" :key="e.id" :value="e.id" :label="e.name" />
      </el-select>
      <el-select v-model="filters.action" placeholder="操作类型" clearable size="small" style="width:140px;">
        <el-option value="update_image" label="update_image" />
        <el-option value="restart" label="restart" />
        <el-option value="rollback" label="rollback" />
      </el-select>
      <el-select v-model="filters.status" placeholder="状态" clearable size="small" style="width:120px;">
        <el-option value="success" label="success" />
        <el-option value="partial" label="partial" />
        <el-option value="failed" label="failed" />
        <el-option value="pending" label="pending" />
        <el-option value="no_change" label="no_change" />
      </el-select>
      <el-input v-model="filters.operator" placeholder="操作人..." size="small" style="width:160px;" />
      <el-button type="primary" size="small" @click="load">查询</el-button>
      <el-button size="small" @click="reset">重置</el-button>
    </div>

    <el-table :data="list" v-loading="loading" size="small" row-key="id" style="background:#fff;border:1px solid var(--border);border-radius:8px;">
      <el-table-column type="expand">
        <template #default="{ row }">
          <div class="expand">
            <div v-if="row.action !== 'restart'">
              <div class="sec-title">Tag 变更明细</div>
              <el-table :data="row.changes || []" size="small" empty-text="无">
                <el-table-column label="模块" prop="module" />
                <el-table-column label="From">
                  <template #default="{ row: r }"><span class="mono tag-old">{{ r.from_tag }}</span></template>
                </el-table-column>
                <el-table-column label="To">
                  <template #default="{ row: r }"><span class="mono tag-new">{{ r.to_tag }}</span></template>
                </el-table-column>
              </el-table>
            </div>
            <div v-else>
              <div class="sec-title">重启模块</div>
              <div>{{ (row.module_names || []).join(', ') }}</div>
            </div>

            <div class="sec-title" style="margin-top:12px;">ArgoCD 结果</div>
            <el-table :data="row.argocd_results || []" size="small" empty-text="无">
              <el-table-column label="App" prop="app" />
              <el-table-column label="Sync" prop="sync_status" width="120" />
              <el-table-column label="Health" prop="health" width="120" />
              <el-table-column label="耗时" prop="duration_sec" width="100">
                <template #default="{ row: r }">{{ r.duration_sec }}s</template>
              </el-table-column>
              <el-table-column label="消息" prop="msg" />
            </el-table>

            <div v-if="row.error_msg" style="margin-top:12px;color:var(--danger);font-family:var(--mono);font-size:11.5px;white-space:pre-wrap;">
              <b>错误：</b>{{ row.error_msg }}
            </div>

            <div style="margin-top:12px;">
              <el-button size="small" type="primary"
                v-if="row.action !== 'restart' && ['success','partial'].includes(row.status)"
                @click="onRollback(row)">
                回滚此次发布
              </el-button>
            </div>
          </div>
        </template>
      </el-table-column>
      <el-table-column label="时间" width="160">
        <template #default="{ row }"><span class="mono">{{ fmt(row.created_at) }}</span></template>
      </el-table-column>
      <el-table-column label="项目环境" width="160">
        <template #default="{ row }"><span class="mono">{{ envName(row.project_env_id) }}</span></template>
      </el-table-column>
      <el-table-column label="操作" width="140">
        <template #default="{ row }">
          <el-tag :type="actionType(row.action)" size="small" effect="plain">{{ row.action }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="模块">
        <template #default="{ row }">
          <el-tag v-for="m in (row.module_names || []).slice(0,4)" :key="m" size="small" effect="plain" style="margin-right:3px;">
            {{ m }}
          </el-tag>
          <el-tag v-if="(row.module_names || []).length > 4" size="small" type="info">+{{ row.module_names.length - 4 }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="状态" width="110">
        <template #default="{ row }">
          <el-tag :type="statusType(row.status)" size="small">{{ row.status }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="Commit" width="110">
        <template #default="{ row }">
          <a v-if="row.git_commit_url" :href="row.git_commit_url" target="_blank" class="mono" style="color:var(--primary);text-decoration:none;">
            {{ row.git_commit }}
          </a>
          <span v-else class="mono" style="color:var(--text-3);">—</span>
        </template>
      </el-table-column>
      <el-table-column label="操作人" prop="operator" width="100" />
    </el-table>

    <el-pagination
      layout="total, prev, pager, next, sizes"
      :total="total" v-model:current-page="page" v-model:page-size="pageSize" :page-sizes="[20, 50, 100]"
      @current-change="load" @size-change="load" style="margin-top: 12px;" />

    <RollbackDialog v-model="rbVisible" :deployment-id="rbID" @done="load" />
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import dayjs from 'dayjs'
import { listDeployments, listProjectEnvs } from '../api'
import RollbackDialog from '../components/RollbackDialog.vue'

const list = ref([])
const envs = ref([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const loading = ref(false)
const filters = reactive({ project_env_id: null, action: '', status: '', operator: '' })
const rbVisible = ref(false)
const rbID = ref(null)

const kpis = ref([
  { label: '当前页数', value: '0', color: '' },
  { label: '成功率', value: '—', color: 'var(--success)' },
  { label: '重启次数', value: '0', color: '' },
  { label: '平均耗时', value: '0s', color: '' },
])

function envName(id) { return envs.value.find(e => e.id === id)?.name || id }
function fmt(s) { return dayjs(s).format('YYYY-MM-DD HH:mm') }
function actionType(a) {
  return a === 'update_image' ? 'primary'
    : a === 'rollback' ? 'warning'
    : 'info'
}
function statusType(s) {
  return s === 'success' ? 'success'
    : s === 'failed' ? 'danger'
    : s === 'partial' ? 'warning'
    : 'info'
}

async function load() {
  loading.value = true
  try {
    const params = { ...filters, page: page.value, page_size: pageSize.value }
    const r = await listDeployments(params)
    list.value = r.list || []
    total.value = r.total || 0
    computeKPIs()
  } finally { loading.value = false }
}
function computeKPIs() {
  const n = list.value.length
  const ok = list.value.filter(d => d.status === 'success').length
  const restart = list.value.filter(d => d.action === 'restart').length
  const avg = n ? Math.round(list.value.reduce((a, b) => a + (b.duration_sec || 0), 0) / n) : 0
  kpis.value = [
    { label: '当前页数', value: n.toString(), color: '' },
    { label: '成功率', value: n ? Math.round(ok * 100 / n) + '%' : '—', color: 'var(--success)' },
    { label: '重启次数', value: restart.toString(), color: '' },
    { label: '平均耗时', value: avg + 's', color: '' },
  ]
}
function reset() {
  Object.assign(filters, { project_env_id: null, action: '', status: '', operator: '' })
  page.value = 1
  load()
}
function onRollback(row) { rbID.value = row.id; rbVisible.value = true }

onMounted(async () => {
  envs.value = (await listProjectEnvs()) || []
  await load()
})
</script>

<style scoped>
.kpi-row { display: grid; grid-template-columns: repeat(4, 1fr); gap: 12px; margin-bottom: 12px; }
.kpi { background: #fff; border: 1px solid var(--border); border-radius: 8px; padding: 12px 14px; }
.kpi .l { font-size: 11px; color: var(--text-3); text-transform: uppercase; letter-spacing: .5px; font-weight: 600; }
.kpi .v { font-size: 22px; font-weight: 700; margin-top: 2px; }
.filters { background: #fff; border: 1px solid var(--border); border-radius: 8px; padding: 10px 14px; margin-bottom: 12px; display: flex; gap: 10px; align-items: center; flex-wrap: wrap; }
.expand { background: #fafbfc; padding: 14px 22px; }
.sec-title { font-size: 10px; text-transform: uppercase; letter-spacing: 1px; color: var(--text-3); font-weight: 600; margin-bottom: 8px; }
.tag-old { color: var(--text-3); text-decoration: line-through; }
.tag-new { color: var(--success); font-weight: 500; }
</style>
