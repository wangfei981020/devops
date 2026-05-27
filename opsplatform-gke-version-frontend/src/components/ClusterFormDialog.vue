<template>
  <el-dialog v-model="visible" :title="form.id ? '编辑集群' : '添加集群'" width="640px">
    <el-form :model="form" label-width="120px">
      <el-form-item label="项目 ID"><el-input v-model="form.project_id" /></el-form-item>
      <el-form-item label="区域"><el-input v-model="form.location" placeholder="asia-east2" /></el-form-item>
      <el-form-item label="集群名"><el-input v-model="form.name" /></el-form-item>
      <el-form-item label="启用"><el-switch v-model="form.enabled_bool" /></el-form-item>
      <el-form-item label="GCP SA Key">
        <el-input
          v-model="form.sa_key_json"
          type="textarea"
          :rows="8"
          :placeholder="form.id && form.has_sa_key ? '已配置 SA Key；如需替换请粘贴新 JSON，留空保留原值' : '粘贴 service account JSON 内容（{type:service_account, ...}）'"
        />
        <div v-if="form.id && form.has_sa_key" style="color:#67c23a;font-size:12px;margin-top:4px;">
          ✓ 当前已配置 SA Key（不显示原值，安全考虑）
        </div>
        <div v-else-if="form.id && !form.has_sa_key" style="color:#e6a23c;font-size:12px;margin-top:4px;">
          ⚠ 当前未配置 SA Key，无法抓取版本数据
        </div>
      </el-form-item>
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
  sa_key_json: '',
  has_sa_key: false,
  enabled: 1,
  enabled_bool: true,
})
const emit = defineEmits(['saved'])

function open(initial) {
  if (initial) {
    Object.assign(form, {
      id: initial.id,
      project_id: initial.project_id,
      location: initial.location,
      name: initial.name,
      sa_key_json: '',
      has_sa_key: !!initial.has_sa_key,
      enabled: initial.enabled,
      enabled_bool: initial.enabled === 1,
    })
  } else {
    Object.assign(form, {
      id: null,
      project_id: '',
      location: 'asia-east2',
      name: '',
      sa_key_json: '',
      has_sa_key: false,
      enabled: 1,
      enabled_bool: true,
    })
  }
  visible.value = true
}
async function save() {
  if (form.sa_key_json) {
    try {
      const parsed = JSON.parse(form.sa_key_json)
      if (parsed.type !== 'service_account') {
        ElMessage.warning('SA Key JSON 的 type 字段不是 service_account，请确认无误')
      }
    } catch {
      ElMessage.error('SA Key 不是合法 JSON')
      return
    }
  }
  saving.value = true
  try {
    const body = {
      project_id: form.project_id,
      location: form.location,
      name: form.name,
      sa_key_json: form.sa_key_json,
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
