<template>
  <div>
    <h2>手动探测</h2>
    <el-card>
      <el-form label-width="80px">
        <el-form-item label="选 Agent">
          <el-select v-model="agentIds" multiple filterable style="width:100%">
            <el-option v-for="a in agents" :key="a.agent_id" :label="`${a.agent_id} (${a.hostname}) - ${a.status}`" :value="a.agent_id" />
          </el-select>
        </el-form-item>
        <el-form-item label="选目标">
          <el-select v-model="targetIds" multiple filterable style="width:100%">
            <el-option v-for="t in targets" :key="t.id" :label="`${t.name} [${t.type}] ${t.target}`" :value="t.id" />
          </el-select>
        </el-form-item>
        <el-form-item>
          <el-button type="primary" :loading="running" @click="start">开始探测</el-button>
          <el-button @click="exportCsv" :disabled="!results.length">导出 CSV</el-button>
        </el-form-item>
      </el-form>
    </el-card>

    <el-card v-if="batchID" style="margin-top:16px">
      <template #header>
        <span>结果矩阵</span>
        <el-tag style="margin-left:10px">{{ done }}/{{ total }} {{ finished?'已完成':'进行中…' }}</el-tag>
      </template>
      <el-table :data="matrix" border size="small">
        <el-table-column prop="agent_id" label="Agent" width="200" fixed />
        <el-table-column v-for="t in targetCols" :key="t.id" :label="t.name + ' ['+t.type+']'" min-width="140">
          <template #default="{row}">
            <div v-if="row.cells[t.id]">
              <el-tag v-if="row.cells[t.id].success" type="success" size="small">✓ {{ row.cells[t.id].latency_ms }}ms</el-tag>
              <el-tooltip v-else :content="row.cells[t.id].error || ('HTTP ' + row.cells[t.id].status_code)">
                <el-tag type="danger" size="small">✗</el-tag>
              </el-tooltip>
            </div>
            <span v-else style="color:#bbb">—</span>
          </template>
        </el-table-column>
      </el-table>
    </el-card>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'
import api from '../api/client'
import { ElMessage } from 'element-plus'

const agents = ref([])
const targets = ref([])
const agentIds = ref([])
const targetIds = ref([])
const batchID = ref('')
const total = ref(0)
const done = ref(0)
const finished = ref(false)
const results = ref([])
const running = ref(false)
let timer = null

const targetCols = computed(() => targets.value.filter(t => targetIds.value.includes(t.id)))

const matrix = computed(() => {
  return agentIds.value.map(aid => {
    const cells = {}
    results.value.filter(r => r.agent_id === aid).forEach(r => { cells[r.target_id] = r })
    return { agent_id: aid, cells }
  })
})

async function start() {
  if (!agentIds.value.length || !targetIds.value.length) {
    ElMessage.warning('请选择 Agent 和目标')
    return
  }
  running.value = true
  results.value = []
  finished.value = false
  try {
    const r = await api.post('/probe', { agent_ids: agentIds.value, target_ids: targetIds.value })
    batchID.value = r.data.batch_id
    total.value = r.data.total
    pollResults()
  } finally { running.value = false }
}

async function pollResults() {
  if (timer) clearInterval(timer)
  timer = setInterval(async () => {
    const r = await api.get('/probe/result', { params: { batch_id: batchID.value } })
    results.value = r.data.results || []
    done.value = r.data.done
    finished.value = r.data.finished
    if (finished.value) clearInterval(timer)
  }, 1500)
}

function exportCsv() {
  const lines = ['agent_id,target_id,target_name,target_addr,success,latency_ms,status_code,error']
  for (const r of results.value) {
    lines.push([r.agent_id, r.target_id, r.target_name, r.target_addr, r.success, r.latency_ms, r.status_code, '"'+(r.error||'').replace(/"/g,'""')+'"'].join(','))
  }
  const blob = new Blob([lines.join('\n')], { type: 'text/csv' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url; a.download = `probe-${batchID.value}.csv`; a.click()
  URL.revokeObjectURL(url)
}

onMounted(async () => {
  const a = await api.get('/agents'); agents.value = (a.data||[]).filter(x=>x.approved && x.status!=='disabled')
  const t = await api.get('/targets'); targets.value = (t.data||[]).filter(x=>x.enabled)
})
onUnmounted(() => { if (timer) clearInterval(timer) })
</script>
