<template>
  <div>
    <h2>Agent 升级</h2>
    <el-card>
      <el-form inline>
        <el-form-item label="目标版本">
          <el-select v-model="toVersion" filterable style="width:200px">
            <el-option v-for="v in versions" :key="v.id" :label="v.version" :value="v.version" />
          </el-select>
        </el-form-item>
        <el-form-item label="选 Agent">
          <el-select v-model="agentIds" multiple filterable style="width:400px">
            <el-option v-for="a in agents" :key="a.agent_id" :label="`${a.agent_id} (${a.hostname}) - ${a.version||'未知'}`" :value="a.agent_id" />
          </el-select>
        </el-form-item>
        <el-form-item>
          <el-checkbox v-model="allowDowngrade">允许降级</el-checkbox>
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="dispatch">下发升级</el-button>
          <el-button @click="loadTasks">刷新任务</el-button>
        </el-form-item>
      </el-form>
    </el-card>
    <el-card style="margin-top:16px">
      <template #header>升级任务</template>
      <el-table :data="tasks" size="small" border>
        <el-table-column prop="id" label="ID" width="60" />
        <el-table-column prop="agent_id" label="Agent" min-width="140" />
        <el-table-column prop="from_version" label="原版本" width="100" />
        <el-table-column prop="to_version" label="目标版本" width="100" />
        <el-table-column label="状态" width="120">
          <template #default="{row}">
            <el-tag :type="statusType(row.status)">{{ row.status }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="error" label="错误" show-overflow-tooltip />
        <el-table-column prop="created_at" label="创建时间" width="170" />
        <el-table-column prop="finished_at" label="完成时间" width="170" />
      </el-table>
    </el-card>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import api from '../api/client'
import { ElMessage } from 'element-plus'

const versions = ref([])
const agents = ref([])
const tasks = ref([])
const toVersion = ref('')
const agentIds = ref([])
const allowDowngrade = ref(false)

function statusType(s) {
  return { success: 'success', failed: 'danger', pending: 'info', downloading: 'warning', upgrading: 'warning', rollback: 'warning' }[s] || ''
}

async function dispatch() {
  if (!toVersion.value || !agentIds.value.length) {
    ElMessage.warning('请选择版本和 Agent')
    return
  }
  const r = await api.post('/upgrades', {
    to_version: toVersion.value,
    agent_ids: agentIds.value,
    allow_downgrade: allowDowngrade.value
  })
  ElMessage.success(`已创建 ${r.data.created} 个升级任务，跳过 ${r.data.skipped.length}`)
  loadTasks()
}
async function loadTasks() {
  const r = await api.get('/upgrades', { params: { limit: 100 } })
  tasks.value = r.data || []
}
onMounted(async () => {
  const v = await api.get('/versions'); versions.value = v.data || []
  const a = await api.get('/agents'); agents.value = (a.data || []).filter(x=>x.approved)
  loadTasks()
  setInterval(loadTasks, 5000)
})
</script>
