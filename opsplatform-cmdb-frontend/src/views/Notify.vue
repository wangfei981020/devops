<template>
  <div class="page">
    <div class="page-head"><span class="page-title">通知</span></div>

    <!-- 机器人配置 -->
    <el-card shadow="never" style="margin-bottom:14px">
      <template #header><b>飞书 / Lark 机器人</b></template>
      <el-form label-width="110px" style="max-width:760px">
        <el-form-item label="Webhook">
          <el-input v-model="cfg.feishu_webhook" placeholder="飞书自定义机器人 webhook 地址" />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="saveCfg">保存</el-button>
          <el-button :icon="Promotion" :loading="testing" @click="test">发送测试</el-button>
          <span class="muted" style="margin-left:10px">测试会用当前 webhook + 通知人 @ 发一条到群里</span>
        </el-form-item>
      </el-form>
    </el-card>

    <!-- 通知人 -->
    <el-card shadow="never" style="margin-bottom:14px">
      <template #header><b>通知人</b><span class="muted" style="margin-left:8px">告警时 @ 这些人（飞书 open_id，ou_ 开头）</span></template>
      <el-form inline @submit.prevent="addUser" style="margin-bottom:12px">
        <el-form-item label="姓名"><el-input v-model="nu.name" style="width:140px" /></el-form-item>
        <el-form-item label="open_id"><el-input v-model="nu.open_id" placeholder="ou_xxxxxxxx" style="width:260px" /></el-form-item>
        <el-button type="primary" :icon="Plus" @click="addUser">添加</el-button>
      </el-form>
      <el-table :data="users" size="small">
        <el-table-column prop="name" label="姓名" width="160" />
        <el-table-column prop="open_id" label="open_id" min-width="260" />
        <el-table-column label="操作" width="80" fixed="right"><template #default="{ row }">
          <el-tooltip content="删除"><el-button link type="danger" :icon="Delete" @click="delUser(row)" /></el-tooltip>
        </template></el-table-column>
      </el-table>
      <el-empty v-if="!users.length" description="还没有通知人，告警将不 @ 人" :image-size="50" />
    </el-card>

    <!-- 通知规则 -->
    <el-card shadow="never">
      <template #header><b>通知规则</b></template>
      <el-form label-width="120px" style="max-width:620px">
        <el-form-item label="到期提醒天数">
          <el-input v-model="cfg.remind_days" placeholder="30,15,7,1" style="width:240px" />
          <span class="muted" style="margin-left:8px">逗号分隔，剩余天数命中即提醒</span>
        </el-form-item>
        <el-form-item label="证书即将到期"><el-switch v-model="ev.notify_cert_expiring" active-value="1" inactive-value="0" /></el-form-item>
        <el-form-item label="续期成功通知"><el-switch v-model="ev.notify_renew_success" active-value="1" inactive-value="0" /></el-form-item>
        <el-form-item label="续期失败告警"><el-switch v-model="ev.notify_renew_fail" active-value="1" inactive-value="0" /></el-form-item>
        <el-form-item label="域名即将到期"><el-switch v-model="ev.notify_domain_expiring" active-value="1" inactive-value="0" /></el-form-item>
        <el-button type="primary" @click="saveCfg">保存规则</el-button>
      </el-form>
    </el-card>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Plus, Delete, Promotion } from '@element-plus/icons-vue'
import { getSettings, updateSettings, listNotifyUsers, createNotifyUser, deleteNotifyUser, testNotify } from '../api/cmdb'
import { useAppStore } from '../stores/app'

const app = useAppStore()
const cfg = ref({}), users = ref([]), testing = ref(false)
const nu = ref({ name: '', open_id: '' })
// 事件开关，settings 没存按默认（续期成功默认关，其余开）
const ev = ref({ notify_cert_expiring: '1', notify_renew_success: '0', notify_renew_fail: '1', notify_domain_expiring: '1' })

async function load() {
  cfg.value = await getSettings()
  for (const k in ev.value) if (cfg.value[k] !== undefined && cfg.value[k] !== '') ev.value[k] = cfg.value[k]
  users.value = await listNotifyUsers()
}
async function saveCfg() {
  try {
    await updateSettings({ feishu_webhook: cfg.value.feishu_webhook || '', remind_days: cfg.value.remind_days || '', ...ev.value })
    ElMessage.success('已保存')
  } catch (e) { ElMessage.error('保存失败') }
}
async function addUser() {
  if (!nu.value.name || !nu.value.open_id) { ElMessage.warning('姓名和 open_id 必填'); return }
  try { await createNotifyUser(nu.value); nu.value = { name: '', open_id: '' }; users.value = await listNotifyUsers(); ElMessage.success('已添加') }
  catch (e) { ElMessage.error(e.response?.data?.error || '失败') }
}
async function delUser(row) {
  try { await app.showConfirm(`删除通知人 ${row.name}？`); await deleteNotifyUser(row.id); users.value = await listNotifyUsers() }
  catch (e) { if (e !== 'cancel') ElMessage.error('失败') }
}
async function test() {
  testing.value = true
  try { const r = await testNotify(); ElMessage.success(r.msg || '已发送') }
  catch (e) { ElMessage.error(e.response?.data?.error || '发送失败') }
  finally { testing.value = false }
}
onMounted(load)
</script>
