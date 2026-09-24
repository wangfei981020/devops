<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'
import api from '@/api'
import { useAppStore, useAuthStore } from '@/stores'

const appStore = useAppStore()
const authStore = useAuthStore()
const isSuperAdmin = computed(() => authStore.isSuperAdmin())

// 按钮级权限。服务端对同样的权限码有强制校验，这里只是不给看/不给点，
// 真正拦住越权的是后端 —— 前端隐藏按钮挡不住直接打接口。
const can = (code) => isSuperAdmin.value || authStore.hasPermission(code)
const canEnvCreate     = computed(() => can('table_alert:env_create'))
const canEnvUpdate     = computed(() => can('table_alert:env_update'))
const canEnvDelete     = computed(() => can('table_alert:env_delete'))
const canCollect       = computed(() => can('table_alert:collect'))
const canRuleUpdate    = computed(() => can('table_alert:rule_update'))
const canBotManage     = computed(() => can('table_alert:bot_manage'))
const canContactManage = computed(() => can('table_alert:contact_manage'))
const canAck           = computed(() => can('table_alert:ack'))
const canViewRaw       = computed(() => can('table_alert:view_raw'))

const activeTab = ref('rooms')   // rooms / envs / alert / logs

// ===================== 环境 =====================
const envs = ref([])
const currentEnvId = ref('')
const currentEnv = computed(() => envs.value.find(e => e.id === currentEnvId.value) || null)

async function loadEnvs(keepSelection = true) {
  try {
    const res = await api.get('/api/table-alert/envs')
    envs.value = res.data || []
    if (!keepSelection || !envs.value.find(e => e.id === currentEnvId.value)) {
      const enabled = envs.value.find(e => e.enabled) || envs.value[0]
      currentEnvId.value = enabled ? enabled.id : ''
    }
  } catch (e) {
    appStore.showToast('读取环境失败: ' + errText(e), 'error')
  }
}

// 采集是否落后（超过 2 个周期就标红，采集停了页面不能装作一切正常）
const collectStale = computed(() => {
  const e = currentEnv.value
  if (!e || !e.enabled || !e.last_collect_at) return false
  const last = new Date(e.last_collect_at.replace(/-/g, '/')).getTime()
  return Date.now() - last > (e.interval_sec || 60) * 2 * 1000
})

// ===================== 桌台列表 =====================
const stats = ref({ total: 0, enable: 0, disable: 0, maintaining: 0, alerting: 0 })
const rooms = ref([])
const roomsTotal = ref(0)
const roomPage = ref(1)
const roomSize = ref(20)
const filters = ref({ status: '', maintaining: '', q: '' })
const loadingRooms = ref(false)

async function loadRooms() {
  if (!currentEnvId.value) { rooms.value = []; roomsTotal.value = 0; return }
  loadingRooms.value = true
  try {
    const params = {
      env_id: currentEnvId.value,
      page: roomPage.value,
      size: roomSize.value,
      ...(filters.value.status ? { status: filters.value.status } : {}),
      ...(filters.value.maintaining !== '' ? { maintaining: filters.value.maintaining } : {}),
      ...(filters.value.q ? { q: filters.value.q } : {})
    }
    const [r1, r2] = await Promise.all([
      api.get('/api/table-alert/rooms', { params }),
      api.get('/api/table-alert/stats', { params: { env_id: currentEnvId.value } })
    ])
    rooms.value = r1.data?.items || []
    roomsTotal.value = r1.data?.total || 0
    stats.value = r2.data || stats.value
  } catch (e) {
    appStore.showToast('加载桌台失败: ' + errText(e), 'error')
  } finally {
    loadingRooms.value = false
  }
}

function applyFilter() { roomPage.value = 1; loadRooms() }
function resetFilter() { filters.value = { status: '', maintaining: '', q: '' }; applyFilter() }

async function collectNow() {
  if (!currentEnvId.value) return
  appStore.showToast('正在采集…', 'info')
  try {
    const res = await api.post(`/api/table-alert/envs/${currentEnvId.value}/collect`)
    if (res.data?.ok) {
      const r = res.data.result || {}
      appStore.showToast(`采集完成：${r.record_count} 台，维护中 ${r.maintain_count}，耗时 ${r.duration_ms}ms`, 'success')
    } else {
      appStore.showToast('采集失败: ' + (res.data?.error || '未知错误'), 'error')
    }
    await Promise.all([loadEnvs(), loadRooms()])
  } catch (e) {
    appStore.showToast('采集失败: ' + errText(e), 'error')
  }
}

// 确认（停止告警 + 静默）
async function ackRoom(room) {
  const ev = await findActiveEvent(room.room_id)
  if (!ev) { appStore.showToast('这张桌台当前没有进行中的告警事件', 'info'); return }
  const ok = await appStore.showConfirm({
    type: 'warning',
    title: `确认桌台 ${room.table_no}`,
    message: '确认后将停止对这张桌台的重复告警，并进入静默期。\n如果维护一直没结束，静默到期后会重新开始告警。',
    okText: '确认',
    cancelText: '取消'
  })
  if (!ok) return
  try {
    const res = await api.post(`/api/table-alert/events/${ev.id}/ack`)
    appStore.showToast(`已确认，静默至 ${res.data?.silence_until || ''}`, 'success')
    loadRooms()
  } catch (e) {
    appStore.showToast('确认失败: ' + errText(e), 'error')
  }
}

async function findActiveEvent(roomId) {
  const res = await api.get('/api/table-alert/events', {
    params: { env_id: currentEnvId.value, active: 1, size: 200 }
  })
  return (res.data?.items || []).find(x => x.room_id === roomId)
}

// ===================== 环境配置编辑 =====================
const envDialog = ref(false)
const envForm = ref(null)
const envSaving = ref(false)
const testResult = ref(null)

function blankEnv() {
  return {
    id: '', name: '', enabled: false, sort_order: 0,
    url: '', method: 'GET', host_header: '', request_body: '', extra_headers: '',
    token: '', token_place: 'none', skip_tls_verify: false, timeout_sec: 10,
    cur_page: 1, page_size: 500,
    data_path: 'data.records', total_path: 'data.total',
    f_room_id: 'id', f_table_no: 'tableNo', f_room_no: 'roomNo',
    f_platform_id: 'gamePlatformId', f_status: 'status',
    f_maintain: 'gameRoomMaintainList', f_operator: 'operator',
    f_update_time: 'updateTime', f_online_total: 'onlineUserTotal',
    maintain_rule: 'list_not_empty', maintain_status_value: '',
    interval_sec: 60, log_raw_response: true
  }
}

function openCreateEnv() {
  envForm.value = blankEnv()
  testResult.value = null
  envDialog.value = true
}

function openEditEnv(env) {
  envForm.value = { ...blankEnv(), ...env, token: '' }   // token 不回显，留空=不修改
  testResult.value = null
  envDialog.value = true
}

async function saveEnv() {
  const f = envForm.value
  if (!f.name?.trim()) { appStore.showToast('环境名称不能为空', 'error'); return }
  if (f.enabled && !f.url?.trim()) { appStore.showToast('启用的环境必须填请求地址', 'error'); return }
  envSaving.value = true
  try {
    if (f.id) await api.put(`/api/table-alert/envs/${f.id}`, f)
    else await api.post('/api/table-alert/envs', f)
    appStore.showToast('已保存', 'success')
    envDialog.value = false
    await loadEnvs()
    loadRooms()
  } catch (e) {
    appStore.showToast('保存失败: ' + errText(e), 'error')
  } finally {
    envSaving.value = false
  }
}

async function testEnv() {
  const f = envForm.value
  if (!f.url?.trim()) { appStore.showToast('请先填写请求地址', 'error'); return }
  if (!f.id) { appStore.showToast('请先保存环境，再测试连接', 'info'); return }
  testResult.value = { loading: true }
  try {
    const res = await api.post(`/api/table-alert/envs/${f.id}/test`, f)
    testResult.value = res.data
  } catch (e) {
    testResult.value = { ok: false, error: errText(e) }
  }
}

async function deleteEnv(env) {
  const ok = await appStore.showConfirm({
    type: 'danger',
    title: `删除环境 ${env.name}`,
    message: '会一并删除这个环境下的桌台快照、告警事件和采集日志，且不可恢复。',
    okText: '确定删除', cancelText: '取消'
  })
  if (!ok) return
  try {
    await api.delete(`/api/table-alert/envs/${env.id}`)
    appStore.showToast('已删除', 'success')
    await loadEnvs(false)
    loadRooms()
  } catch (e) {
    appStore.showToast('删除失败: ' + errText(e), 'error')
  }
}

// ===================== 告警规则 =====================
const rule = ref(null)
const ruleSaving = ref(false)
const bots = ref([])
const contacts = ref([])

const intervalOptions = computed(() => {
  // 告警间隔不能小于采集间隔，小于的选项直接禁用
  const sec = currentEnv.value?.interval_sec || 60
  const minMin = Math.max(1, Math.ceil(sec / 60))
  return [1, 3, 5, 10, 15, 30, 60].map(v => ({ value: v, disabled: v < minMin }))
})

async function loadRule() {
  if (!currentEnvId.value) { rule.value = null; return }
  try {
    const res = await api.get('/api/table-alert/rules', { params: { env_id: currentEnvId.value } })
    rule.value = res.data
    if (!Array.isArray(rule.value.bot_ids)) rule.value.bot_ids = []
  } catch (e) {
    appStore.showToast('读取告警规则失败: ' + errText(e), 'error')
  }
}

async function saveRule() {
  if (!rule.value) return
  rule.value.env_id = currentEnvId.value
  ruleSaving.value = true
  try {
    await api.put('/api/table-alert/rules', rule.value)
    appStore.showToast('告警规则已保存并立即生效', 'success')
    loadRule()
  } catch (e) {
    appStore.showToast('保存失败: ' + errText(e), 'error')
  } finally {
    ruleSaving.value = false
  }
}

function toggleBot(id) {
  const arr = rule.value.bot_ids
  const i = arr.indexOf(id)
  if (i >= 0) arr.splice(i, 1); else arr.push(id)
}

// 艾特人：规则里存 lark_id 列表（逗号分隔），页面上按通讯录勾选
const atSelected = computed({
  get: () => (rule.value?.at_lark_ids || '').split(',').map(s => s.trim()).filter(Boolean),
  set: v => { if (rule.value) rule.value.at_lark_ids = v.join(',') }
})
const escalateSelected = computed({
  get: () => (rule.value?.escalate_at_lark_ids || '').split(',').map(s => s.trim()).filter(Boolean),
  set: v => { if (rule.value) rule.value.escalate_at_lark_ids = v.join(',') }
})
function toggleAt(larkId, which) {
  const target = which === 'escalate' ? escalateSelected : atSelected
  const arr = [...target.value]
  const i = arr.indexOf(larkId)
  if (i >= 0) arr.splice(i, 1); else arr.push(larkId)
  target.value = arr
}

// ===================== Lark 群 / 通知人 =====================
async function loadBots() {
  try { bots.value = (await api.get('/api/table-alert/bots')).data || [] } catch (e) { /* 静默 */ }
}
async function loadContacts() {
  try { contacts.value = (await api.get('/api/table-alert/contacts')).data || [] } catch (e) { /* 静默 */ }
}

const botDialog = ref(false)
const botForm = ref({ id: '', name: '', webhook: '', secret: '', description: '', enabled: true })

function openCreateBot() {
  botForm.value = { id: '', name: '', webhook: '', secret: '', description: '', enabled: true }
  botDialog.value = true
}
function openEditBot(b) {
  botForm.value = { id: b.id, name: b.name, webhook: '', secret: '', description: b.description, enabled: b.enabled }
  botDialog.value = true
}
async function saveBot() {
  if (!botForm.value.name?.trim()) { appStore.showToast('群名称不能为空', 'error'); return }
  if (!botForm.value.id && !botForm.value.webhook?.trim()) {
    appStore.showToast('请填写 Lark 机器人 webhook', 'error'); return
  }
  try {
    await api.post('/api/table-alert/bots', botForm.value)
    appStore.showToast('已保存', 'success')
    botDialog.value = false
    loadBots()
  } catch (e) {
    appStore.showToast('保存失败: ' + errText(e), 'error')
  }
}
async function deleteBot(b) {
  const ok = await appStore.showConfirm({
    type: 'danger', title: `删除群「${b.name}」`,
    message: '删除后所有引用了这个群的告警规则将不再往这里发送。', okText: '确定删除', cancelText: '取消'
  })
  if (!ok) return
  try { await api.delete(`/api/table-alert/bots/${b.id}`); appStore.showToast('已删除', 'success'); loadBots(); loadRule() }
  catch (e) { appStore.showToast('删除失败: ' + errText(e), 'error') }
}
async function testBot(b) {
  appStore.showToast('正在发送测试消息…', 'info')
  try {
    const res = await api.post(`/api/table-alert/bots/${b.id}/test`, {
      at_lark_ids: (rule.value?.at_lark_ids || '')
    })
    if (res.data?.ok) appStore.showToast('已发送，请到群里确认', 'success')
    else appStore.showToast('发送失败: ' + (res.data?.error || '未知错误'), 'error')
  } catch (e) {
    appStore.showToast('发送失败: ' + errText(e), 'error')
  }
}

const contactDialog = ref(false)
const contactForm = ref({ id: '', name: '', lark_id: '', remark: '' })
function openCreateContact() { contactForm.value = { id: '', name: '', lark_id: '', remark: '' }; contactDialog.value = true }
function openEditContact(c) { contactForm.value = { ...c }; contactDialog.value = true }
async function saveContact() {
  if (!contactForm.value.name?.trim()) { appStore.showToast('姓名不能为空', 'error'); return }
  try {
    await api.post('/api/table-alert/contacts', contactForm.value)
    appStore.showToast('已保存', 'success')
    contactDialog.value = false
    loadContacts()
  } catch (e) { appStore.showToast('保存失败: ' + errText(e), 'error') }
}
async function deleteContact(c) {
  const ok = await appStore.showConfirm({ type: 'danger', title: `删除通知人 ${c.name}`, message: '确定删除吗？', okText: '删除', cancelText: '取消' })
  if (!ok) return
  try { await api.delete(`/api/table-alert/contacts/${c.id}`); loadContacts() }
  catch (e) { appStore.showToast('删除失败: ' + errText(e), 'error') }
}

// ===================== 采集日志 =====================
const logs = ref([])
const logsTotal = ref(0)
const logPage = ref(1)
const logFilter = ref({ ok: '', has_change: '' })
const rawDialog = ref(false)
const rawContent = ref('')
const rawMeta = ref({})

async function loadLogs() {
  if (!currentEnvId.value) { logs.value = []; return }
  try {
    const res = await api.get('/api/table-alert/collect-logs', {
      params: {
        env_id: currentEnvId.value, page: logPage.value, size: 20,
        ...(logFilter.value.ok !== '' ? { ok: logFilter.value.ok } : {}),
        ...(logFilter.value.has_change ? { has_change: 1 } : {})
      }
    })
    logs.value = res.data?.items || []
    logsTotal.value = res.data?.total || 0
  } catch (e) {
    appStore.showToast('读取采集日志失败: ' + errText(e), 'error')
  }
}

async function viewRaw(log) {
  try {
    const res = await api.get(`/api/table-alert/collect-logs/${log.id}/raw`)
    if (!res.data?.raw) {
      appStore.showToast(res.data?.note || '该次采集未保存原始响应', 'info')
      return
    }
    try { rawContent.value = JSON.stringify(JSON.parse(res.data.raw), null, 2) }
    catch { rawContent.value = res.data.raw }
    rawMeta.value = { env: res.data.env_name, at: res.data.started_at, size: log.raw_size }
    rawDialog.value = true
  } catch (e) {
    appStore.showToast('读取失败: ' + errText(e), 'error')
  }
}

function copyRaw() {
  navigator.clipboard?.writeText(rawContent.value)
    .then(() => appStore.showToast('已复制到剪贴板', 'success'))
    .catch(() => appStore.showToast('复制失败，请手动选中', 'error'))
}

// ===================== 通用 =====================
function errText(e) { return e?.response?.data?.error || e?.message || String(e) }

// 维护时长的来源说明：区分实测跃迁与回溯估算，别让人把估算值当精确值用
function durationTip(r) {
  if (!r.maintain_since) return ''
  if (r.since_estimated) {
    return `维护开始时间 ${r.maintain_since}（估算）\n`
         + `首次采集到这张桌台时它已经在维护，没有观测到「正常→维护」的跃迁，`
         + `所以按接口返回的 updateTime 回溯。\n`
         + `updateTime 会被任何编辑操作刷新，真实维护时间可能更早。`
  }
  return `维护开始时间 ${r.maintain_since}（实测）\n采集时观测到「正常→维护」的跃迁，时间准确。`
}

function maintainBadge(r) {
  return r.maintaining ? { text: '🔧 维护中', cls: 'tag-maintain' } : { text: '正常', cls: 'tag-normal' }
}
function statusBadge(s) {
  if (s === 'Enable') return { text: 'Enable', cls: 'tag-enable' }
  if (s === 'Disable') return { text: 'Disable', cls: 'tag-disable' }
  return { text: s || '-', cls: 'tag-unknown' }
}

let timer = null
const autoRefresh = ref(true)

async function switchEnv() {
  roomPage.value = 1
  logPage.value = 1
  await Promise.all([loadRooms(), loadRule(), loadLogs()])
}

onMounted(async () => {
  await loadEnvs()
  await Promise.all([loadBots(), loadContacts()])
  await switchEnv()
  timer = setInterval(() => {
    if (!autoRefresh.value) return
    loadEnvs()
    if (activeTab.value === 'rooms') loadRooms()
    if (activeTab.value === 'logs') loadLogs()
  }, 30000)
})
onUnmounted(() => { if (timer) clearInterval(timer) })
</script>

<template>
  <div class="ta-page">
    <div class="page-header">
      <h2>桌台维护告警</h2>
      <div class="header-actions">
        <label class="auto-refresh">
          <input type="checkbox" v-model="autoRefresh"> 自动刷新(30s)
        </label>
        <button v-if="canCollect" class="btn btn-secondary" @click="collectNow" :disabled="!currentEnv?.enabled">🔄 立即采集</button>
      </div>
    </div>

    <!-- 环境切换 + 采集状态 -->
    <div class="env-bar" :class="{ stale: collectStale }">
      <div class="env-left">
        <span class="label">环境</span>
        <select v-model="currentEnvId" @change="switchEnv" class="env-select">
          <option v-for="e in envs" :key="e.id" :value="e.id">
            {{ e.name }}{{ e.enabled ? '' : '（未启用）' }}
          </option>
        </select>
      </div>
      <div class="env-right" v-if="currentEnv">
        <template v-if="!currentEnv.enabled">
          <span class="collect-idle">未启用 —— 到「环境配置」填好地址并勾选启用后才会采集</span>
        </template>
        <template v-else-if="currentEnv.last_collect_at">
          <span :class="currentEnv.last_collect_ok ? 'ok' : 'fail'">
            {{ currentEnv.last_collect_ok ? '✅' : '❌' }}
          </span>
          采集于 {{ currentEnv.last_collect_at }}
          · {{ currentEnv.last_collect_count }} 台
          · {{ currentEnv.last_duration_ms }}ms
          · 每 {{ currentEnv.interval_sec }}s 一次
          <span v-if="collectStale" class="stale-warn">⚠ 采集已落后，请到采集日志查原因</span>
          <span v-if="!currentEnv.last_collect_ok && currentEnv.last_collect_error" class="err-msg">
            {{ currentEnv.last_collect_error }}
          </span>
        </template>
        <template v-else>
          <span class="collect-idle">尚未采集过</span>
        </template>
      </div>
    </div>

    <!-- Tabs -->
    <div class="tabs">
      <button class="tab" :class="{ active: activeTab === 'rooms' }" @click="activeTab = 'rooms'">🎰 桌台列表</button>
      <button class="tab" :class="{ active: activeTab === 'envs' }" @click="activeTab = 'envs'">⚙️ 环境配置</button>
      <button class="tab" :class="{ active: activeTab === 'alert' }" @click="activeTab = 'alert'; loadRule()">🔔 告警设置</button>
      <button class="tab" :class="{ active: activeTab === 'logs' }" @click="activeTab = 'logs'; loadLogs()">📋 采集日志</button>
    </div>

    <!-- ================= Tab 桌台列表 ================= -->
    <div v-if="activeTab === 'rooms'" class="tab-content">
      <div class="stat-row">
        <div class="stat-card"><div class="sc-num">{{ stats.total }}</div><div class="sc-label">总桌台</div></div>
        <div class="stat-card maintain"><div class="sc-num">{{ stats.maintaining }}</div><div class="sc-label">🔧 维护中</div></div>
        <div class="stat-card enable"><div class="sc-num">{{ stats.enable }}</div><div class="sc-label">Enable</div></div>
        <div class="stat-card disable"><div class="sc-num">{{ stats.disable }}</div><div class="sc-label">Disable</div></div>
        <div class="stat-card alerting"><div class="sc-num">{{ stats.alerting }}</div><div class="sc-label">告警中</div></div>
      </div>

      <div class="filter-bar">
        <select v-model="filters.maintaining" @change="applyFilter">
          <option value="">维护状态：全部</option>
          <option value="1">仅维护中</option>
          <option value="0">仅正常</option>
        </select>
        <select v-model="filters.status" @change="applyFilter">
          <option value="">启停：全部</option>
          <option value="Enable">Enable</option>
          <option value="Disable">Disable</option>
        </select>
        <input v-model="filters.q" placeholder="桌台号 / 房间号 / 平台ID" @keyup.enter="applyFilter">
        <button class="btn btn-primary" @click="applyFilter">搜索</button>
        <button class="btn btn-secondary" @click="resetFilter">重置</button>
      </div>

      <p class="hint-line">
        「启停」和「维护」是两件事：<code>status</code> 是桌台启用/关闭，
        <code>gameRoomMaintainList</code> 非空才是维护中 —— <strong>只有维护中才会告警</strong>。
        维护时长带「估」的表示系统首次采集时它已在维护，开始时间由接口 <code>updateTime</code> 回溯，
        真实时间可能更早；在线人数带「?」表示该桌台维护中、这个数多半已停止更新。
      </p>

      <table class="data-table">
        <thead>
          <tr>
            <th>桌台</th><th>房间号</th><th>平台</th><th>启停</th><th>维护</th>
            <th>影响站点</th>
            <th title="带「估」字的是回溯估算：系统首次采集时该桌台已在维护，没有观测到跃迁">维护时长</th>
            <th title="接口原样返回；维护中的桌台该字段通常不再更新">在线人数</th>
            <th>告警</th><th>操作人</th><th>操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="loadingRooms"><td colspan="11" class="empty">加载中…</td></tr>
          <tr v-else-if="!rooms.length"><td colspan="11" class="empty">
            暂无数据 —— 如果这个环境刚配好，点右上角「立即采集」拉一次
          </td></tr>
          <tr v-for="r in rooms" :key="r.room_id" :class="{ 'row-maintain': r.maintaining, 'row-acked': r.event_state === 'acked' }">
            <td class="mono strong">{{ r.table_no }}</td>
            <td class="mono">{{ r.room_no }}</td>
            <td class="mono dim">{{ r.platform_id }}</td>
            <td><span class="tag" :class="statusBadge(r.status).cls">{{ statusBadge(r.status).text }}</span></td>
            <td><span class="tag" :class="maintainBadge(r).cls">{{ maintainBadge(r).text }}</span></td>
            <td>{{ r.maintaining ? r.maintain_site_count + ' 个' : '—' }}</td>
            <td :class="{ 'dur-long': r.duration_min >= 60 }">
              <template v-if="r.duration_text">
                <span :title="durationTip(r)">{{ r.since_estimated ? '≈' : '' }}{{ r.duration_text }}</span>
                <span v-if="r.since_estimated" class="est-mark" :title="durationTip(r)">估</span>
              </template>
              <template v-else>—</template>
            </td>
            <td>
              <span v-if="r.maintaining" class="dim" :title="'接口原样返回 ' + r.online_user_total + '；维护中的桌台该字段通常不再更新，仅供参考'">
                {{ r.online_user_total }} <span class="stale-mark">?</span>
              </span>
              <span v-else>{{ r.online_user_total }}</span>
            </td>
            <td>
              <span v-if="r.alert_count">{{ r.alert_count }} 次</span>
              <span v-else>—</span>
            </td>
            <td class="dim">{{ r.operator }}</td>
            <td>
              <span v-if="r.event_state === 'acked'" class="acked-label">{{ r.acked_by }} 已确认</span>
              <button v-else-if="r.maintaining && canAck" class="btn-link" @click="ackRoom(r)">确认</button>
              <span v-else-if="r.maintaining" class="dim">维护中</span>
              <span v-else>—</span>
            </td>
          </tr>
        </tbody>
      </table>

      <div class="pager" v-if="roomsTotal > roomSize">
        <button class="btn btn-secondary" :disabled="roomPage <= 1" @click="roomPage--; loadRooms()">上一页</button>
        <span>第 {{ roomPage }} 页 / 共 {{ Math.ceil(roomsTotal / roomSize) }} 页（{{ roomsTotal }} 条）</span>
        <button class="btn btn-secondary" :disabled="roomPage >= Math.ceil(roomsTotal / roomSize)" @click="roomPage++; loadRooms()">下一页</button>
      </div>
    </div>

    <!-- ================= Tab 环境配置 ================= -->
    <div v-if="activeTab === 'envs'" class="tab-content">
      <div class="action-bar">
        <button v-if="canEnvCreate" class="btn btn-primary" @click="openCreateEnv">+ 新增环境</button>
        <span class="hint">不同环境的接口地址不一样，各填各的；地址和 token 都不写在代码里。</span>
      </div>

      <table class="data-table">
        <thead>
          <tr><th>环境</th><th>地址</th><th>Host 头</th><th>采集间隔</th><th>上次采集</th><th>状态</th><th>操作</th></tr>
        </thead>
        <tbody>
          <tr v-if="!envs.length"><td colspan="7" class="empty">还没有环境，点「新增环境」开始</td></tr>
          <tr v-for="e in envs" :key="e.id">
            <td class="strong">{{ e.name }}</td>
            <td class="mono small">{{ e.url || '（未填写）' }}</td>
            <td class="mono small">{{ e.host_header || '—' }}</td>
            <td>{{ e.interval_sec }}s</td>
            <td class="small">{{ e.last_collect_at || '—' }}</td>
            <td>
              <span v-if="!e.enabled" class="tag tag-unknown">未启用</span>
              <span v-else-if="e.last_collect_ok" class="tag tag-enable">正常</span>
              <span v-else class="tag tag-disable">异常</span>
            </td>
            <td>
              <button v-if="canEnvUpdate" class="btn-link" @click="openEditEnv(e)">编辑</button>
              <button v-if="canEnvDelete" class="btn-link danger" @click="deleteEnv(e)">删除</button>
              <span v-if="!canEnvUpdate && !canEnvDelete" class="dim">—</span>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- ================= Tab 告警设置 ================= -->
    <div v-if="activeTab === 'alert'" class="tab-content">
      <div v-if="!currentEnvId" class="empty-block">请先在上方选择一个环境</div>
      <div v-else-if="rule" class="rule-panel">
        <div class="panel">
          <h3>触发与频率 —— 环境「{{ currentEnv?.name }}」</h3>
          <label class="line"><input type="checkbox" v-model="rule.enabled"> 启用告警</label>
          <div class="line">
            桌台维护超过 <input type="number" v-model.number="rule.threshold_min" min="0" class="num"> 分钟开始告警
          </div>
          <div class="line">
            每隔
            <select v-model.number="rule.interval_min" class="num-select">
              <option v-for="o in intervalOptions" :key="o.value" :value="o.value" :disabled="o.disabled">
                {{ o.value }} 分钟{{ o.disabled ? '（小于采集间隔）' : '' }}
              </option>
            </select>
            告警一次
            <span class="inline-hint">采集间隔 {{ currentEnv?.interval_sec }}s，告警间隔不能比它还短，否则是拿同一份快照重复报</span>
          </div>
          <div class="line">
            最多 <input type="number" v-model.number="rule.max_times" min="1" class="num"> 次后
            <label><input type="radio" :value="true" v-model="rule.escalate"> 升级为每
              <input type="number" v-model.number="rule.escalate_interval_min" min="1" class="num"> 分钟</label>
            <label><input type="radio" :value="false" v-model="rule.escalate"> 停止告警</label>
          </div>
          <label class="line"><input type="checkbox" v-model="rule.notify_on_recover"> 桌台恢复正常时发一条恢复通知</label>
        </div>

        <div class="panel">
          <h3>发送到哪些群（Lark）</h3>
          <p class="hint">可以同时发多个群，勾选即可。</p>
          <div class="bot-list">
            <label v-for="b in bots" :key="b.id" class="bot-item" :class="{ checked: rule.bot_ids.includes(b.id) }">
              <input type="checkbox" :checked="rule.bot_ids.includes(b.id)" @change="toggleBot(b.id)">
              <span class="bot-name">{{ b.name }}</span>
              <span class="bot-hook mono">{{ b.webhook_masked }}</span>
              <span v-if="!b.enabled" class="tag tag-unknown">已禁用</span>
              <template v-if="canBotManage">
                <button class="btn-link" @click.prevent="testBot(b)">发送测试</button>
                <button class="btn-link" @click.prevent="openEditBot(b)">编辑</button>
                <button class="btn-link danger" @click.prevent="deleteBot(b)">删除</button>
              </template>
            </label>
            <div v-if="!bots.length" class="empty-block small">
              还没有配置 Lark 群。点下面「+ 添加 Lark 群」，把机器人 webhook 填进来。
            </div>
          </div>
          <button v-if="canBotManage" class="btn btn-secondary" @click="openCreateBot">+ 添加 Lark 群</button>
        </div>

        <div class="panel">
          <h3>艾特谁</h3>
          <p class="hint">在通讯录里勾选；没有 Lark ID 的人无法被艾特。</p>
          <div class="at-block">
            <div class="at-title">常规告警艾特</div>
            <div class="chip-list">
              <label v-for="c in contacts" :key="c.id" class="chip"
                     :class="{ on: atSelected.includes(c.lark_id), disabled: !c.lark_id }">
                <input type="checkbox" :disabled="!c.lark_id"
                       :checked="atSelected.includes(c.lark_id)" @change="toggleAt(c.lark_id, 'normal')">
                {{ c.name }}<span v-if="!c.lark_id" class="no-id">（缺 Lark ID）</span>
              </label>
              <span v-if="!contacts.length" class="hint">通讯录为空，先在下面添加通知人</span>
            </div>
          </div>
          <div class="at-block">
            <div class="at-title">升级时追加艾特</div>
            <div class="chip-list">
              <label v-for="c in contacts" :key="'e' + c.id" class="chip"
                     :class="{ on: escalateSelected.includes(c.lark_id), disabled: !c.lark_id }">
                <input type="checkbox" :disabled="!c.lark_id"
                       :checked="escalateSelected.includes(c.lark_id)" @change="toggleAt(c.lark_id, 'escalate')">
                {{ c.name }}
              </label>
            </div>
          </div>
          <label class="line"><input type="checkbox" v-model="rule.reat_every_time"> 未确认时每次告警都重新艾特，已确认则停止</label>

          <div class="contact-mgr">
            <div class="cm-head">
              <span>通讯录</span>
              <button v-if="canContactManage" class="btn-link" @click="openCreateContact">+ 添加通知人</button>
            </div>
            <table class="data-table compact">
              <thead><tr><th>姓名</th><th>Lark ID</th><th>备注</th><th>操作</th></tr></thead>
              <tbody>
                <tr v-if="!contacts.length"><td colspan="4" class="empty">暂无</td></tr>
                <tr v-for="c in contacts" :key="c.id">
                  <td>{{ c.name }}</td>
                  <td class="mono small">{{ c.lark_id || '（未填）' }}</td>
                  <td class="dim">{{ c.remark }}</td>
                  <td>
                    <template v-if="canContactManage">
                      <button class="btn-link" @click="openEditContact(c)">编辑</button>
                      <button class="btn-link danger" @click="deleteContact(c)">删除</button>
                    </template>
                    <span v-else class="dim">—</span>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>

        <div class="panel">
          <h3>静默</h3>
          <div class="line">确认后静默 <input type="number" v-model.number="rule.silence_after_ack_min" min="0" class="num"> 分钟</div>
          <div class="line">
            <label><input type="checkbox" v-model="rule.quiet_enabled"> 免打扰时段</label>
            <input type="text" v-model="rule.quiet_start" class="time" placeholder="03:00"> -
            <input type="text" v-model="rule.quiet_end" class="time" placeholder="08:00">
            <span class="inline-hint">支持跨零点，如 23:00 - 07:00</span>
          </div>
        </div>

        <div class="panel-actions">
          <button v-if="canRuleUpdate" class="btn btn-primary" @click="saveRule" :disabled="ruleSaving">
            {{ ruleSaving ? '保存中…' : '保存并立即生效' }}
          </button>
          <span v-else class="hint">没有「修改告警规则」权限，当前为只读</span>
        </div>
      </div>
    </div>

    <!-- ================= Tab 采集日志 ================= -->
    <div v-if="activeTab === 'logs'" class="tab-content">
      <div class="filter-bar">
        <select v-model="logFilter.ok" @change="logPage = 1; loadLogs()">
          <option value="">结果：全部</option>
          <option value="1">仅成功</option>
          <option value="0">仅失败</option>
        </select>
        <label class="cb"><input type="checkbox" v-model="logFilter.has_change" @change="logPage = 1; loadLogs()"> 只看有状态变化的</label>
        <button class="btn btn-secondary" @click="loadLogs">刷新</button>
      </div>

      <p class="hint-line">
        状态比对只看<strong>维护状态 / 启停状态 / 操作人</strong>三项；
        在线人数这类每次都在抖的字段一律忽略，否则日志会被噪音淹没。
        完整响应体同时也打到了容器控制台（<code>[table-alert][DEBUG]</code>），可直接 kubectl logs 查。
      </p>

      <table class="data-table">
        <thead>
          <tr><th>时间</th><th>结果</th><th>耗时</th><th>条数</th><th>维护中</th><th>变化</th><th>大小</th><th>操作</th></tr>
        </thead>
        <tbody>
          <tr v-if="!logs.length"><td colspan="8" class="empty">暂无采集日志</td></tr>
          <template v-for="l in logs" :key="l.id">
            <tr :class="{ 'row-fail': !l.ok }">
              <td class="mono small">{{ l.started_at }}</td>
              <td>
                <span v-if="l.ok" class="tag tag-enable">✅ {{ l.http_status }}</span>
                <span v-else class="tag tag-disable">❌ {{ l.http_status || '错误' }}</span>
              </td>
              <td>{{ l.duration_ms }}ms</td>
              <td>{{ l.record_count }}<span v-if="l.total_count" class="dim"> / {{ l.total_count }}</span></td>
              <td :class="{ strong: l.maintain_count > 0 }">{{ l.maintain_count }}</td>
              <td>
                <span v-if="l.change_count" class="tag tag-maintain">{{ l.change_count }} 条</span>
                <span v-else class="dim">0</span>
              </td>
              <td class="dim small">{{ l.raw_size ? (l.raw_size / 1024).toFixed(1) + ' KB' : '—' }}</td>
              <td>
                <button v-if="l.has_raw && canViewRaw" class="btn-link" @click="viewRaw(l)">看原始响应</button>
                <span v-else-if="l.has_raw" class="dim" title="需要「查看原始响应」权限">无权查看</span>
                <span v-else class="dim">未留存</span>
              </td>
            </tr>
            <tr v-if="l.changes && l.changes.length" class="change-row">
              <td colspan="8">
                <div v-for="(c, i) in l.changes" :key="i" class="change-item">
                  <span class="mono strong">{{ c.table_no }}</span>
                  <span class="dim">({{ c.room_no }})</span>
                  {{ c.field }}：<span class="from">{{ c.from || '空' }}</span> → <span class="to">{{ c.to }}</span>
                </div>
              </td>
            </tr>
            <tr v-if="!l.ok && l.error_msg" class="err-row">
              <td colspan="8"><span class="err-msg">{{ l.error_msg }}</span></td>
            </tr>
          </template>
        </tbody>
      </table>

      <div class="pager" v-if="logsTotal > 20">
        <button class="btn btn-secondary" :disabled="logPage <= 1" @click="logPage--; loadLogs()">上一页</button>
        <span>第 {{ logPage }} 页 / 共 {{ Math.ceil(logsTotal / 20) }} 页</span>
        <button class="btn btn-secondary" :disabled="logPage >= Math.ceil(logsTotal / 20)" @click="logPage++; loadLogs()">下一页</button>
      </div>
    </div>

    <!-- ================= 环境编辑弹窗 ================= -->
    <div v-if="envDialog" class="modal-mask" @click.self="envDialog = false">
      <div class="modal wide">
        <div class="modal-head">
          <h3>{{ envForm.id ? '编辑环境' : '新增环境' }}</h3>
          <button class="close" @click="envDialog = false">×</button>
        </div>
        <div class="modal-body">
          <div class="form-row">
            <label>环境名称 *</label>
            <input v-model="envForm.name" placeholder="UAT / PROD / DEV">
            <label class="cb"><input type="checkbox" v-model="envForm.enabled"> 启用（启用后才会定时采集）</label>
          </div>

          <fieldset>
            <legend>请求配置</legend>
            <div class="form-row">
              <label>请求地址 *</label>
              <input v-model="envForm.url" class="wide-input" placeholder="例如 http://网关IP/gameRoom/list">
            </div>
            <div class="form-row">
              <label>请求方法</label>
              <select v-model="envForm.method"><option>GET</option><option>POST</option></select>
              <label>超时(秒)</label>
              <input type="number" v-model.number="envForm.timeout_sec" class="num">
            </div>
            <div class="form-row">
              <label>Host 头</label>
              <input v-model="envForm.host_header" class="wide-input" placeholder="按 IP 直连网关时必填">
            </div>
            <p class="field-hint">
              直接用 IP 访问网关时，<strong>Host 头必须填</strong>，否则网关匹配不到路由会直接返回 404。
            </p>
            <div class="form-row">
              <label>分页参数</label>
              curPage <input type="number" v-model.number="envForm.cur_page" class="num">
              pageSize <input type="number" v-model.number="envForm.page_size" class="num">
            </div>
            <p class="field-hint">curPage 不传的话接口会空指针，返回 <code>code:9999 Server Error</code>。</p>
            <div class="form-row" v-if="envForm.method === 'POST'">
              <label>请求体</label>
              <textarea v-model="envForm.request_body" rows="3" placeholder='{"key":"value"}'></textarea>
            </div>
            <div class="form-row">
              <label>Token</label>
              <input v-model="envForm.token" type="password"
                     :placeholder="envForm.has_token ? '已配置，留空表示不修改' : '可选，不需要就留空'">
              <select v-model="envForm.token_place">
                <option value="none">不带 token</option>
                <option value="bearer">Authorization: Bearer</option>
                <option value="raw">Authorization: 裸 token</option>
                <option value="token_header">token 请求头</option>
                <option value="query">放 query 参数</option>
              </select>
            </div>
            <div class="form-row">
              <label>额外请求头</label>
              <input v-model="envForm.extra_headers" class="wide-input" placeholder='可选，JSON 如 {"X-Foo":"bar"}'>
            </div>
            <label class="cb"><input type="checkbox" v-model="envForm.skip_tls_verify"> 跳过 TLS 证书校验（https 且证书过期时打开）</label>
          </fieldset>

          <fieldset>
            <legend>采集间隔</legend>
            <div class="form-row">
              <label>每</label>
              <select v-model.number="envForm.interval_sec">
                <option :value="30">30 秒</option>
                <option :value="60">60 秒（推荐）</option>
                <option :value="120">2 分钟</option>
                <option :value="300">5 分钟</option>
              </select>
              <label>采集一次</label>
            </div>
            <label class="cb"><input type="checkbox" v-model="envForm.log_raw_response"> 记录原始响应（调试期建议打开，稳定后关掉省空间）</label>
          </fieldset>

          <fieldset>
            <legend>响应解析</legend>
            <div class="form-row">
              <label>数据路径</label><input v-model="envForm.data_path">
              <label>总数路径</label><input v-model="envForm.total_path">
            </div>
            <div class="form-row">
              <label>桌台号</label><input v-model="envForm.f_table_no">
              <label>房间号</label><input v-model="envForm.f_room_no">
              <label>主键</label><input v-model="envForm.f_room_id">
            </div>
            <div class="form-row">
              <label>启停字段</label><input v-model="envForm.f_status">
              <label>维护字段</label><input v-model="envForm.f_maintain">
            </div>
            <div class="form-row">
              <label>平台</label><input v-model="envForm.f_platform_id">
              <label>操作人</label><input v-model="envForm.f_operator">
              <label>在线人数</label><input v-model="envForm.f_online_total">
            </div>
          </fieldset>

          <fieldset>
            <legend>「维护中」怎么判定</legend>
            <label class="radio-line">
              <input type="radio" value="list_not_empty" v-model="envForm.maintain_rule">
              维护字段非空即视为维护中（推荐）
            </label>
            <label class="radio-line">
              <input type="radio" value="status_equals" v-model="envForm.maintain_rule">
              启停字段等于
              <input v-model="envForm.maintain_status_value" class="num-wide" placeholder="例如 Maintain">
            </label>
            <p class="field-hint">
              实测：桌台被维护后 <code>status</code> 仍然是 <code>Enable</code>，真正变化的是
              <code>gameRoomMaintainList</code> 从 null 变成站点数组。所以默认用第一种。
            </p>
          </fieldset>

          <div v-if="testResult" class="test-result" :class="{ ok: testResult.ok, fail: testResult.ok === false }">
            <div v-if="testResult.loading">测试中…</div>
            <template v-else-if="testResult.ok">
              <div class="tr-head">✅ 连接成功</div>
              <div>HTTP {{ testResult.result?.http_status }} · 耗时 {{ testResult.result?.duration_ms }}ms
                · 解析出 {{ testResult.result?.record_count }} 台
                （维护中 {{ testResult.result?.maintain_count }} /
                Enable {{ testResult.result?.enable_count }} /
                Disable {{ testResult.result?.disable_count }}）</div>
              <div class="sample" v-if="testResult.sample?.length">
                <div class="sample-title">样例（核对字段映射对不对）：</div>
                <div v-for="(s, i) in testResult.sample" :key="i" class="mono small">
                  {{ s.table_no }} / {{ s.room_no }} · {{ s.status }} ·
                  {{ s.maintaining ? '维护中(' + s.site_count + '站点)' : '正常' }} · {{ s.operator }}
                </div>
              </div>
            </template>
            <template v-else>
              <div class="tr-head">❌ 失败</div>
              <div class="err-msg">{{ testResult.error }}</div>
            </template>
          </div>
        </div>
        <div class="modal-foot">
          <button v-if="canCollect" class="btn btn-secondary" @click="testEnv">测试连接</button>
          <div class="spacer"></div>
          <button class="btn btn-secondary" @click="envDialog = false">取消</button>
          <button class="btn btn-primary" @click="saveEnv" :disabled="envSaving">
            {{ envSaving ? '保存中…' : '保存' }}
          </button>
        </div>
      </div>
    </div>

    <!-- ================= Lark 群弹窗 ================= -->
    <div v-if="botDialog" class="modal-mask" @click.self="botDialog = false">
      <div class="modal">
        <div class="modal-head">
          <h3>{{ botForm.id ? '编辑 Lark 群' : '添加 Lark 群' }}</h3>
          <button class="close" @click="botDialog = false">×</button>
        </div>
        <div class="modal-body">
          <div class="form-row"><label>群名称 *</label><input v-model="botForm.name" placeholder="便于识别，如 G32业务运维群"></div>
          <div class="form-row">
            <label>Webhook {{ botForm.id ? '' : '*' }}</label>
            <input v-model="botForm.webhook" class="wide-input"
                   :placeholder="botForm.id ? '已配置，留空表示不修改' : 'Lark 群机器人的 webhook 地址'">
          </div>
          <div class="form-row">
            <label>签名密钥</label>
            <input v-model="botForm.secret" type="password"
                   :placeholder="botForm.id ? '留空表示不修改' : '群机器人开了签名校验才需要填'">
          </div>
          <div class="form-row"><label>备注</label><input v-model="botForm.description"></div>
          <label class="cb"><input type="checkbox" v-model="botForm.enabled"> 启用</label>
        </div>
        <div class="modal-foot">
          <div class="spacer"></div>
          <button class="btn btn-secondary" @click="botDialog = false">取消</button>
          <button class="btn btn-primary" @click="saveBot">保存</button>
        </div>
      </div>
    </div>

    <!-- ================= 通知人弹窗 ================= -->
    <div v-if="contactDialog" class="modal-mask" @click.self="contactDialog = false">
      <div class="modal">
        <div class="modal-head">
          <h3>{{ contactForm.id ? '编辑通知人' : '添加通知人' }}</h3>
          <button class="close" @click="contactDialog = false">×</button>
        </div>
        <div class="modal-body">
          <div class="form-row"><label>姓名 *</label><input v-model="contactForm.name"></div>
          <div class="form-row"><label>Lark ID</label><input v-model="contactForm.lark_id" class="wide-input" placeholder="open_id 或 user_id，用于艾特"></div>
          <div class="form-row"><label>备注</label><input v-model="contactForm.remark"></div>
        </div>
        <div class="modal-foot">
          <div class="spacer"></div>
          <button class="btn btn-secondary" @click="contactDialog = false">取消</button>
          <button class="btn btn-primary" @click="saveContact">保存</button>
        </div>
      </div>
    </div>

    <!-- ================= 原始响应弹窗 ================= -->
    <div v-if="rawDialog" class="modal-mask" @click.self="rawDialog = false">
      <div class="modal wide">
        <div class="modal-head">
          <h3>原始响应 · {{ rawMeta.env }} · {{ rawMeta.at }}</h3>
          <button class="close" @click="rawDialog = false">×</button>
        </div>
        <div class="modal-body">
          <pre class="raw-box">{{ rawContent }}</pre>
        </div>
        <div class="modal-foot">
          <button class="btn btn-secondary" @click="copyRaw">复制</button>
          <div class="spacer"></div>
          <button class="btn btn-secondary" @click="rawDialog = false">关闭</button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
/* 主题变量用项目统一的那套：--bg-card / --bg-hover / --bg-input / --text-primary /
   --text-secondary / --text-muted / --border-color / --primary。
   容器必须显式声明 color，否则深色主题下会继承出深色文字，在深底上看不见。 */
.ta-page { padding: 20px; color: var(--text-primary); }
.page-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 16px; }
.page-header h2 { margin: 0; color: var(--text-primary); }
.header-actions { display: flex; align-items: center; gap: 12px; }
.auto-refresh { font-size: 13px; color: var(--text-secondary); display: flex; align-items: center; gap: 4px; }

/* 环境条 */
.env-bar {
  display: flex; justify-content: space-between; align-items: center; gap: 16px;
  padding: 12px 16px; margin-bottom: 16px; border-radius: 8px;
  background: var(--bg-card); border: 1px solid var(--border-color); color: var(--text-primary);
}
.env-bar.stale { border-color: var(--danger); background: rgba(239, 68, 68, .08); }
.env-left { display: flex; align-items: center; gap: 8px; }
.env-left .label { font-weight: 600; color: var(--text-primary); }
.env-select {
  padding: 6px 10px; border-radius: 6px; border: 1px solid var(--border-color);
  background: var(--bg-input); color: var(--text-primary);
}
.env-right { font-size: 13px; color: var(--text-secondary); display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.env-right .ok { color: var(--success); }
.env-right .fail { color: var(--danger); }
.stale-warn { color: var(--danger); font-weight: 600; }
.collect-idle { color: var(--text-muted); }
.err-msg { color: var(--danger); word-break: break-all; }

/* Tabs */
.tabs { display: flex; gap: 4px; border-bottom: 2px solid var(--border-color); margin-bottom: 16px; }
.tab {
  padding: 10px 18px; border: none; background: transparent; cursor: pointer;
  font-size: 14px; color: var(--text-secondary); border-bottom: 2px solid transparent; margin-bottom: -2px;
}
.tab:hover { color: var(--text-primary); }
.tab.active { color: var(--primary); border-bottom-color: var(--primary); font-weight: 600; }

/* 统计卡 */
.stat-row { display: flex; gap: 12px; margin-bottom: 16px; flex-wrap: wrap; }
.stat-card {
  flex: 1; min-width: 120px; padding: 16px; border-radius: 8px; text-align: center;
  background: var(--bg-card); border: 1px solid var(--border-color);
}
.stat-card .sc-num { font-size: 26px; font-weight: 700; color: var(--text-primary); }
.stat-card .sc-label { font-size: 12px; color: var(--text-secondary); margin-top: 4px; }
.stat-card.maintain { border-color: var(--warning); } .stat-card.maintain .sc-num { color: var(--warning); }
.stat-card.enable .sc-num { color: var(--success); }
.stat-card.disable .sc-num { color: var(--text-muted); }
.stat-card.alerting { border-color: var(--danger); } .stat-card.alerting .sc-num { color: var(--danger); }

/* 筛选 */
.filter-bar { display: flex; gap: 8px; margin-bottom: 12px; flex-wrap: wrap; align-items: center; }
.filter-bar select, .filter-bar input {
  padding: 6px 10px; border-radius: 6px; border: 1px solid var(--border-color);
  background: var(--bg-input); color: var(--text-primary);
}
.filter-bar input { min-width: 220px; }
.filter-bar input::placeholder { color: var(--text-muted); }
.cb { display: inline-flex; align-items: center; gap: 4px; font-size: 13px; color: var(--text-secondary); }

.hint-line { font-size: 12px; color: var(--text-secondary); margin: 0 0 12px; line-height: 1.7; }
.hint-line code, .field-hint code {
  background: var(--bg-hover); color: var(--text-primary); padding: 1px 5px; border-radius: 3px;
}
.hint-line strong { color: var(--text-primary); }

/* 表格 */
.data-table { width: 100%; border-collapse: collapse; font-size: 13px; color: var(--text-primary); }
.data-table th, .data-table td { padding: 9px 10px; border-bottom: 1px solid var(--border-color); text-align: left; }
.data-table th {
  background: var(--bg-hover); color: var(--text-primary);
  font-weight: 600; white-space: nowrap;
}
.data-table tbody tr:hover { background: var(--bg-hover); }
.data-table.compact th, .data-table.compact td { padding: 6px 8px; }
.data-table .empty { text-align: center; color: var(--text-muted); padding: 28px; }
.row-maintain { background: rgba(245, 158, 11, .1); }
.row-acked { opacity: .6; }
.row-fail { background: rgba(239, 68, 68, .08); }
.mono { font-family: ui-monospace, Menlo, Consolas, monospace; }
.small { font-size: 12px; }
.strong { font-weight: 600; color: var(--text-primary); }
.dim { color: var(--text-muted); }
.dur-long { color: var(--danger); font-weight: 600; }
.acked-label { font-size: 12px; color: var(--success); }
.est-mark {
  display: inline-block; margin-left: 4px; padding: 0 4px; border-radius: 3px;
  font-size: 10px; background: rgba(245, 158, 11, .18); color: var(--warning); cursor: help;
}
.stale-mark { color: var(--text-muted); cursor: help; }

.tag { display: inline-block; padding: 2px 8px; border-radius: 10px; font-size: 12px; white-space: nowrap; }
.tag-enable { background: rgba(16, 185, 129, .15); color: var(--success); }
.tag-disable { background: rgba(113, 128, 150, .2); color: var(--text-secondary); }
.tag-maintain { background: rgba(245, 158, 11, .18); color: var(--warning); font-weight: 600; }
.tag-normal { background: rgba(16, 185, 129, .12); color: var(--success); }
.tag-unknown { background: var(--bg-hover); color: var(--text-muted); }

.btn { padding: 6px 14px; border-radius: 6px; border: 1px solid transparent; cursor: pointer; font-size: 13px; }
.btn-primary { background: var(--primary); color: #fff; }
.btn-primary:hover:not(:disabled) { background: var(--primary-dark); }
.btn-secondary { background: var(--bg-hover); border-color: var(--border-color); color: var(--text-primary); }
.btn:disabled { opacity: .5; cursor: not-allowed; }
.btn-link { background: none; border: none; color: var(--primary); cursor: pointer; font-size: 13px; padding: 0 6px; }
.btn-link.danger { color: var(--danger); }

.pager { display: flex; align-items: center; justify-content: center; gap: 12px; margin-top: 16px; font-size: 13px; color: var(--text-secondary); }
.action-bar { display: flex; align-items: center; gap: 12px; margin-bottom: 12px; }
.action-bar .hint, .hint { font-size: 12px; color: var(--text-secondary); }

/* 规则面板 */
.rule-panel { display: flex; flex-direction: column; gap: 16px; }
.panel {
  padding: 16px; border-radius: 8px; border: 1px solid var(--border-color);
  background: var(--bg-card); color: var(--text-primary);
}
.panel h3 { margin: 0 0 12px; font-size: 15px; color: var(--text-primary); }
.panel .line { display: flex; align-items: center; gap: 8px; margin-bottom: 10px; flex-wrap: wrap; font-size: 13px; color: var(--text-primary); }
.panel .line label { display: inline-flex; align-items: center; gap: 4px; color: var(--text-primary); }
.num, .num-wide, .num-select, .time {
  padding: 4px 8px; border-radius: 4px; border: 1px solid var(--border-color);
  background: var(--bg-input); color: var(--text-primary);
}
.num { width: 70px; }
.num-wide { width: 140px; }
.time { width: 80px; }
.inline-hint { font-size: 12px; color: var(--text-muted); }
.panel-actions { display: flex; justify-content: flex-end; }

.bot-list { display: flex; flex-direction: column; gap: 6px; margin-bottom: 12px; }
.bot-item {
  display: flex; align-items: center; gap: 10px; padding: 8px 12px; border-radius: 6px;
  border: 1px solid var(--border-color); background: var(--bg-input);
  color: var(--text-primary); cursor: pointer; font-size: 13px;
}
.bot-item.checked { border-color: var(--primary); background: var(--primary-bg); }
.bot-name { font-weight: 600; min-width: 140px; color: var(--text-primary); }
.bot-hook { font-size: 11px; color: var(--text-muted); flex: 1; }

.at-block { margin-bottom: 12px; }
.at-title { font-size: 13px; font-weight: 600; margin-bottom: 6px; color: var(--text-primary); }
.chip-list { display: flex; flex-wrap: wrap; gap: 6px; align-items: center; }
.chip {
  display: inline-flex; align-items: center; gap: 4px; padding: 4px 10px; border-radius: 14px;
  border: 1px solid var(--border-color); background: var(--bg-input);
  color: var(--text-primary); font-size: 12px; cursor: pointer;
}
.chip.on { border-color: var(--primary); background: var(--primary-bg); color: var(--primary); }
.chip.disabled { opacity: .45; cursor: not-allowed; }
.chip .no-id { color: var(--danger); font-size: 11px; }

.contact-mgr { margin-top: 16px; padding-top: 12px; border-top: 1px dashed var(--border-color); }
.cm-head { display: flex; justify-content: space-between; align-items: center; margin-bottom: 8px; font-size: 13px; font-weight: 600; color: var(--text-primary); }

.empty-block { padding: 40px; text-align: center; color: var(--text-muted); }
.empty-block.small { padding: 16px; font-size: 13px; }

/* 变化行 */
.change-row td { background: rgba(245, 158, 11, .07); padding: 6px 16px; }
.change-item { font-size: 12px; margin: 2px 0; color: var(--text-secondary); }
.change-item .from { color: var(--text-muted); }
.change-item .to { color: var(--warning); font-weight: 600; }
.err-row td { background: rgba(239, 68, 68, .07); padding: 6px 16px; font-size: 12px; }

/* 弹窗 */
.modal-mask { position: fixed; inset: 0; background: rgba(0, 0, 0, .55); display: flex; align-items: center; justify-content: center; z-index: 1000; }
.modal {
  background: var(--bg-card); color: var(--text-primary); border-radius: 10px;
  width: 560px; max-width: 94vw; max-height: 90vh; display: flex; flex-direction: column;
  border: 1px solid var(--border-color); box-shadow: var(--shadow-lg);
}
.modal.wide { width: 860px; }
.modal-head { display: flex; justify-content: space-between; align-items: center; padding: 16px 20px; border-bottom: 1px solid var(--border-color); }
.modal-head h3 { margin: 0; font-size: 16px; color: var(--text-primary); }
.modal-head .close { background: none; border: none; font-size: 24px; cursor: pointer; color: var(--text-muted); line-height: 1; }
.modal-body { padding: 20px; overflow-y: auto; }
.modal-foot { display: flex; align-items: center; gap: 8px; padding: 14px 20px; border-top: 1px solid var(--border-color); }
.modal-foot .spacer { flex: 1; }

.form-row { display: flex; align-items: center; gap: 8px; margin-bottom: 10px; flex-wrap: wrap; font-size: 13px; color: var(--text-primary); }
.form-row > label { min-width: 76px; color: var(--text-secondary); }
.form-row label.cb { min-width: auto; color: var(--text-primary); }
.form-row input, .form-row select, .form-row textarea {
  padding: 6px 10px; border-radius: 6px; border: 1px solid var(--border-color);
  background: var(--bg-input); color: var(--text-primary); font-size: 13px;
}
.form-row input::placeholder, .form-row textarea::placeholder { color: var(--text-muted); }
.form-row input { width: 180px; }
.form-row .wide-input { width: 100%; max-width: 560px; }
.form-row textarea { width: 100%; max-width: 560px; font-family: ui-monospace, Menlo, monospace; }
fieldset { border: 1px solid var(--border-color); border-radius: 8px; padding: 14px 16px; margin-bottom: 14px; }
legend { font-size: 13px; font-weight: 600; padding: 0 6px; color: var(--text-primary); }
.field-hint { font-size: 12px; color: var(--text-muted); margin: 4px 0 10px; line-height: 1.6; }
.radio-line { display: flex; align-items: center; gap: 6px; font-size: 13px; margin-bottom: 8px; color: var(--text-primary); }

.test-result { margin-top: 14px; padding: 12px 14px; border-radius: 8px; font-size: 13px; color: var(--text-primary); }
.test-result.ok { background: rgba(16, 185, 129, .1); border: 1px solid rgba(16, 185, 129, .35); }
.test-result.fail { background: rgba(239, 68, 68, .08); border: 1px solid rgba(239, 68, 68, .35); }
.tr-head { font-weight: 600; margin-bottom: 6px; }
.sample { margin-top: 8px; }
.sample-title { font-size: 12px; color: var(--text-secondary); margin-bottom: 4px; }

.raw-box {
  background: #0d1117; color: #c9d1d9; padding: 14px; border-radius: 6px;
  border: 1px solid var(--border-color);
  font-family: ui-monospace, Menlo, Consolas, monospace; font-size: 12px;
  max-height: 60vh; overflow: auto; white-space: pre-wrap; word-break: break-all; margin: 0;
}
</style>
