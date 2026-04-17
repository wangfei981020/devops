<template>
  <div>
    <h2>Agent 分组</h2>
    <el-card>
      <el-button type="primary" @click="open()">新建分组</el-button>
      <el-table :data="list" size="small" border style="margin-top:10px">
        <el-table-column prop="id" label="ID" width="60" />
        <el-table-column prop="name" label="名称" />
        <el-table-column prop="description" label="描述" />
        <el-table-column prop="agent_count" label="Agent 数" width="100" />
        <el-table-column label="操作" width="160">
          <template #default="{row}">
            <el-button size="small" @click="open(row)">编辑</el-button>
            <el-button size="small" type="danger" @click="del(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <el-dialog v-model="dialog" :title="form.id?'编辑分组':'新建分组'" width="500px">
      <el-form :model="form" label-width="80px">
        <el-form-item label="名称"><el-input v-model="form.name" /></el-form-item>
        <el-form-item label="描述"><el-input v-model="form.description" type="textarea" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialog=false">取消</el-button>
        <el-button type="primary" @click="save">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import api from '../api/client'
import { ElMessage, ElMessageBox } from 'element-plus'

const list = ref([])
const dialog = ref(false)
const form = ref({})

async function load() {
  const r = await api.get('/agent-groups')
  list.value = r.data || []
}
function open(row) { form.value = row ? { ...row } : {}; dialog.value = true }
async function save() {
  if (form.value.id) {
    await api.put(`/agent-groups/${form.value.id}`, form.value)
  } else {
    await api.post('/agent-groups', form.value)
  }
  dialog.value = false
  ElMessage.success('已保存')
  load()
}
async function del(row) {
  await ElMessageBox.confirm(`删除分组 ${row.name}？`, '提示', { type: 'warning' })
  await api.delete(`/agent-groups/${row.id}`)
  load()
}
onMounted(load)
</script>
