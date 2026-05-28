<template>
  <div>
    <div style="margin-bottom:16px">
      <el-button type="primary" @click="openDlg()">添加 Webhook</el-button>
    </div>
    <el-table :data="list" v-loading="loading" stripe>
      <el-table-column prop="name" label="名字" min-width="160" />
      <el-table-column prop="url" label="Webhook URL" min-width="400" show-overflow-tooltip />
      <el-table-column prop="remark" label="备注" min-width="200" />
      <el-table-column label="操作" width="160" fixed="right">
        <template #default="{ row }">
          <el-button size="small" @click="openDlg(row)">编辑</el-button>
          <el-button size="small" type="danger" @click="onDelete(row)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog v-model="dlg" :title="form.id ? '编辑 Webhook' : '添加 Webhook'" width="640px">
      <el-form :model="form" label-width="100px">
        <el-form-item label="名字"><el-input v-model="form.name" placeholder="如：生产告警群" /></el-form-item>
        <el-form-item label="URL"><el-input v-model="form.url" type="textarea" :rows="2" placeholder="https://open.larksuite.com/open-apis/bot/v2/hook/..." /></el-form-item>
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
import { listWebhooks, createWebhook, updateWebhook, deleteWebhook } from '../api/lark_webhooks'

const list = ref([])
const loading = ref(false)
const dlg = ref(false)
const form = reactive({ id: null, name: '', url: '', remark: '' })
const app = useAppStore()

async function load() {
  loading.value = true
  try { list.value = await listWebhooks() } finally { loading.value = false }
}
function openDlg(row) {
  if (row) Object.assign(form, row)
  else Object.assign(form, { id: null, name: '', url: '', remark: '' })
  dlg.value = true
}
async function save() {
  try {
    const body = { name: form.name, url: form.url, remark: form.remark }
    if (form.id) await updateWebhook(form.id, body)
    else await createWebhook(body)
    dlg.value = false
    ElMessage.success('保存成功')
    await load()
  } catch (e) {
    ElMessage.error('保存失败：' + (e.response?.data?.error || e.message))
  }
}
async function onDelete(row) {
  if (!await app.showConfirm(`删除 Webhook ${row.name}？`)) return
  try {
    await deleteWebhook(row.id)
    ElMessage.success('已删除')
    await load()
  } catch (e) {
    ElMessage.error('删除失败：' + (e.response?.data?.error || e.message))
  }
}
onMounted(load)
</script>
