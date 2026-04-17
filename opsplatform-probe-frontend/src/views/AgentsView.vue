<template>
  <div>
    <h2>Agent 管理</h2>
    <el-card>
      <div style="margin-bottom:10px">
        <el-select v-model="filterStatus" clearable placeholder="状态" @change="load" style="width:140px;margin-right:8px">
          <el-option label="全部" value="" />
          <el-option label="待审批" value="pending" />
          <el-option label="在线" value="online" />
          <el-option label="离线" value="offline" />
          <el-option label="已禁用" value="disabled" />
        </el-select>
        <el-button @click="load">刷新</el-button>
      </div>
      <el-table :data="list" v-loading="loading" size="small" border>
        <el-table-column prop="agent_id" label="Agent ID" min-width="160" />
        <el-table-column prop="hostname" label="主机名" min-width="140" />
        <el-table-column prop="ip" label="IP" width="130" />
        <el-table-column prop="version" label="版本" width="100" />
        <el-table-column prop="group_name" label="分组" width="120" />
        <el-table-column label="状态" width="100">
          <template #default="{row}">
            <el-tag v-if="!row.approved" type="warning">待审批</el-tag>
            <el-tag v-else-if="row.status==='online'" type="success">在线</el-tag>
            <el-tag v-else-if="row.status==='offline'" type="info">离线</el-tag>
            <el-tag v-else-if="row.status==='disabled'" type="danger">已禁用</el-tag>
            <el-tag v-else>{{ row.status }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="last_heartbeat_at" label="最后心跳" width="170" />
        <el-table-column label="操作" width="280" fixed="right">
          <template #default="{row}">
            <el-button v-if="!row.approved" size="small" type="primary" @click="approve(row)">审批</el-button>
            <el-button v-if="row.approved" size="small" @click="reissue(row)">重签 Token</el-button>
            <el-button v-if="row.status!=='disabled'" size="small" @click="offline(row)">下线</el-button>
            <el-button size="small" type="danger" :disabled="row.status==='online'" @click="del(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import api from '../api/client'
import { ElMessage, ElMessageBox } from 'element-plus'

const list = ref([])
const loading = ref(false)
const filterStatus = ref('')

async function load() {
  loading.value = true
  try {
    const params = {}
    if (filterStatus.value) params.status = filterStatus.value
    const r = await api.get('/agents', { params })
    list.value = r.data || []
  } finally { loading.value = false }
}

async function approve(row) {
  await api.post(`/agents/${row.id}/approve`)
  ElMessage.success('已审批')
  load()
}
async function offline(row) {
  await ElMessageBox.confirm('确认下线该 Agent？', '提示', { type: 'warning' })
  await api.post(`/agents/${row.id}/offline`)
  ElMessage.success('已下线')
  load()
}
async function reissue(row) {
  await ElMessageBox.confirm(
    `确认为 ${row.agent_id} 重新签发 Token？签发后旧 Token 立即失效, 必须把新 Token 写入该 Agent 的 config.yaml 后重启 Agent。`,
    '重签 Token', { type: 'warning' }
  )
  const r = await api.post(`/agents/${row.id}/reissue-token`)
  ElMessageBox.alert(
    `新 Token (仅显示一次, 请立即复制):\n\n${r.data.token}\n\n过期时间: ${r.data.expires_at || '永不'}`,
    '重签成功', { type: 'success' }
  )
  load()
}

async function del(row) {
  if (row.status === 'online') {
    ElMessage.warning('在线 Agent 不允许删除，请先下线')
    return
  }
  await ElMessageBox.confirm(`确认删除 Agent ${row.agent_id}？`, '提示', { type: 'warning' })
  await api.delete(`/agents/${row.id}`)
  ElMessage.success('已删除')
  load()
}

onMounted(load)
</script>
