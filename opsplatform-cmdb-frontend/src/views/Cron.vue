<template>
  <div class="page">
    <div class="page-head">
      <span class="page-title">定时任务</span>
      <el-button :icon="Refresh" @click="load">刷新</el-button>
    </div>
    <div class="muted" style="margin-bottom:14px">配置后台定时任务的频率，可单独开关、立即运行。改频率保存后即时生效（热重载 cron）。</div>

    <el-card v-for="t in tasks" :key="t.task_key" shadow="never" style="margin-bottom:12px" v-loading="!tasks.length">
      <div class="task">
        <div class="task-main">
          <div class="task-title">
            <b>{{ t.name }}</b>
            <el-switch v-model="t.enabled" :active-value="1" :inactive-value="0" @change="toggle(t)" style="margin-left:12px" />
          </div>
          <div class="muted" style="margin-top:4px">{{ desc[t.task_key] }}</div>
        </div>
        <div class="task-cfg">
          <span class="muted">频率</span>
          <el-select v-model="t.preset" style="width:170px" @change="onPreset(t)">
            <el-option v-for="p in presets" :key="p.v" :label="p.l" :value="p.v" />
          </el-select>
          <el-input v-if="t.preset==='custom'" v-model="t.schedule" placeholder="cron：分 时 日 月 周" style="width:180px" />
          <el-button type="primary" :loading="saving[t.task_key]" @click="save(t)">保存</el-button>
          <el-button :icon="VideoPlay" :loading="running[t.task_key]" @click="run(t)">立即运行</el-button>
        </div>
        <div class="task-meta muted">
          上次：{{ t.last_run_at || '—' }}<template v-if="t.last_result"> · {{ t.last_result }}</template><br>
          下次：{{ t.next_run_at || (t.enabled ? '计算中' : '已停用') }}
        </div>
      </div>
    </el-card>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Refresh, VideoPlay } from '@element-plus/icons-vue'
import { listScheduledTasks, updateScheduledTask, runScheduledTask } from '../api/cmdb'

const presets = [
  { v: '0 */3 * * *', l: '每 3 小时' },
  { v: '0 */6 * * *', l: '每 6 小时' },
  { v: '0 */12 * * *', l: '每 12 小时' },
  { v: '0 3 * * *', l: '每天一次（03:00）' },
  { v: '0 3,15 * * *', l: '每天两次（03/15）' },
  { v: '0 9 * * *', l: '每天一次（09:00）' },
  { v: 'custom', l: '自定义 cron' },
]
const desc = {
  refresh_expiry: 'WHOIS 查域名注册到期 + 连 443 查主域名证书到期，更新到库。',
  auto_renew: '扫描到期前 30 天的证书自动重签；成功/失败发 Lark。',
  remind: '按到期提醒阈值天数命中则推 Lark 通知人。',
  inspect: '逐条连 443 检测所有域名下所有解析记录的证书到期。',
}
const tasks = ref([]), saving = ref({}), running = ref({})

async function load() {
  const list = await listScheduledTasks()
  list.forEach((t) => {
    const p = presets.find((p) => p.v === t.schedule)
    t.preset = p ? p.v : 'custom'
  })
  tasks.value = list
}
function onPreset(t) { if (t.preset !== 'custom') t.schedule = t.preset }
async function save(t) {
  const schedule = t.preset === 'custom' ? t.schedule : t.preset
  saving.value = { ...saving.value, [t.task_key]: true }
  try { await updateScheduledTask(t.task_key, { schedule }); ElMessage.success('已保存，cron 已热重载'); load() }
  catch (e) { ElMessage.error(e.response?.data?.error || '保存失败') }
  finally { saving.value = { ...saving.value, [t.task_key]: false } }
}
async function toggle(t) {
  try { await updateScheduledTask(t.task_key, { enabled: t.enabled }); ElMessage.success(t.enabled ? '已启用' : '已停用'); load() }
  catch (e) { ElMessage.error('失败'); t.enabled = t.enabled ? 0 : 1 }
}
async function run(t) {
  running.value = { ...running.value, [t.task_key]: true }
  try { await runScheduledTask(t.task_key); ElMessage.success('已触发，约几秒后自动刷新结果'); setTimeout(load, 3000) }
  catch (e) { ElMessage.error('触发失败') }
  finally { running.value = { ...running.value, [t.task_key]: false } }
}
onMounted(load)
</script>

<style scoped>
.task { display: flex; align-items: center; gap: 24px; }
.task-main { flex: 1; }
.task-title { font-size: 14px; display: flex; align-items: center; }
.task-cfg { display: flex; align-items: center; gap: 8px; }
.task-meta { text-align: right; font-size: 12px; min-width: 210px; line-height: 1.7; }
</style>
