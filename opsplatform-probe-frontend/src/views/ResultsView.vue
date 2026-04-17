<template>
  <div>
    <h2>探测结果</h2>
    <el-card>
      <el-form inline>
        <el-form-item label="时间范围">
          <el-date-picker
            v-model="timeRange"
            type="datetimerange"
            range-separator="至"
            start-placeholder="开始时间"
            end-placeholder="结束时间"
            value-format="YYYY-MM-DD HH:mm:ss"
            :shortcuts="timeShortcuts"
          />
        </el-form-item>
        <el-form-item label="Agent"><el-input v-model="filters.agent_id" clearable /></el-form-item>
        <el-form-item label="目标 ID"><el-input v-model="filters.target_id" clearable style="width:120px" /></el-form-item>
        <el-form-item label="执行人"><el-input v-model="filters.created_by" clearable placeholder="发起人用户名" /></el-form-item>
        <el-form-item label="来源">
          <el-select v-model="filters.source" clearable style="width:130px">
            <el-option label="手动探测" value="manual" />
            <el-option label="定时探测" value="scheduled" />
          </el-select>
        </el-form-item>
        <el-form-item label="状态">
          <el-select v-model="filters.success" clearable style="width:120px">
            <el-option label="成功" value="1" />
            <el-option label="失败" value="0" />
          </el-select>
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="onSearch">查询</el-button>
          <el-button @click="resetFilters">重置</el-button>
          <el-button @click="cleanOld">清理旧数据</el-button>
        </el-form-item>
      </el-form>
      <el-table :data="list" size="small" border>
        <el-table-column prop="probed_at" label="探测时间" width="170" />
        <el-table-column label="发起人 / 来源" width="140">
          <template #default="{row}">
            <div>{{ row.created_by || '-' }}</div>
            <el-tag size="small" :type="row.source==='scheduled'?'warning':'info'">
              {{ row.source==='scheduled' ? '定时' : (row.source==='manual'?'手动':'-') }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="batch_created_at" label="批次创建时间" width="170" />
        <el-table-column prop="agent_id" label="Agent" min-width="140" show-overflow-tooltip />
        <el-table-column prop="target_name" label="目标" min-width="140" show-overflow-tooltip />
        <el-table-column prop="target_type" label="类型" width="80" />
        <el-table-column prop="target_addr" label="地址" min-width="180" show-overflow-tooltip />
        <el-table-column label="结果" width="80">
          <template #default="{row}"><el-tag :type="row.success?'success':'danger'" size="small">{{ row.success?'✓':'✗' }}</el-tag></template>
        </el-table-column>
        <el-table-column prop="latency_ms" label="延迟(ms)" width="100" />
        <el-table-column prop="status_code" label="状态码" width="90" />
        <el-table-column prop="error" label="错误" min-width="200" show-overflow-tooltip />
      </el-table>
      <el-pagination
        v-model:current-page="page"
        :page-size="pageSize"
        :total="total"
        layout="total, prev, pager, next, jumper"
        @current-change="load"
        style="margin-top:10px"
      />
    </el-card>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import api from '../api/client'
import { ElMessage, ElMessageBox } from 'element-plus'

const list = ref([])
const filters = ref({})
const timeRange = ref([])
const page = ref(1)
const pageSize = ref(50)
const total = ref(0)

const timeShortcuts = [
  { text: '最近 1 小时', value: () => { const e = new Date(); const s = new Date(); s.setHours(s.getHours()-1); return [s,e] } },
  { text: '最近 24 小时', value: () => { const e = new Date(); const s = new Date(); s.setDate(s.getDate()-1); return [s,e] } },
  { text: '最近 7 天', value: () => { const e = new Date(); const s = new Date(); s.setDate(s.getDate()-7); return [s,e] } },
  { text: '最近 30 天', value: () => { const e = new Date(); const s = new Date(); s.setDate(s.getDate()-30); return [s,e] } }
]

async function load() {
  const params = { page: page.value, page_size: pageSize.value, ...filters.value }
  if (timeRange.value && timeRange.value.length === 2) {
    params.start_time = timeRange.value[0]
    params.end_time = timeRange.value[1]
  }
  const r = await api.get('/probe/results', { params })
  list.value = r.data.list || []
  total.value = r.data.total || 0
}

function onSearch() { page.value = 1; load() }

function resetFilters() {
  filters.value = {}
  timeRange.value = []
  page.value = 1
  load()
}

async function cleanOld() {
  const { value } = await ElMessageBox.prompt('保留最近多少天？', '清理旧数据', { inputValue: '30' })
  await api.post('/probe/results/clean', { before_days: parseInt(value) })
  ElMessage.success('已清理')
  load()
}

onMounted(load)
</script>
