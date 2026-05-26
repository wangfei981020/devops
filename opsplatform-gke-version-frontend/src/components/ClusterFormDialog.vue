<template>
  <el-dialog v-model="visible" :title="form.id ? '编辑集群' : '添加集群'" width="500px">
    <el-form :model="form" label-width="80px">
      <el-form-item label="项目 ID"><el-input v-model="form.project_id" /></el-form-item>
      <el-form-item label="区域"><el-input v-model="form.location" placeholder="asia-east2" /></el-form-item>
      <el-form-item label="集群名"><el-input v-model="form.name" /></el-form-item>
      <el-form-item label="启用"><el-switch v-model="form.enabled_bool" /></el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="visible = false">取消</el-button>
      <el-button type="primary" :loading="saving" @click="save">保存</el-button>
    </template>
  </el-dialog>
</template>
<script setup>
import { ref, reactive } from 'vue'
import { ElMessage } from 'element-plus'
import { createCluster, updateCluster } from '../api/clusters'

const visible = ref(false)
const saving = ref(false)
const form = reactive({
  id: null,
  project_id: '',
  location: 'asia-east2',
  name: '',
  enabled: 1,
  enabled_bool: true,
})
const emit = defineEmits(['saved'])

function open(initial) {
  if (initial) {
    Object.assign(form, initial, { enabled_bool: initial.enabled === 1 })
  } else {
    Object.assign(form, {
      id: null,
      project_id: '',
      location: 'asia-east2',
      name: '',
      enabled: 1,
      enabled_bool: true,
    })
  }
  visible.value = true
}
async function save() {
  saving.value = true
  try {
    const body = {
      project_id: form.project_id,
      location: form.location,
      name: form.name,
      enabled: form.enabled_bool ? 1 : 0,
    }
    if (form.id) await updateCluster(form.id, body)
    else await createCluster(body)
    ElMessage.success('保存成功')
    visible.value = false
    emit('saved')
  } catch (e) {
    ElMessage.error('保存失败：' + (e.response?.data?.error || e.message))
  } finally {
    saving.value = false
  }
}
defineExpose({ open })
</script>
