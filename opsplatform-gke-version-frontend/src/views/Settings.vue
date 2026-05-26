<template>
  <div style="max-width:600px">
    <h3>设置</h3>
    <el-form :model="form" label-width="160px">
      <el-form-item label="采集间隔（分钟）">
        <el-input-number v-model="form.scrape_interval_minutes" :min="5" :max="1440" />
        <div style="color:#999;font-size:12px;margin-top:4px;">范围 5-1440，最大延迟 30 秒生效。</div>
      </el-form-item>
      <el-form-item>
        <el-button type="primary" :loading="saving" @click="save">保存</el-button>
      </el-form-item>
    </el-form>
  </div>
</template>
<script setup>
import { ref, onMounted, reactive } from 'vue'
import { getSettings, updateSetting } from '../api/settings'
import { ElMessage } from 'element-plus'

const form = reactive({ scrape_interval_minutes: 30 })
const saving = ref(false)

onMounted(async () => {
  const s = await getSettings()
  if (s.scrape_interval_minutes) form.scrape_interval_minutes = parseInt(s.scrape_interval_minutes, 10)
})

async function save() {
  saving.value = true
  try {
    await updateSetting('scrape_interval_minutes', String(form.scrape_interval_minutes))
    ElMessage.success('已保存，最长 30 秒生效')
  } catch (e) {
    ElMessage.error('保存失败：' + (e.response?.data?.error || e.message))
  } finally {
    saving.value = false
  }
}
</script>
