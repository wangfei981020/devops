<template>
  <div class="page">
    <div class="page-head">
      <span class="page-title">定时任务</span>
      <el-button :icon="Refresh" @click="load">刷新</el-button>
    </div>
    <div class="muted" style="margin-bottom:14px">每个任务可开关、改频率、立即运行；跑完按配置发 Lark 群通知（成败 + @人）。Lark 群在「通知」页维护。</div>

    <el-card shadow="never">
      <el-table :data="tPaged" size="small" v-loading="loading">
        <el-table-column label="任务名" min-width="200"><template #default="{ row }">
          <b>{{ row.name }}</b>
          <div class="muted" style="font-size:12px">{{ desc[row.task_key] || '' }}</div>
        </template></el-table-column>
        <el-table-column label="频率" width="130"><template #default="{ row }"><span class="mono">{{ row.schedule }}</span></template></el-table-column>
        <el-table-column label="启用" width="70"><template #default="{ row }">
          <el-switch v-model="row.enabled" :active-value="1" :inactive-value="0" @change="toggle(row)" />
        </template></el-table-column>
        <el-table-column label="通知" width="80"><template #default="{ row }">
          <el-tag v-if="row.notify_enabled" type="success" size="small">开</el-tag>
          <el-tag v-else type="info" size="small">关</el-tag>
        </template></el-table-column>
        <el-table-column label="Lark群" width="120"><template #default="{ row }">{{ groupName(row.lark_group_id) }}</template></el-table-column>
        <el-table-column label="@人" min-width="140"><template #default="{ row }">
          <span v-if="row.at_user_ids && row.at_user_ids.length">{{ atNames(row.at_user_ids) }}</span>
          <span v-else class="muted">—</span>
        </template></el-table-column>
        <el-table-column label="上次运行" width="150"><template #default="{ row }">
          {{ row.last_run_at || '—' }}
          <div v-if="row.next_run_at" class="muted" style="font-size:12px">下次 {{ row.next_run_at }}</div>
        </template></el-table-column>
        <el-table-column label="结果" width="110"><template #default="{ row }">
          <el-tooltip v-if="row.last_run_at" :content="row.last_result">
            <el-tag :type="row.last_ok ? 'success' : 'danger'" size="small">{{ row.last_ok ? '✅ 成功' : '❌ 失败' }}</el-tag>
          </el-tooltip>
          <span v-else class="muted">—</span>
        </template></el-table-column>
        <el-table-column label="操作" width="110" fixed="right"><template #default="{ row }">
          <div style="display:flex;gap:8px;align-items:center">
            <el-tooltip content="立即运行"><el-button link type="primary" :icon="VideoPlay" :loading="running[row.task_key]" @click="run(row)" /></el-tooltip>
            <el-tooltip content="编辑"><el-button link type="primary" :icon="Edit" @click="openEdit(row)" /></el-tooltip>
          </div>
        </template></el-table-column>
      </el-table>
      <el-pagination v-if="tasks.length > tSize" v-model:current-page="tPage" v-model:page-size="tSize" :page-sizes="[10,20,50,100]"
        :total="tasks.length" layout="total, sizes, prev, pager, next" style="margin-top:12px; justify-content:flex-end" />
    </el-card>

    <el-dialog :close-on-click-modal="false" :close-on-press-escape="false" v-model="dlg" :title="`编辑任务：${form.name}`" width="520px">
      <el-form :model="form" label-width="90px">
        <el-form-item label="频率">
          <el-select v-model="form.preset" style="width:180px" @change="onPreset">
            <el-option v-for="p in presets" :key="p.v" :label="p.l" :value="p.v" />
          </el-select>
          <el-input v-if="form.preset==='custom'" v-model="form.schedule" placeholder="cron：分 时 日 月 周" style="width:170px; margin-left:8px" />
        </el-form-item>
        <el-form-item label="启用"><el-switch v-model="form.enabled" :active-value="1" :inactive-value="0" /></el-form-item>
        <el-divider style="margin:8px 0" />
        <el-form-item label="Lark 通知"><el-switch v-model="form.notify_enabled" :active-value="1" :inactive-value="0" /></el-form-item>
        <template v-if="form.notify_enabled">
          <el-form-item label="发送到群">
            <el-select v-model="form.lark_group_id" clearable placeholder="选择 Lark 群" style="width:240px">
              <el-option v-for="g in groups" :key="g.id" :label="g.name" :value="g.id" />
            </el-select>
            <span v-if="!groups.length" class="muted" style="margin-left:8px">先去「通知」页加群</span>
          </el-form-item>
          <el-form-item label="通知时机">
            <el-radio-group v-model="form.notify_when">
              <el-radio value="always">每次都发</el-radio>
              <el-radio value="fail">仅失败时发</el-radio>
            </el-radio-group>
          </el-form-item>
          <el-form-item label="@ 提醒人">
            <el-select v-model="form.at_user_ids" multiple clearable placeholder="选择 @ 的人" style="width:100%">
              <el-option v-for="u in users" :key="u.id" :label="u.name" :value="u.id" />
            </el-select>
          </el-form-item>
        </template>
      </el-form>
      <template #footer>
        <el-button :icon="VideoPlay" :loading="running[form.task_key]" @click="runFromDialog">立即运行一次</el-button>
        <el-button @click="dlg=false">取消</el-button>
        <el-button type="primary" @click="save">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Refresh, VideoPlay, Edit } from '@element-plus/icons-vue'
import { listScheduledTasks, updateScheduledTask, runScheduledTask, listLarkGroups, listNotifyUsers } from '../api/cmdb'
import { usePaged } from '../composables/usePaged'

const presets = [
  { v: '0 */6 * * *', l: '每 6 小时' },
  { v: '0 */12 * * *', l: '每 12 小时' },
  { v: '0 3 * * *', l: '每天一次（03:00）' },
  { v: '0 3,15 * * *', l: '每天两次（03/15）' },
  { v: '0 9 * * *', l: '每天一次（09:00）' },
  { v: 'custom', l: '自定义 cron' },
]
const desc = {
  refresh_expiry: 'WHOIS 查所有域名的注册到期，更新到库。',
  inspect: '连 443 检测所有证书到期（主域名 + 业务域名），统一一处。',
  auto_renew: '扫描到期前 30 天的证书自动重签。',
  remind: '按到期提醒阈值命中则推 Lark。',
  dns_sync: '从所有数据源全量同步 DNS 记录到缓存。',
  host_sync: '同步所有云账号所有 project 的主机（各用各自凭据）。',
}
const tasks = ref([]), groups = ref([]), users = ref([]), loading = ref(false)
const { page: tPage, size: tSize, paged: tPaged } = usePaged(tasks)
const running = ref({})
const dlg = ref(false), form = ref({})

function groupName(id) { const g = groups.value.find((x) => x.id === id); return g ? g.name : '—' }
function atNames(ids) { return (ids || []).map((id) => users.value.find((u) => u.id === id)?.name).filter(Boolean).join('、') || '—' }

async function load() {
  loading.value = true
  try {
    groups.value = await listLarkGroups()
    users.value = await listNotifyUsers()
    tasks.value = await listScheduledTasks()
  } catch (e) {} finally { loading.value = false }
}
function openEdit(row) {
  const p = presets.find((p) => p.v === row.schedule)
  form.value = { ...row, preset: p ? p.v : 'custom', at_user_ids: [...(row.at_user_ids || [])] }
  dlg.value = true
}
function onPreset() { if (form.value.preset !== 'custom') form.value.schedule = form.value.preset }
async function save() {
  const schedule = form.value.preset === 'custom' ? form.value.schedule : form.value.preset
  try {
    await updateScheduledTask(form.value.task_key, {
      schedule, enabled: form.value.enabled, notify_enabled: form.value.notify_enabled,
      lark_group_id: form.value.lark_group_id ?? null, notify_when: form.value.notify_when,
      at_user_ids: form.value.at_user_ids,
    })
    ElMessage.success('已保存，cron 已热重载'); dlg.value = false; load()
  } catch (e) { ElMessage.error(e.response?.data?.error || '保存失败') }
}
async function toggle(row) {
  try { await updateScheduledTask(row.task_key, { enabled: row.enabled }); ElMessage.success(row.enabled ? '已启用' : '已停用'); load() }
  catch (e) { ElMessage.error('失败'); row.enabled = row.enabled ? 0 : 1 }
}
async function run(row) {
  running.value = { ...running.value, [row.task_key]: true }
  try { await runScheduledTask(row.task_key); ElMessage.success('已触发，约几秒后自动刷新结果'); setTimeout(load, 3000) }
  catch (e) { ElMessage.error('触发失败') }
  finally { running.value = { ...running.value, [row.task_key]: false } }
}
function runFromDialog() { run(form.value) }
onMounted(load)
</script>

<style scoped>
.mono { font-family: ui-monospace, Menlo, Consolas, monospace; font-size: 12px; }
</style>
