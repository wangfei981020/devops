<template>
  <div>
    <h2>探测目标</h2>
    <el-card>
      <el-button type="primary" @click="open()">新建目标</el-button>
      <el-button @click="batchDialog=true">批量导入</el-button>
      <el-table :data="list" size="small" border style="margin-top:10px">
        <el-table-column prop="id" label="ID" width="60" />
        <el-table-column prop="name" label="名称" min-width="140" />
        <el-table-column prop="type" label="类型" width="80" />
        <el-table-column prop="target" label="地址" min-width="200" />
        <el-table-column prop="port" label="端口" width="80" />
        <el-table-column prop="group_name" label="分组" width="100" />
        <el-table-column prop="agent_scope" label="范围" width="100" />
        <el-table-column label="状态" width="80">
          <template #default="{row}"><el-tag :type="row.enabled?'success':'info'">{{ row.enabled?'启用':'禁用' }}</el-tag></template>
        </el-table-column>
        <el-table-column label="操作" width="160" fixed="right">
          <template #default="{row}">
            <el-button size="small" @click="open(row)">编辑</el-button>
            <el-button size="small" type="danger" @click="del(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <el-dialog v-model="batchDialog" title="批量导入目标" width="640px">
      <el-form :model="batch" label-width="100px">
        <el-form-item label="类型">
          <el-select v-model="batch.type">
            <el-option label="自动识别" value="auto" />
            <el-option label="HTTP/HTTPS" value="http" />
            <el-option label="TCP 端口" value="tcp" />
            <el-option label="DNS 解析" value="dns" />
          </el-select>
          <div style="color:#999;font-size:12px;margin-top:4px">
            自动识别规则：含 http(s):// → HTTP；带 :端口 → TCP；其他 → DNS
          </div>
        </el-form-item>
        <el-form-item label="分组"><el-input v-model="batch.group_name" placeholder="可选, 应用于所有导入的目标" /></el-form-item>
        <el-form-item label="超时(秒)"><el-input-number v-model="batch.timeout_sec" :min="1" :max="60" /></el-form-item>
        <el-form-item label="可用范围">
          <el-radio-group v-model="batch.agent_scope">
            <el-radio label="all">全部 Agent</el-radio>
            <el-radio label="group">指定分组</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item v-if="batch.agent_scope==='group'" label="选择分组">
          <el-select v-model="batch.scope_group_id">
            <el-option v-for="g in groups" :key="g.id" :label="g.name" :value="g.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="目标列表">
          <el-input
            v-model="batch.lines"
            type="textarea"
            :rows="12"
            placeholder="每行一个目标，支持三种写法混用：&#10;&#10;# 带类型前缀(推荐, 最明确):&#10;http  https://www.baidu.com&#10;tcp   redis.internal:6379&#10;tcp   192.168.1.10:3306, 生产MySQL&#10;dns   example.com&#10;&#10;# 自动识别:&#10;https://api.example.com/health, API&#10;&#10;# # 开头的行会被跳过(注释)" />
          <div style="color:#999;font-size:12px;margin-top:4px">
            带前缀 <code>http / tcp / dns</code>(后跟空格) 会强制类型, 否则用上面"类型"下拉框的规则
          </div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="batchDialog=false">取消</el-button>
        <el-button type="primary" :loading="batchLoading" @click="doBatch">导入</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="dialog" :title="form.id?'编辑目标':'新建目标'" width="640px">
      <el-form :model="form" label-width="100px">
        <el-form-item label="名称"><el-input v-model="form.name" /></el-form-item>
        <el-form-item label="类型">
          <el-select v-model="form.type">
            <el-option label="HTTP/HTTPS" value="http" />
            <el-option label="TCP 端口" value="tcp" />
            <el-option label="DNS 解析" value="dns" />
          </el-select>
        </el-form-item>
        <el-form-item label="目标">
          <el-input v-model="form.target" :placeholder="placeholderHint" />
        </el-form-item>
        <el-form-item label="端口" v-if="form.type==='tcp'">
          <el-input-number v-model="form.port" :min="1" :max="65535" />
        </el-form-item>
        <el-form-item label="HTTP 方法" v-if="form.type==='http'">
          <el-select v-model="form.method">
            <el-option label="GET" value="GET" />
            <el-option label="HEAD" value="HEAD" />
            <el-option label="POST" value="POST" />
          </el-select>
        </el-form-item>
        <el-form-item label="超时(秒)"><el-input-number v-model="form.timeout_sec" :min="1" :max="60" /></el-form-item>
        <el-form-item label="分组"><el-input v-model="form.group_name" /></el-form-item>
        <el-form-item label="可用范围">
          <el-radio-group v-model="form.agent_scope">
            <el-radio label="all">全部 Agent</el-radio>
            <el-radio label="group">指定分组</el-radio>
            <el-radio label="specific">指定 Agent</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item v-if="form.agent_scope==='group'" label="选择分组">
          <el-select v-model="form.scope_group_id">
            <el-option v-for="g in groups" :key="g.id" :label="g.name" :value="g.id" />
          </el-select>
        </el-form-item>
        <el-form-item v-if="form.agent_scope==='specific'" label="选择 Agent">
          <el-select v-model="form.agent_ids" multiple filterable style="width:100%">
            <el-option v-for="a in agents" :key="a.agent_id" :label="`${a.agent_id} (${a.hostname})`" :value="a.agent_id" />
          </el-select>
        </el-form-item>
        <el-form-item label="描述"><el-input v-model="form.description" type="textarea" /></el-form-item>
        <el-form-item label="启用"><el-switch v-model="form.enabled" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialog=false">取消</el-button>
        <el-button type="primary" @click="save">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import api from '../api/client'
import { ElMessage, ElMessageBox } from 'element-plus'

const list = ref([])
const groups = ref([])
const agents = ref([])
const dialog = ref(false)
const form = ref({})
const batchDialog = ref(false)
const batchLoading = ref(false)
const batch = ref({ type: 'auto', timeout_sec: 5, agent_scope: 'all', lines: '' })

async function doBatch() {
  if (!batch.value.lines.trim()) { ElMessage.warning('请输入目标列表'); return }
  batchLoading.value = true
  try {
    const r = await api.post('/targets/batch-import', batch.value)
    ElMessage.success(`成功导入 ${r.data.created} 个，跳过 ${r.data.skipped.length} 个`)
    if (r.data.skipped.length) {
      ElMessageBox.alert(r.data.skipped.join('\n'), '跳过的行', { type: 'warning' })
    }
    batchDialog.value = false
    batch.value = { type: 'auto', timeout_sec: 5, agent_scope: 'all', lines: '' }
    load()
  } finally { batchLoading.value = false }
}

const placeholderHint = computed(() => {
  if (form.value.type === 'http') return 'https://www.example.com/api/health'
  if (form.value.type === 'tcp')  return 'redis.internal'
  if (form.value.type === 'dns')  return 'example.com'
  return ''
})

async function load() {
  const r = await api.get('/targets')
  list.value = r.data || []
  const g = await api.get('/agent-groups'); groups.value = g.data || []
  const a = await api.get('/agents'); agents.value = a.data || []
}
async function open(row) {
  if (row) {
    form.value = { ...row, agent_ids: [] }
    if (row.agent_scope === 'specific') {
      const r = await api.get(`/targets/${row.id}/agents`)
      form.value.agent_ids = r.data || []
    }
  } else {
    form.value = { type: 'http', method: 'GET', timeout_sec: 5, agent_scope: 'all', enabled: true, agent_ids: [] }
  }
  dialog.value = true
}
async function save() {
  const payload = { ...form.value }
  if (form.value.id) {
    await api.put(`/targets/${form.value.id}`, payload)
  } else {
    await api.post('/targets', payload)
  }
  dialog.value = false
  ElMessage.success('已保存')
  load()
}
async function del(row) {
  await ElMessageBox.confirm(`删除目标 ${row.name}？`, '提示', { type: 'warning' })
  await api.delete(`/targets/${row.id}`)
  load()
}
onMounted(load)
</script>
