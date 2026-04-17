<template>
  <div>
    <h2>用户管理</h2>
    <el-card>
      <el-button type="primary" @click="open()">新建用户</el-button>
      <el-table :data="list" size="small" border style="margin-top:10px">
        <el-table-column prop="id" label="ID" width="60" />
        <el-table-column prop="username" label="用户名" />
        <el-table-column prop="display_name" label="显示名" />
        <el-table-column label="角色" width="100">
          <template #default="{row}">
            <el-tag :type="row.role==='admin'?'danger':'info'" size="small">{{ row.role }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="来源" width="100">
          <template #default="{row}">
            <el-tag :type="row.auth_source==='portal'?'warning':''" size="small">{{ row.auth_source }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="80">
          <template #default="{row}">
            <el-tag :type="row.status?'success':'info'" size="small">{{ row.status?'启用':'禁用' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="created_at" label="创建时间" width="170" />
        <el-table-column label="操作" width="280" fixed="right">
          <template #default="{row}">
            <el-button size="small" @click="open(row)">编辑</el-button>
            <el-button size="small" @click="resetPwd(row)" v-if="row.auth_source==='local'">重置密码</el-button>
            <el-button size="small" @click="toggle(row)">{{ row.status?'禁用':'启用' }}</el-button>
            <el-button size="small" type="danger" @click="del(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <el-dialog v-model="dialog" :title="form.id?'编辑用户':'新建用户'" width="500px">
      <el-form :model="form" label-width="90px">
        <el-form-item label="用户名"><el-input v-model="form.username" :disabled="!!form.id" /></el-form-item>
        <el-form-item label="密码" v-if="!form.id"><el-input v-model="form.password" type="password" show-password /></el-form-item>
        <el-form-item label="显示名"><el-input v-model="form.display_name" /></el-form-item>
        <el-form-item label="角色">
          <el-select v-model="form.role">
            <el-option label="普通用户" value="user" />
            <el-option label="管理员" value="admin" />
          </el-select>
        </el-form-item>
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
  const r = await api.get('/users')
  list.value = r.data || []
}
function open(row) { form.value = row ? { ...row } : { role: 'user' }; dialog.value = true }
async function save() {
  if (form.value.id) {
    await api.put(`/users/${form.value.id}`, form.value)
  } else {
    if (!form.value.username || !form.value.password) {
      ElMessage.warning('用户名和密码必填')
      return
    }
    await api.post('/users', form.value)
  }
  dialog.value = false
  ElMessage.success('已保存')
  load()
}
async function resetPwd(row) {
  const { value } = await ElMessageBox.prompt(`重置 ${row.username} 的密码`, '提示', {
    inputType: 'password',
    inputValidator: v => v && v.length >= 6 || '密码至少6位'
  })
  await api.post(`/users/${row.id}/reset-password`, { password: value })
  ElMessage.success('密码已重置')
}
async function toggle(row) {
  await api.put(`/users/${row.id}/toggle`)
  load()
}
async function del(row) {
  await ElMessageBox.confirm(`删除用户 ${row.username}？`, '提示', { type: 'warning' })
  await api.delete(`/users/${row.id}`)
  load()
}
onMounted(load)
</script>
