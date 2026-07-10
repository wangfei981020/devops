<template>
  <div class="env-page">
    <div class="page-head">
      <div>
        <h2>环境管理</h2>
        <p class="sub">配置可用环境（dev / test / uat / prod…），每个环境独立权限档（提交需对应权限）</p>
      </div>
      <el-button type="primary" @click="openEnv()">新增环境</el-button>
    </div>

    <el-table :data="envs" border stripe v-loading="loading">
      <el-table-column label="环境名" prop="name" width="140" />
      <el-table-column label="显示名" prop="display_name" width="160" />
      <el-table-column label="权限档" prop="permission_code" min-width="160">
        <template #default="{ row }"><code>{{ row.permission_code }}</code></template>
      </el-table-column>
      <el-table-column label="排序" prop="sort_order" width="90" />
      <el-table-column label="启用" width="90">
        <template #default="{ row }">
          <el-tag :type="row.enabled ? 'success' : 'info'" size="small">{{ row.enabled ? '启用' : '停用' }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="操作" width="140">
        <template #default="{ row }">
          <el-button link type="primary" @click="openEnv(row)">编辑</el-button>
          <el-button link type="danger" @click="delEnv(row)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog v-model="dialog" :title="isEdit ? '编辑环境' : '新增环境'" width="480px" :close-on-click-modal="false">
      <el-form :model="form" label-width="90px">
        <el-form-item label="环境名" required>
          <el-input v-model="form.name" :disabled="isEdit" placeholder="如 dev / test / uat / prod" />
        </el-form-item>
        <el-form-item label="显示名">
          <el-input v-model="form.display_name" placeholder="如 开发 / 测试" />
        </el-form-item>
        <el-form-item label="权限档">
          <el-input v-model="form.permission_code" placeholder="留空=自动 submit_<环境名>" />
        </el-form-item>
        <el-form-item label="排序">
          <el-input-number v-model="form.sort_order" :min="0" />
        </el-form-item>
        <el-form-item label="启用">
          <el-switch v-model="form.enabled" :active-value="1" :inactive-value="0" />
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
import { ref, computed, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import * as api from '../api'

const envs = ref([])
const loading = ref(false)
const dialog = ref(false)
const saving = ref(false)
const form = ref({ name: '', display_name: '', permission_code: '', sort_order: 0, enabled: 1 })
const isEdit = computed(() => !!form.value._edit)

function openEnv(row) {
  form.value = row
    ? { _edit: true, name: row.name, display_name: row.display_name, permission_code: row.permission_code, sort_order: row.sort_order, enabled: row.enabled }
    : { name: '', display_name: '', permission_code: '', sort_order: 0, enabled: 1 }
  dialog.value = true
}

async function save() {
  if (!form.value.name.trim()) { ElMessage.warning('环境名必填'); return }
  saving.value = true
  try {
    const body = { display_name: form.value.display_name, permission_code: form.value.permission_code, sort_order: form.value.sort_order, enabled: form.value.enabled }
    if (isEdit.value) await api.updateEnvironment(form.value.name, body)
    else await api.createEnvironment({ ...body, name: form.value.name.trim() })
    dialog.value = false
    ElMessage.success('已保存')
    await load()
  } finally {
    saving.value = false
  }
}

async function delEnv(row) {
  try { await ElMessageBox.confirm(`删除环境「${row.name}」？`, '确认', { type: 'warning' }) } catch { return }
  await api.deleteEnvironment(row.name)
  ElMessage.success('已删除')
  await load()
}

async function load() {
  loading.value = true
  try { envs.value = (await api.listEnvironments()) || [] } finally { loading.value = false }
}

onMounted(load)
</script>

<style scoped>
.env-page { padding: 16px 20px; }
.page-head { display: flex; justify-content: space-between; align-items: flex-start; margin-bottom: 16px; }
.page-head h2 { margin: 0 0 4px; font-size: 18px; }
.sub { color: #909399; font-size: 13px; margin: 0; }
</style>
