<template>
  <div class="tpl-page">
    <div class="page-head">
      <div>
        <h2>模板库</h2>
        <p class="sub">把现有服务登记为「参照模板」，新增模块时从它拷骨架（体现 项目 · 模块名 · 前/后端）</p>
      </div>
      <el-button type="primary" @click="openTpl()">登记模板</el-button>
    </div>

    <el-table :data="templates" border stripe v-loading="loading">
      <el-table-column label="项目" width="110">
        <template #default="{ row }">{{ row.project || '全局' }}</template>
      </el-table-column>
      <el-table-column label="模块名（样板服务）" prop="src_service" min-width="240" />
      <el-table-column label="类型" width="90">
        <template #default="{ row }">
          <el-tag :type="row.module_type === 'frontend' ? 'warning' : (row.module_type === 'zkv' ? 'info' : 'success')" size="small">
            {{ row.module_type === 'frontend' ? '前端' : (row.module_type === 'zkv' ? 'z-kv' : '后端') }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column label="来源环境" prop="src_env" width="130" />
      <el-table-column label="名称" prop="name" min-width="140" />
      <el-table-column label="描述" prop="description" min-width="160" show-overflow-tooltip />
      <el-table-column label="操作" width="140">
        <template #default="{ row }">
          <el-button link type="primary" @click="openTpl(row)">编辑</el-button>
          <el-button link type="danger" @click="delTpl(row)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog v-model="dialog" :title="form.id ? '编辑模板' : '登记模板'" width="560px" :close-on-click-modal="false">
      <el-form :model="form" label-width="100px">
        <el-form-item label="模板名" required>
          <el-input v-model="form.name" placeholder="唯一名称（字母/数字/-/_），如 g32-backend-std" />
        </el-form-item>
        <el-form-item label="绑定项目">
          <el-input v-model="form.project" placeholder="留空=全局可用，如 g32" />
        </el-form-item>
        <el-form-item label="类型" required>
          <el-radio-group v-model="form.module_type">
            <el-radio label="backend">后端</el-radio>
            <el-radio label="frontend">前端</el-radio>
            <el-radio label="zkv">z-kv-secrets</el-radio>
          </el-radio-group>
          <div v-if="form.module_type === 'zkv'" class="form-hint">整份密钥 chart，供新项目「初始化 z-kv-secrets」时复制</div>
        </el-form-item>
        <el-form-item label="样板环境" required>
          <el-select v-model="form.src_env" placeholder="样板服务所在项目环境" filterable style="width: 100%">
            <el-option v-for="e in envs" :key="e.id" :label="`${e.name}（${e.env_type}）`" :value="e.name" />
          </el-select>
        </el-form-item>
        <el-form-item label="样板服务" required>
          <el-input v-if="form.module_type !== 'zkv'" v-model="form.src_service" placeholder="chart_base_path 下的服务目录名" />
          <el-input v-else model-value="z-kv-secrets" disabled placeholder="z-kv-secrets（固定）" />
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="form.description" type="textarea" :rows="2" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialog = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="save">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import * as api from '../api'

const templates = ref([])
const envs = ref([])
const loading = ref(false)
const dialog = ref(false)
const saving = ref(false)
const form = ref({ id: null, name: '', project: '', module_type: 'backend', src_env: '', src_service: '', description: '' })

function openTpl(row) {
  form.value = row
    ? { id: row.id, name: row.name, project: row.project, module_type: row.module_type, src_env: row.src_env, src_service: row.src_service, description: row.description }
    : { id: null, name: '', project: '', module_type: 'backend', src_env: '', src_service: '', description: '' }
  dialog.value = true
}

async function save() {
  const f = form.value
  if (f.module_type === 'zkv') f.src_service = 'z-kv-secrets' // zkv 类型源目录固定
  if (!f.name.trim() || !f.src_env || !f.src_service.trim()) {
    ElMessage.warning('模板名 / 样板环境 / 样板服务 必填')
    return
  }
  saving.value = true
  try {
    const body = { name: f.name.trim(), project: f.project.trim(), module_type: f.module_type, src_env: f.src_env, src_service: f.src_service.trim(), description: f.description }
    if (f.id) await api.updateTemplate(f.id, body)
    else await api.createTemplate(body)
    dialog.value = false
    ElMessage.success('已保存')
    await load()
  } finally {
    saving.value = false
  }
}

async function delTpl(row) {
  try { await ElMessageBox.confirm(`删除模板「${row.name}」？`, '确认', { type: 'warning' }) } catch { return }
  await api.deleteTemplate(row.id)
  ElMessage.success('已删除')
  await load()
}

async function load() {
  loading.value = true
  try { templates.value = (await api.listTemplates()) || [] } finally { loading.value = false }
}

onMounted(async () => {
  envs.value = (await api.listProjectEnvs()) || []
  await load()
})
</script>

<style scoped>
.tpl-page { padding: 16px 20px; }
.page-head { display: flex; justify-content: space-between; align-items: flex-start; margin-bottom: 16px; }
.page-head h2 { margin: 0 0 4px; font-size: 18px; }
.sub { color: #909399; font-size: 13px; margin: 0; }
</style>
