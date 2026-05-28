<template>
  <div>
    <div style="margin-bottom:16px">
      <el-button type="primary" @click="openDlg()">添加通知人</el-button>
    </div>
    <el-table :data="list" v-loading="loading" stripe>
      <el-table-column prop="name" label="名字" min-width="120" />
      <el-table-column prop="lark_id" label="Lark ID" min-width="280" />
      <el-table-column prop="remark" label="备注" min-width="200" />
      <el-table-column label="操作" width="160" fixed="right">
        <template #default="{ row }">
          <el-button size="small" @click="openDlg(row)">编辑</el-button>
          <el-button size="small" type="danger" @click="onDelete(row)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog v-model="dlg" :title="form.id ? '编辑通知人' : '添加通知人'" width="500px">
      <el-form :model="form" label-width="100px">
        <el-form-item label="名字"><el-input v-model="form.name" /></el-form-item>
        <el-form-item label="Lark ID"><el-input v-model="form.lark_id" placeholder="ou_xxx 或 on_xxx 形式的 open_id" /></el-form-item>
        <el-form-item label="备注"><el-input v-model="form.remark" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dlg = false">取消</el-button>
        <el-button type="primary" @click="save">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>
<script setup>
import { ref, reactive, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { useAppStore } from '../stores/app'
import { listNotifyUsers, createNotifyUser, updateNotifyUser, deleteNotifyUser } from '../api/notify_users'

const list = ref([])
const loading = ref(false)
const dlg = ref(false)
const form = reactive({ id: null, name: '', lark_id: '', remark: '' })
const app = useAppStore()

async function load() {
  loading.value = true
  try { list.value = await listNotifyUsers() } finally { loading.value = false }
}
function openDlg(row) {
  if (row) Object.assign(form, row)
  else Object.assign(form, { id: null, name: '', lark_id: '', remark: '' })
  dlg.value = true
}
async function save() {
  try {
    const body = { name: form.name, lark_id: form.lark_id, remark: form.remark }
    if (form.id) await updateNotifyUser(form.id, body)
    else await createNotifyUser(body)
    dlg.value = false
    ElMessage.success('保存成功')
    await load()
  } catch (e) {
    ElMessage.error('保存失败：' + (e.response?.data?.error || e.message))
  }
}
async function onDelete(row) {
  if (!await app.showConfirm(`删除通知人 ${row.name}？`)) return
  try {
    await deleteNotifyUser(row.id)
    ElMessage.success('已删除')
    await load()
  } catch (e) {
    ElMessage.error('删除失败：' + (e.response?.data?.error || e.message))
  }
}
onMounted(load)
</script>
