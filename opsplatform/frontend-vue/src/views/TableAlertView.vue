<script setup>
import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
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
// 这四件事以前都借用 rule_update，现在各自独立授权
const canWindowManage  = computed(() => can('table_alert:window_manage'))
const canSiteManage    = computed(() => can('table_alert:site_manage'))
const canInService     = computed(() => can('table_alert:in_service'))
const canOfflineConfirm = computed(() => can('table_alert:offline_confirm'))
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
// in_service 默认 '1'：页签默认停在「在用」，筛选条件必须跟页签一致，
// 否则首屏列的是全部桌台、页签却高亮在「在用」上，对不上。
const filters = ref({ status: '', maintaining: '', q: '', routine: '', in_service: '1' })
const loadingRooms = ref(false)
const roomJump = ref(1)
const roomPages = computed(() => Math.max(1, Math.ceil(roomsTotal.value / roomSize.value)))

// 跳页输入框只跟随「当前页」变化，不跟随每次加载。
// 之前写在 loadRooms() 里无条件同步，结果 30s 自动刷新会把用户正在输入的页码冲掉。
watch(roomPage, v => { roomJump.value = v })

// 跳页：越界直接夹到合法范围，不弹错误打断操作
function gotoRoomPage() {
  const n = Math.min(Math.max(1, Number(roomJump.value) || 1), roomPages.value)
  roomJump.value = n
  roomPage.value = n
  loadRooms()
}

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
      ...(filters.value.q ? { q: filters.value.q } : {}),
      ...(filters.value.in_service !== '' ? { in_service: filters.value.in_service } : {})
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

// 例行维护的归属是在服务端内存里算的，没进 SQL，
// 所以这一项在当前页内过滤，不参与分页统计
const displayRooms = computed(() => {
  if (filters.value.routine === '') return rooms.value
  const want = filters.value.routine === '1'
  return rooms.value.filter(r => (!!(r.routine_windows && r.routine_windows.length)) === want)
})

// 在用标记：系统分不清一张停用的桌台是刚被误停还是压根没上线，
// 这个信息只有人知道。标了在用，维护和停用都算不可用、都告警。
// 桌台列表页签：默认停在「在用」—— 要盯的就是这一组，非在用的随时能翻。
// 它只是 filters.in_service 的一层外壳，翻页签＝换筛选条件，后端不用改。
const roomTab = ref('in')

function switchRoomTab(t) {
  roomTab.value = t
  filters.value.in_service = t === 'in' ? '1' : (t === 'off' ? '0' : '')
  applyFilter()
}

// ===== 批量确认在用清单 =====
// 首次接入时几十上百台全是按启停自动猜的，逐台勾选要翻好几页，实际没人会去点。
// 这里按「范围」整组确认，不依赖前端勾了哪些行 —— 只置「已人工确认」，不改在用标记本身。
const confirmOpen = ref(false)
const confirming = ref(false)

async function confirmList(scope) {
  confirming.value = true
  try {
    const res = await api.post('/api/table-alert/rooms/confirm', {
      env_id: currentEnvId.value, scope
    })
    appStore.showToast(`已确认 ${res.data?.count || 0} 台（${res.data?.scope || ''}）`, 'success')
    await loadRooms()
    if ((stats.value.unconfirmed ?? 0) === 0) confirmOpen.value = false
  } catch (e) {
    appStore.showToast('确认失败: ' + errText(e), 'error')
  } finally {
    confirming.value = false
  }
}

// ===== 待复核 =====
// 非在用、维护挂了很久、又没人确认过确实是下线的。
// 防的是和「在用桌台被误停」相反的方向：本该在用的被标成非在用，一直没人发现。
const reviewOpen = ref(false)
const reviewList = ref([])
const reviewDays = ref(3)
const reviewSel = ref([])

async function openReview() {
  reviewOpen.value = true
  reviewSel.value = []
  try {
    const res = await api.get('/api/table-alert/review', { params: { env_id: currentEnvId.value } })
    reviewList.value = res.data?.items || []
    reviewDays.value = res.data?.review_days || 3
  } catch (e) {
    appStore.showToast('加载待复核失败: ' + errText(e), 'error')
  }
}

// 确认下线：和「标为非在用」是两件事 —— 非在用只是不告警，
// 确认下线是明确说「这是有意为之，别再提醒我」，确认完就不再进复核列表。
async function confirmOffline(ids) {
  if (!ids.length) return
  try {
    const res = await api.post('/api/table-alert/rooms/offline-confirm', {
      env_id: currentEnvId.value, room_ids: ids, confirm: true
    })
    appStore.showToast(`已确认 ${res.data?.count || 0} 台下线`, 'success')
    await openReview()
    loadRooms()
  } catch (e) {
    appStore.showToast('操作失败: ' + errText(e), 'error')
  }
}

// 从复核列表直接捞回在用 —— 这才是复核真正想抓的那种情况
async function reviewMarkInService(ids) {
  if (!ids.length) return
  try {
    const res = await api.post('/api/table-alert/rooms/in-service', {
      env_id: currentEnvId.value, room_ids: ids, in_service: true
    })
    appStore.showToast(`已将 ${res.data?.count || 0} 台标为在用，下一轮采集起开始告警`, 'success')
    await openReview()
    loadRooms()
  } catch (e) {
    appStore.showToast('操作失败: ' + errText(e), 'error')
  }
}

// 采集间隔可以直接填任意秒数，这几个只是常用值的快捷入口
const intervalPresets = [
  { v: 30, t: '30秒' }, { v: 60, t: '1分钟' }, { v: 120, t: '2分钟' }, { v: 300, t: '5分钟' },
]

const roomSelection = ref([])
const allRoomsChecked = computed(() =>
  displayRooms.value.length > 0 && roomSelection.value.length === displayRooms.value.length)

function toggleAllRooms(e) {
  roomSelection.value = e.target.checked ? displayRooms.value.map(r => r.room_id) : []
}

async function toggleInService(r) {
  try {
    await api.post('/api/table-alert/rooms/in-service', {
      env_id: currentEnvId.value, room_ids: [r.room_id], in_service: !r.in_service
    })
    r.in_service = !r.in_service
    r.in_service_manual = true
    loadRooms()
  } catch (e) {
    appStore.showToast('保存失败: ' + errText(e), 'error')
  }
}

async function batchInService(v) {
  try {
    const res = await api.post('/api/table-alert/rooms/in-service', {
      env_id: currentEnvId.value, room_ids: roomSelection.value, in_service: v
    })
    appStore.showToast(`已将 ${res.data?.count || 0} 个桌台标为${v ? '在用' : '非在用'}`, 'success')
    roomSelection.value = []
    loadRooms()
  } catch (e) {
    appStore.showToast('操作失败: ' + errText(e), 'error')
  }
}

function applyFilter() { roomPage.value = 1; roomSelection.value = []; loadRooms() }
function resetFilter() {
  // 重置回当前页签的口径，而不是一律清空 —— 在「在用」页签上点重置却列出全部，是反直觉的
  const keep = roomTab.value === 'in' ? '1' : (roomTab.value === 'off' ? '0' : '')
  filters.value = { status: '', maintaining: '', q: '', routine: '', in_service: keep }
  applyFilter()
}

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

// ===================== 站点 =====================
const sites = ref([])
const siteStats = ref({ total: 0, named: 0, watched: 0 })
const siteFilter = ref({ watched: '', named: '', q: '' })
const siteSelection = ref([])
const siteDialog = ref(false)
const siteForm = ref({ id: '', site_id: '', site_name: '', watched: false, remark: '' })
const roomSitesDialog = ref(false)
const roomSites = ref({ table_no: '', watched: [], others: [], total: 0 })

const allSitesChecked = computed(() => sites.value.length > 0 && siteSelection.value.length === sites.value.length)

async function loadSites() {
  if (!currentEnvId.value) { sites.value = []; return }
  try {
    const res = await api.get('/api/table-alert/sites', {
      params: {
        env_id: currentEnvId.value,
        ...(siteFilter.value.watched ? { watched: siteFilter.value.watched } : {}),
        ...(siteFilter.value.named !== '' ? { named: siteFilter.value.named } : {}),
        ...(siteFilter.value.q ? { q: siteFilter.value.q } : {})
      }
    })
    sites.value = res.data?.items || []
    siteStats.value = res.data?.stats || siteStats.value
    siteSelection.value = []
  } catch (e) {
    appStore.showToast('读取站点失败: ' + errText(e), 'error')
  }
}

function toggleAllSites(e) {
  siteSelection.value = e.target.checked ? sites.value.map(x => x.id) : []
}

async function toggleWatch(st) {
  try {
    await api.put(`/api/table-alert/sites/${st.id}`, {
      site_name: st.site_name, watched: !st.watched, remark: st.remark
    })
    st.watched = !st.watched
    siteStats.value.watched += st.watched ? 1 : -1
    loadRooms()   // 关注变了，桌台列表的影响站点跟着变
  } catch (e) {
    appStore.showToast('保存失败: ' + errText(e), 'error')
  }
}

async function batchWatch(watched) {
  try {
    const res = await api.post('/api/table-alert/sites/watch', { ids: siteSelection.value, watched })
    appStore.showToast(`已${watched ? '关注' : '取消关注'} ${res.data?.count || 0} 个站点`, 'success')
    loadSites(); loadRooms()
  } catch (e) {
    appStore.showToast('操作失败: ' + errText(e), 'error')
  }
}

// 手动录入站点：开发只开放了 game 入口，站点列表接口不便去调，
// 所以人工录入是主路径，采集自动发现做兜底。
const addSitesDialog = ref(false)
const addSitesSaving = ref(false)
const addMode = ref('batch')
const addSitesRaw = ref('')
const addSitesWatched = ref(false)
const addSitesResult = ref(null)
const addSiteForm = ref({ site_id: '', site_name: '', watched: false, remark: '' })

function openAddSites() {
  addMode.value = 'batch'
  addSitesRaw.value = ''
  addSitesWatched.value = false
  addSitesResult.value = null
  addSiteForm.value = { site_id: '', site_name: '', watched: false, remark: '' }
  addSitesDialog.value = true
}

async function submitAddSites() {
  const payload = { env_id: currentEnvId.value }
  if (addMode.value === 'batch') {
    if (!addSitesRaw.value.trim()) { appStore.showToast('请粘贴站点列表', 'error'); return }
    payload.raw = addSitesRaw.value
    payload.watched = addSitesWatched.value
  } else {
    if (!addSiteForm.value.site_id.trim()) { appStore.showToast('siteId 不能为空', 'error'); return }
    Object.assign(payload, addSiteForm.value)
  }
  addSitesSaving.value = true
  try {
    const res = await api.post('/api/table-alert/sites', payload)
    addSitesResult.value = res.data
    addSitesRaw.value = ''
    addSiteForm.value = { site_id: '', site_name: '', watched: false, remark: '' }
    loadSites(); loadRooms()
  } catch (e) {
    appStore.showToast('导入失败: ' + errText(e), 'error')
  } finally {
    addSitesSaving.value = false
  }
}

function openEditSite(st) {
  siteForm.value = { ...st }
  siteDialog.value = true
}

async function saveSite() {
  try {
    await api.put(`/api/table-alert/sites/${siteForm.value.id}`, {
      site_name: siteForm.value.site_name,
      watched: siteForm.value.watched,
      remark: siteForm.value.remark
    })
    appStore.showToast('已保存', 'success')
    siteDialog.value = false
    loadSites(); loadRooms()
  } catch (e) {
    appStore.showToast('保存失败: ' + errText(e), 'error')
  }
}

// 桌台详情：列出全部站点，关注的在前
async function openRoomSites(r) {
  try {
    const res = await api.get(`/api/table-alert/rooms/${r.room_id}/sites`, {
      params: { env_id: currentEnvId.value }
    })
    roomSites.value = {
      table_no: res.data?.table_no || r.table_no,
      watched: res.data?.watched || [],
      others: res.data?.others || [],
      total: res.data?.total || 0
    }
    roomSitesDialog.value = true
  } catch (e) {
    appStore.showToast('读取站点失败: ' + errText(e), 'error')
  }
}

// 列表里只显示前几个，剩下的收成「等 N 个」，避免一行被站点名撞爆
function joinSites(names, max = 3) {
  if (!names || !names.length) return ''
  if (names.length <= max) return names.join('\u3001')
  return names.slice(0, max).join('\u3001') + ` \u7b49 ${names.length} \u4e2a`
}

// ===================== 例行维护窗口 =====================
const windows = ref([])
const windowDialog = ref(false)
const windowSaving = ref(false)
const windowForm = ref(blankWindow())
const windowTableMode = ref('list')
const windowTableInput = ref('')
const weekdayOptions = [
  { v: 1, label: '周一' }, { v: 2, label: '周二' }, { v: 3, label: '周三' },
  { v: 4, label: '周四' }, { v: 5, label: '周五' }, { v: 6, label: '周六' }, { v: 7, label: '周日' }
]

// 当前维护中的桌台，配置时可以直接点选，省去手敲桌台号

function blankWindow() {
  return {
    id: '', env_id: '', name: '', enabled: true,
    repeat_type: 'daily', weekdays: '', month_days: '', once_date: '',
    start_time: '02:00', end_time: '04:00', table_nos: '',
    action: 'annotate', overrun_alert: true, remark: ''
  }
}

async function loadWindows() {
  if (!currentEnvId.value) { windows.value = []; return }
  try {
    const res = await api.get('/api/table-alert/windows', { params: { env_id: currentEnvId.value } })
    windows.value = res.data || []
  } catch (e) {
    appStore.showToast('读取例行维护失败: ' + errText(e), 'error')
  }
}

function openCreateWindow() {
  windowForm.value = blankWindow()
  windowTableMode.value = 'list'
  windowTableInput.value = ''
  roomPickerQuery.value = ''
  loadAllRooms()
  windowDialog.value = true
}

function openEditWindow(w) {
  windowForm.value = { ...blankWindow(), ...w }
  windowTableMode.value = w.table_nos === '*' ? 'all' : 'list'
  windowTableInput.value = w.table_nos === '*' ? '' : (w.table_nos || '')
  roomPickerQuery.value = ''
  loadAllRooms()
  windowDialog.value = true
}

// 窗口配置用的房间选择器：列出该环境所有启用的房间，搜索勾选，
// 不用手敲房间号 —— 手敲容易敲错，错了窗口永远不会命中，而且很难发现。
const allRooms = ref([])
const roomPickerQuery = ref('')

const pickedRooms = computed(() =>
  windowTableInput.value.split(',').map(x => x.trim()).filter(Boolean))

const filteredPickerRooms = computed(() => {
  const kw = roomPickerQuery.value.trim().toLowerCase()
  if (!kw) return allRooms.value
  return allRooms.value.filter(r =>
    (r.room_no || '').toLowerCase().includes(kw) ||
    (r.table_no || '').toLowerCase().includes(kw))
})

async function loadAllRooms() {
  if (!currentEnvId.value) { allRooms.value = []; return }
  try {
    // 只要启用的：停用的房间配了也不会维护，列出来只会干扰
    const res = await api.get('/api/table-alert/rooms', {
      params: { env_id: currentEnvId.value, status: 'Enable', page: 1, size: 500 }
    })
    allRooms.value = res.data?.items || []
  } catch (e) {
    appStore.showToast('读取房间列表失败: ' + errText(e), 'error')
  }
}

function toggleRoomPick(roomNo) {
  const arr = [...pickedRooms.value]
  const i = arr.indexOf(roomNo)
  if (i >= 0) arr.splice(i, 1); else arr.push(roomNo)
  windowTableInput.value = arr.join(',')
}

function clearPickedRooms() { windowTableInput.value = '' }

function hasWeekday(v) {
  return (windowForm.value.weekdays || '').split(',').map(x => x.trim()).includes(String(v))
}
function toggleWeekday(v) {
  const arr = (windowForm.value.weekdays || '').split(',').map(x => x.trim()).filter(Boolean)
  const i = arr.indexOf(String(v))
  if (i >= 0) arr.splice(i, 1); else arr.push(String(v))
  arr.sort()
  windowForm.value.weekdays = arr.join(',')
}
async function saveWindow() {
  const f = windowForm.value
  if (!f.name?.trim()) { appStore.showToast('名称不能为空', 'error'); return }
  f.env_id = currentEnvId.value
  f.table_nos = windowTableMode.value === 'all' ? '*' : windowTableInput.value.trim()
  if (!f.table_nos) { appStore.showToast('请指定适用的房间', 'error'); return }
  windowSaving.value = true
  try {
    await api.post('/api/table-alert/windows', f)
    appStore.showToast('已保存', 'success')
    windowDialog.value = false
    loadWindows()
  } catch (e) {
    appStore.showToast('保存失败: ' + errText(e), 'error')
  } finally {
    windowSaving.value = false
  }
}

async function deleteWindow(w) {
  const ok = await appStore.showConfirm({
    type: 'danger', title: `删除例行维护「${w.name}」`,
    message: '删除后，这些桌台在该时段的维护会被当作计划外维护正常告警。',
    okText: '确定删除', cancelText: '取消'
  })
  if (!ok) return
  try { await api.delete(`/api/table-alert/windows/${w.id}`); loadWindows() }
  catch (e) { appStore.showToast('删除失败: ' + errText(e), 'error') }
}

// ===================== 采集日志 =====================
const logs = ref([])
const logsTotal = ref(0)
const logPage = ref(1)
const logSize = ref(20)
const logJump = ref(1)
const logPages = computed(() => Math.max(1, Math.ceil(logsTotal.value / logSize.value)))
const logFilter = ref({ ok: '', has_change: '' })

watch(logPage, v => { logJump.value = v })

function gotoLogPage() {
  const n = Math.min(Math.max(1, Number(logJump.value) || 1), logPages.value)
  logJump.value = n
  logPage.value = n
  loadLogs()
}
const rawDialog = ref(false)
const rawContent = ref('')
const rawMeta = ref({})

async function loadLogs() {
  if (!currentEnvId.value) { logs.value = []; return }
  try {
    const res = await api.get('/api/table-alert/collect-logs', {
      params: {
        env_id: currentEnvId.value, page: logPage.value, size: logSize.value,
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
  // 在用桌台被停用，往往是误操作（想点维护点成了停用），比维护更该立刻看
  if (r.in_service && r.status !== 'Enable') {
    return {
      text: r.maintaining ? '⛔ 停用+维护' : '⛔ 已停用',
      cls: 'tag-overrun',
      tip: '该桌台标记为在用，却处于停用状态 —— 请确认是否误操作'
    }
  }
  if (!r.maintaining) return { text: '正常', cls: 'tag-normal', tip: '' }
  // 例行维护和计划外维护要一眼分得开，否则例行保养会把人练到对告警无感
  if (r.window_name && r.window_overrun) {
    return {
      text: '⏰ 例行超时', cls: 'tag-overrun',
      tip: `例行维护「${r.window_name}」计划 ${r.window_end_at} 结束，已超时 ${r.window_overrun_text}`
    }
  }
  if (r.window_name) {
    return {
      text: '🗓 例行维护', cls: 'tag-routine',
      tip: `例行维护「${r.window_name}」，计划 ${r.window_end_at} 结束`
    }
  }
  return { text: '🔧 维护中', cls: 'tag-maintain', tip: '计划外维护' }
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
  await Promise.all([loadRooms(), loadRule(), loadLogs(), loadWindows(), loadSites()])
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
      <button class="tab" :class="{ active: activeTab === 'windows' }" @click="activeTab = 'windows'; loadWindows()">🗓 例行维护</button>
      <button class="tab" :class="{ active: activeTab === 'sites' }" @click="activeTab = 'sites'; loadSites()">🏢 站点管理</button>
      <button class="tab" :class="{ active: activeTab === 'logs' }" @click="activeTab = 'logs'; loadLogs()">📋 采集日志</button>
    </div>

    <!-- ================= Tab 桌台列表 ================= -->
    <div v-if="activeTab === 'rooms'" class="tab-content">
      <!-- 在用清单没人确认过的话，一直挂着提示。
           在用标记是按启停自动猜的，猜错了就会漏报，这事必须让人看见。 -->
      <div v-if="(stats.unconfirmed ?? 0) > 0" class="banner warn">
        <span>
          ⚠️ <b>在用清单未确认</b> —— {{ stats.unconfirmed }} 台的在用状态是系统按启停自动推断的，还没人工确认过。
          推断错了的桌台不会告警。
        </span>
        <button v-if="canInService" class="btn btn-primary" @click="confirmOpen = true">批量确认</button>
        <button class="btn btn-secondary" @click="switchRoomTab('off')">先看看非在用的</button>
      </div>

      <!-- 非在用里挂着长期维护的，提醒复核一次。防的是「本该在用却被标成非在用、一直没人发现」 -->
      <div v-if="(stats.review ?? 0) > 0" class="banner info">
        <span>
          🔎 <b>{{ stats.review }} 台</b>非在用桌台已维护超过 {{ stats.review_days ?? 3 }} 天，建议复核一次：
          是真的下线了，还是本该在用？
        </span>
        <button class="btn btn-secondary" @click="openReview()">查看待复核</button>
      </div>

      <!-- 统计按「在用 / 非在用」分成两组。
           只报一个「不可用（在用）」会把非在用那侧维护中的桌台吞掉，看的人以为数字漏了。 -->
      <div class="stat-row">
        <div class="stat-card"><div class="sc-num">{{ stats.total }}</div><div class="sc-label">总桌台</div></div>

        <div class="stat-group primary" title="标记为在用的桌台 —— 这是唯一会告警的范围">
          <div class="grp-title">在用 {{ stats.in_service ?? 0 }} 台 · 要盯的</div>
          <div class="grp-body">
            <div class="grp-main" :class="{ bad: (stats.unavailable ?? 0) > 0 }">
              <div class="sc-num">{{ stats.unavailable ?? 0 }}</div>
              <div class="sc-label">⚠️ 不可用</div>
            </div>
            <div class="grp-split">
              <div class="grp-item">维护中 <b>{{ stats.unavail_maintain ?? 0 }}</b></div>
              <div class="grp-item" :class="{ bad: (stats.unavail_disabled ?? 0) > 0 }"
                   title="在用却被停用 —— 多半是本该点维护，点成了停用">
                被停用 <b>{{ stats.unavail_disabled ?? 0 }}</b> ⛔
              </div>
            </div>
          </div>
        </div>

        <div class="stat-group muted" title="没标在用的桌台，怎么折腾都不告警，仅供复核">
          <div class="grp-title">非在用 {{ stats.off_service ?? 0 }} 台 · 不告警</div>
          <div class="grp-body">
            <div class="grp-main">
              <div class="sc-num">{{ stats.off_maintaining ?? 0 }}</div>
              <div class="sc-label">维护中</div>
            </div>
            <div class="grp-split">
              <div class="grp-item" :class="{ bad: (stats.review ?? 0) > 0 }">
                超 {{ stats.review_days ?? 3 }} 天 <b>{{ stats.review ?? 0 }}</b>
              </div>
              <div class="grp-item">这些不告警</div>
            </div>
          </div>
        </div>

        <div class="stat-card alerting"><div class="sc-num">{{ stats.alerting }}</div><div class="sc-label">告警中</div></div>
      </div>

      <!-- 页签：默认停在「在用」，非在用随时能翻 -->
      <div class="room-tabs">
        <button class="rt" :class="{ on: roomTab === 'in' }" @click="switchRoomTab('in')">
          在用 ({{ stats.in_service ?? 0 }})
        </button>
        <button class="rt" :class="{ on: roomTab === 'off' }" @click="switchRoomTab('off')">
          非在用 ({{ stats.off_service ?? 0 }})
        </button>
        <button class="rt" :class="{ on: roomTab === 'all' }" @click="switchRoomTab('all')">
          全部 ({{ stats.total }})
        </button>
      </div>

      <div v-if="roomTab === 'off'" class="banner info sm">
        这 {{ stats.off_service ?? 0 }} 台不会告警。其中 {{ stats.off_maintaining ?? 0 }} 台正在维护、{{ stats.review ?? 0 }} 台已超过 {{ stats.review_days ?? 3 }} 天。
        如果有本该对外服务的桌台在这里，勾选后「标为在用」。
      </div>

      <div class="filter-bar">
        <select v-model="filters.maintaining" @change="applyFilter">
          <option value="">站点状态：全部</option>
          <option value="1">仅维护中</option>
          <option value="0">仅正常</option>
        </select>
        <select v-model="filters.routine" @change="applyFilter">
          <option value="">例行维护：全部</option>
          <option value="1">已配例行维护</option>
          <option value="0">未配例行维护</option>
        </select>
        <select v-model="filters.status" @change="applyFilter">
          <option value="">状态：全部</option>
          <option value="Enable">Enable（启用）</option>
          <option value="Disable">Disable（停用）</option>
        </select>
        <input v-model="filters.q" placeholder="桌台号 / 房间号" @keyup.enter="applyFilter">
        <button class="btn btn-primary" @click="applyFilter">搜索</button>
        <button class="btn btn-secondary" @click="resetFilter">重置</button>
        <template v-if="canInService && roomSelection.length">
          <button class="btn btn-secondary" @click="batchInService(true)">✔ 标为在用 ({{ roomSelection.length }})</button>
          <button class="btn btn-secondary" @click="batchInService(false)">标为非在用</button>
        </template>
      </div>

      <p class="hint-line">
        「状态」和「站点状态」是两件事：<code>status</code> 是桌台启用/停用，
        <code>gameRoomMaintainList</code> 非空才是站点侧维护中 —— <strong>只有维护中才会告警</strong>。
        维护时长带「估」的表示系统首次采集时它已在维护，开始时间由接口 <code>updateTime</code> 回溯，真实时间可能更早。
      </p>

      <table class="data-table">
        <thead>
          <tr>
            <th style="width:34px"><input type="checkbox" :checked="allRoomsChecked" @change="toggleAllRooms"></th>
            <th title="人工标记：在用的桌台不论维护还是停用都会告警；非在用的怎么折腾都不打扰">在用</th>
            <th>桌台</th><th>房间号</th>
            <th title="对应中台后台的「状态」：桌台启用 / 停用">状态</th>
            <th title="对应中台后台的「站点状态」：该桌台在站点侧是否处于维护">站点状态</th>
            <th>影响站点</th>
            <th title="带「估」字的是回溯估算：系统首次采集时该桌台已在维护，没有观测到跃迁">维护时长</th>
            <th title="该房间配了哪些例行保养安排（不是此刻是否在维护）">例行维护</th>
            <th>告警</th><th>操作人</th><th>操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="loadingRooms"><td colspan="12" class="empty">加载中…</td></tr>
          <tr v-else-if="!displayRooms.length"><td colspan="12" class="empty">
            暂无数据 —— 如果这个环境刚配好，点右上角「立即采集」拉一次
          </td></tr>
          <tr v-for="r in displayRooms" :key="r.room_id" :class="{ 'row-maintain': r.maintaining, 'row-acked': r.event_state === 'acked' }">
            <td><input type="checkbox" :value="r.room_id" v-model="roomSelection"></td>
            <td>
              <button class="btn-link svc-toggle" :class="{ on: r.in_service }"
                      :disabled="!canInService"
                      :title="(r.in_service ? '在用 —— 维护或停用都会告警' : '非在用 —— 不告警') + (r.in_service_manual ? '（人工标记）' : '（按启停自动初始化）')"
                      @click="toggleInService(r)">
                {{ r.in_service ? '✔' : '—' }}
              </button>
            </td>
            <td class="mono strong">{{ r.table_no }}</td>
            <td class="mono">{{ r.room_no }}</td>
            <td><span class="tag" :class="statusBadge(r.status).cls">{{ statusBadge(r.status).text }}</span></td>
            <td>
              <span class="tag" :class="maintainBadge(r).cls" :title="maintainBadge(r).tip">{{ maintainBadge(r).text }}</span>
            </td>
              <td>
                <template v-if="!r.maintaining">—</template>
                <template v-else-if="r.watched_site_count">
                  <button class="btn-link site-link" @click="openRoomSites(r)"
                          :title="'共 ' + r.maintain_site_count + ' 个站点受影响，点击查看全部'">
                    {{ joinSites(r.watched_sites) }}
                  </button>
                </template>
                <template v-else>
                  <button class="btn-link dim site-link" @click="openRoomSites(r)"
                          :title="'共 ' + r.maintain_site_count + ' 个站点受影响，但没有一个是关注的；点击查看全部'">
                    无关注站点
                  </button>
                </template>
              </td>
            <td :class="{ 'dur-long': r.duration_min >= 60 }">
              <template v-if="r.duration_text">
                <span :title="durationTip(r)">{{ r.since_estimated ? '≈' : '' }}{{ r.duration_text }}</span>
                <span v-if="r.since_estimated" class="est-mark" :title="durationTip(r)">估</span>
              </template>
              <template v-else>—</template>
            </td>
            <td>
              <template v-if="r.routine_windows && r.routine_windows.length">
                <span v-for="(w, i) in r.routine_windows" :key="i" class="tag tag-routine routine-chip"
                      :title="w.name + '：' + w.rule_text">{{ w.rule_text }}</span>
              </template>
              <span v-else class="dim">无</span>
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

      <div class="pager" v-if="roomsTotal">
        <span class="pg-total">共 {{ roomsTotal }} 条</span>
        <select v-model.number="roomSize" @change="roomPage = 1; loadRooms()" class="pg-size">
          <option :value="10">10 条/页</option>
          <option :value="20">20 条/页</option>
          <option :value="50">50 条/页</option>
          <option :value="100">100 条/页</option>
        </select>
        <button class="btn btn-secondary" :disabled="roomPage <= 1" @click="roomPage--; loadRooms()">上一页</button>
        <span class="pg-cur">第 {{ roomPage }} / {{ roomPages }} 页</span>
        <button class="btn btn-secondary" :disabled="roomPage >= roomPages" @click="roomPage++; loadRooms()">下一页</button>
        <span class="pg-jump">
          跳至
          <input type="number" min="1" :max="roomPages" v-model.number="roomJump"
                 @keyup.enter="gotoRoomPage" class="pg-input">
          页
          <button class="btn btn-secondary" @click="gotoRoomPage">确定</button>
        </span>
      </div>
    </div>

    <!-- ================= 批量确认在用清单 ================= -->
    <div v-if="confirmOpen" class="modal-mask" @click.self="confirmOpen = false">
      <div class="modal">
        <div class="modal-head">
          <h3>确认在用清单</h3>
          <button class="close" @click="confirmOpen = false">×</button>
        </div>
        <div class="modal-body">
          <p class="field-hint">
            系统按启停自动推断了在用状态：启用的当作「在用」，停用的当作「非在用」。
            确认＝认可这个推断，<b>不会改动任何桌台的标记</b>；确认之后采集也不会再自动调整它们。
          </p>

          <div class="cfm-row">
            <div class="cfm-info">
              <b>在用 {{ stats.unconfirmed_in ?? 0 }} 台</b>
              <span class="dim">推断自「启用」—— 这些会告警</span>
            </div>
            <button class="btn btn-primary" :disabled="!(stats.unconfirmed_in > 0) || confirming"
                    @click="confirmList('in')">确认这 {{ stats.unconfirmed_in ?? 0 }} 台</button>
          </div>

          <div class="cfm-row">
            <div class="cfm-info">
              <b>非在用 {{ stats.unconfirmed_off ?? 0 }} 台</b>
              <span class="dim">推断自「停用」—— 这些<b>不会告警</b></span>
              <span v-if="(stats.off_maintaining ?? 0) > 0" class="warn-line">
                ⚠️ 其中 {{ stats.off_maintaining }} 台正在维护。确认前最好先看一眼：
                如果有本该对外服务的桌台在里面，它被确认成非在用之后就永远不会告警了。
              </span>
            </div>
            <div class="cfm-btns">
              <button class="btn btn-secondary" @click="confirmOpen = false; switchRoomTab('off')">先去看看</button>
              <button class="btn btn-primary" :disabled="!(stats.unconfirmed_off > 0) || confirming"
                      @click="confirmList('off')">确认这 {{ stats.unconfirmed_off ?? 0 }} 台</button>
            </div>
          </div>

          <div class="cfm-row all">
            <div class="cfm-info"><b>全部 {{ stats.unconfirmed ?? 0 }} 台</b><span class="dim">两组一起确认</span></div>
            <button class="btn btn-secondary" :disabled="!(stats.unconfirmed > 0) || confirming"
                    @click="confirmList('all')">全部确认</button>
          </div>
        </div>
        <div class="modal-foot">
          <button class="btn btn-secondary" @click="confirmOpen = false">关闭</button>
        </div>
      </div>
    </div>

    <!-- ================= 待复核弹窗 ================= -->
    <div v-if="reviewOpen" class="modal-mask" @click.self="reviewOpen = false">
      <div class="modal wide">
        <div class="modal-head">
          <h3>待复核 —— 非在用但长期维护</h3>
          <button class="close" @click="reviewOpen = false">×</button>
        </div>
        <div class="modal-body">
          <p class="field-hint">
            这些桌台没标在用（所以不告警），但维护已经挂了超过 {{ reviewDays }} 天。
            要么它真的下线了 —— 点「确认下线」，以后不再提醒；
            要么它本该对外服务 —— 点「标为在用」，下一轮起开始告警。
          </p>
          <div v-if="!reviewList.length" class="empty-block">没有待复核的桌台</div>
          <template v-else>
            <div class="action-bar" v-if="reviewSel.length">
              已选 {{ reviewSel.length }} 台
              <button class="btn btn-primary" @click="reviewMarkInService(reviewSel)">✔ 标为在用</button>
              <button class="btn btn-secondary" @click="confirmOffline(reviewSel)">确认下线</button>
            </div>
            <table class="data-table compact">
              <thead>
                <tr>
                  <th style="width:36px">
                    <input type="checkbox"
                           :checked="reviewSel.length === reviewList.length && reviewList.length > 0"
                           @change="reviewSel = $event.target.checked ? reviewList.map(x => x.room_id) : []">
                  </th>
                  <th>桌台</th><th>房间号</th><th>状态</th><th>已维护</th><th>最后操作人</th><th>操作</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="x in reviewList" :key="x.room_id">
                  <td><input type="checkbox" :value="x.room_id" v-model="reviewSel"></td>
                  <td><b>{{ x.table_no }}</b></td>
                  <td>{{ x.room_no }}</td>
                  <td><span class="tag" :class="x.status === 'Enable' ? 'tag-enable' : 'tag-disable'">{{ x.status }}</span></td>
                  <td class="dur-long">
                    {{ x.duration_text }}
                    <span v-if="x.since_estimated" class="est-mark" title="系统首次采集时它已在维护，开始时间由接口 updateTime 回溯">估</span>
                  </td>
                  <td class="dim">{{ x.operator || '—' }}</td>
                  <td>
                    <button class="btn-link" @click="reviewMarkInService([x.room_id])">标为在用</button>
                    <button class="btn-link" @click="confirmOffline([x.room_id])">确认下线</button>
                  </td>
                </tr>
              </tbody>
            </table>
          </template>
        </div>
        <div class="modal-foot">
          <button class="btn btn-secondary" @click="reviewOpen = false">关闭</button>
        </div>
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
          <h3>告警范围（按桌台启停）</h3>
          <label class="radio-line">
            <input type="radio" value="enabled" v-model="rule.alert_table_scope">
            仅启用中的桌台（推荐）—— 停用的桌台维护与否没有业务影响
          </label>
          <label class="radio-line">
            <input type="radio" value="all" v-model="rule.alert_table_scope">
            全部桌台 —— 不论启停，只要维护就告警
          </label>
          <label class="line" style="margin-top:8px">
            <input type="checkbox" v-model="rule.alert_on_disable">
            桌台被停用时发一条提醒
          </label>
          <hr class="rule-sep">
          <h4 class="sub-h">复核提醒</h4>
          <label class="line">
            <input type="checkbox" v-model="rule.review_enabled">
            非在用的桌台维护超过
            <input type="number" v-model.number="rule.review_days" min="1" max="90" class="num-input narrow">
            天时，在页面上提醒复核
          </label>
          <label class="line">
            <input type="checkbox" v-model="rule.review_notify" :disabled="!rule.review_enabled">
            同时发一条 Lark 提醒（不勾选就只在页面上提示，不打扰群）
          </label>
          <p class="field-hint">
            防的是和「在用桌台被误停」相反的方向：本该对外服务的桌台被标成非在用，
            一直挂着维护没人发现，永远不会告警。
            复核时点「确认下线」之后这台就不再提醒了 —— 确认过是有意下线的，就没必要一直问。
          </p>

          <p class="field-hint">
            选了「仅启用中」之后，桌台一停用它的维护告警就不再发了。
            如果这个停用本身是<strong>误操作</strong>，问题就被这条策略掩盖了 ——
            把上面那个开关打开，启用→停用的变化会单独提醒一次（只发一次，不重复告警）。
          </p>
        </div>

        <div class="panel">
          <h3>告警范围（按站点过滤）</h3>
          <label class="radio-line">
            <input type="radio" value="all" v-model="rule.alert_scope">
            全部站点 —— 只要桌台维护就告警
          </label>
          <label class="radio-line">
            <input type="radio" value="watched" v-model="rule.alert_scope">
            仅关注站点 —— 维护涉及的站点里有关注的才告警
          </label>
          <p class="field-hint">
            已关注 <strong>{{ siteStats.watched }}</strong> 个站点。
            <button class="btn-link" @click="activeTab = 'sites'; loadSites()">去站点管理</button><br>
            选了「仅关注站点」后，没碰到关注站点的维护<strong>页面上照样看得到</strong>，
            只是不发 Lark、不计入「告警中」—— 信息不丢，只是不吵人。
            <span v-if="rule.alert_scope === 'watched' && !siteStats.watched" class="err-msg">
              ⚠ 当前一个关注站点都没有，这会导致<strong>所有告警都不发</strong>。
            </span>
          </p>
          <label class="line">
            <input type="checkbox" v-model="rule.list_watched_sites">
            告警内容里列出受影响的关注站点，最多显示
            <input type="number" v-model.number="rule.max_list_sites" min="1" class="num"> 个
          </label>
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

    <!-- ================= Tab 例行维护 ================= -->
    <div v-if="activeTab === 'windows'" class="tab-content">
      <div v-if="!currentEnvId" class="empty-block">请先在上方选择一个环境</div>
      <template v-else>
        <div class="action-bar">
          <button v-if="canWindowManage" class="btn btn-primary" @click="openCreateWindow">+ 新增例行维护</button>
          <span class="hint">
  一个窗口可以覆盖多个房间（同一时间一起保养）。以<strong>房间号</strong>为准，一个桌台可能有多个房间。
          </span>
        </div>

        <p class="hint-line">
          维护<strong>开始时间</strong>落在窗口内，就算作这次例行保养 —— 即使拖到窗口之外，
          也仍然知道它属于哪次计划，能报出「已超时多久」。
          <strong>超时不恢复才是真正要人去看的情况</strong>，建议保持开启。
        </p>

        <table class="data-table">
          <thead>
            <tr><th>名称</th><th>时间规则</th><th>适用房间</th><th>窗口内</th><th>超时告警</th><th>状态</th><th>备注</th><th>操作</th></tr>
          </thead>
          <tbody>
            <tr v-if="!windows.length"><td colspan="8" class="empty">
              还没有配置例行维护窗口。没有窗口时，所有维护都按计划外处理。
            </td></tr>
            <tr v-for="w in windows" :key="w.id" :class="{ 'row-routine': w.active_now }">
              <td class="strong">
                {{ w.name }}
                <span v-if="w.active_now" class="tag tag-routine" :title="'当前正处于该窗口，计划 ' + w.current_end + ' 结束'">进行中</span>
              </td>
              <td class="mono small">{{ w.rule_text }}</td>
              <td>
                <span v-if="w.table_nos === '*'" class="tag tag-unknown">全部房间</span>
                <span v-else :title="w.table_nos">{{ w.table_count }} 项</span>
              </td>
              <td>
                <span v-if="w.action === 'suppress'" class="dim">不告警</span>
                <span v-else>告警并标注例行</span>
              </td>
              <td>
                <span v-if="w.overrun_alert" class="tag tag-enable">开启</span>
                <span v-else class="tag tag-disable">关闭</span>
              </td>
              <td>
                <span v-if="w.enabled" class="tag tag-enable">启用</span>
                <span v-else class="tag tag-unknown">停用</span>
              </td>
              <td class="dim small">{{ w.remark }}</td>
              <td>
                <template v-if="canWindowManage">
                  <button class="btn-link" @click="openEditWindow(w)">编辑</button>
                  <button class="btn-link danger" @click="deleteWindow(w)">删除</button>
                </template>
                <span v-else class="dim">—</span>
              </td>
            </tr>
          </tbody>
        </table>
      </template>
    </div>

    <!-- ================= Tab 站点管理 ================= -->
    <div v-if="activeTab === 'sites'" class="tab-content">
      <div v-if="!currentEnvId" class="empty-block">请先在上方选择一个环境</div>
      <template v-else>
        <div class="stat-row">
          <div class="stat-card"><div class="sc-num">{{ siteStats.total }}</div><div class="sc-label">已发现站点</div></div>
          <div class="stat-card"><div class="sc-num">{{ siteStats.named }}</div><div class="sc-label">已命名</div></div>
          <div class="stat-card maintain"><div class="sc-num">{{ siteStats.watched }}</div><div class="sc-label">★ 关注中</div></div>
        </div>

        <p class="hint-line">
          站点由采集<strong>自动发现</strong>，接口只给 siteId 不给名称，所以名字需要你补。
          只需给关心的那几个起名、打★，其余可以一直“未命名”放着。
          <strong>“出现在 N 台”</strong>指有多少张桌台的维护涉及过它，可据此判断重不重要。
        </p>

        <div class="filter-bar">
          <select v-model="siteFilter.watched" @change="loadSites()">
            <option value="">关注：全部</option>
            <option value="1">只看关注</option>
          </select>
          <select v-model="siteFilter.named" @change="loadSites()">
            <option value="">命名：全部</option>
            <option value="1">已命名</option>
            <option value="0">未命名</option>
          </select>
          <input v-model="siteFilter.q" placeholder="站点名 / siteId" @keyup.enter="loadSites()">
          <button class="btn btn-primary" @click="loadSites()">搜索</button>
          <button v-if="canSiteManage" class="btn btn-primary" @click="openAddSites">+ 手动添加站点</button>
          <button v-if="canSiteManage && siteSelection.length" class="btn btn-secondary" @click="batchWatch(true)">
            ★ 关注选中 ({{ siteSelection.length }})
          </button>
          <button v-if="canSiteManage && siteSelection.length" class="btn btn-secondary" @click="batchWatch(false)">
            取消关注
          </button>
        </div>

        <table class="data-table">
          <thead>
            <tr>
              <th style="width:34px"><input type="checkbox" :checked="allSitesChecked" @change="toggleAllSites"></th>
              <th>关注</th><th>站点名称</th><th>siteId</th><th>来源</th><th>出现在</th><th>最近一次</th><th>备注</th><th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="!sites.length"><td colspan="9" class="empty">
              还没有发现站点 —— 采集到维护中的桌台后会自动入库
            </td></tr>
            <tr v-for="st in sites" :key="st.id" :class="{ 'row-routine': st.watched }">
              <td><input type="checkbox" :value="st.id" v-model="siteSelection"></td>
              <td>
                <button class="btn-link star" :class="{ on: st.watched }"
                        :disabled="!canInService" @click="toggleWatch(st)">
                  {{ st.watched ? '★' : '☆' }}
                </button>
              </td>
              <td :class="st.site_name ? 'strong' : 'dim'">{{ st.site_name || '(未命名)' }}</td>
              <td class="mono small dim">{{ st.site_id }}</td>
              <td>
                <span v-if="st.source === 'manual'" class="tag tag-routine" title="人工录入，名称不会被采集覆盖">手动</span>
                <span v-else class="tag tag-unknown" title="由采集从维护列表里自动发现">自动</span>
              </td>
              <td>
                <span v-if="st.never_seen" class="dim" title="已录入，但至今没在任何一次维护里出现过">尚未出现</span>
                <span v-else>{{ st.table_count }} 台</span>
              </td>
              <td class="small dim">{{ st.last_seen_at || '—' }}</td>
              <td class="dim small">{{ st.remark }}</td>
              <td>
                <button v-if="canSiteManage" class="btn-link" @click="openEditSite(st)">编辑</button>
                <span v-else class="dim">—</span>
              </td>
            </tr>
          </tbody>
        </table>
      </template>
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

      <div class="pager" v-if="logsTotal">
        <span class="pg-total">共 {{ logsTotal }} 条</span>
        <select v-model.number="logSize" @change="logPage = 1; loadLogs()" class="pg-size">
          <option :value="10">10 条/页</option>
          <option :value="20">20 条/页</option>
          <option :value="50">50 条/页</option>
          <option :value="100">100 条/页</option>
        </select>
        <button class="btn btn-secondary" :disabled="logPage <= 1" @click="logPage--; loadLogs()">上一页</button>
        <span class="pg-cur">第 {{ logPage }} / {{ logPages }} 页</span>
        <button class="btn btn-secondary" :disabled="logPage >= logPages" @click="logPage++; loadLogs()">下一页</button>
        <span class="pg-jump">
          跳至
          <input type="number" min="1" :max="logPages" v-model.number="logJump"
                 @keyup.enter="gotoLogPage" class="pg-input">
          页
          <button class="btn btn-secondary" @click="gotoLogPage">确定</button>
        </span>
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
              <input type="number" v-model.number="envForm.interval_sec"
                     min="10" max="3600" step="5" class="num-input"
                     title="10 ~ 3600 秒；改完保存即刻生效，不用重启服务">
              <label>秒采集一次</label>
              <span class="preset-group">
                <button type="button" class="btn-link" v-for="p in intervalPresets" :key="p.v"
                        :class="{ on: envForm.interval_sec === p.v }"
                        @click="envForm.interval_sec = p.v">{{ p.t }}</button>
              </span>
            </div>
            <div class="inline-hint">
              范围 10 ~ 3600 秒，推荐 60。保存后立刻生效 —— 调度器每 5 秒重读一次各环境的间隔，不用重启。
              实际精度 ±5 秒（填 5 的倍数最准），间隔越短中台压力越大；注意告警间隔不能比采集间隔还短。
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
              <label>状态字段</label><input v-model="envForm.f_status">
              <label>站点状态字段</label><input v-model="envForm.f_maintain">
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
              站点状态字段非空即视为维护中（推荐）
            </label>
            <label class="radio-line">
              <input type="radio" value="status_equals" v-model="envForm.maintain_rule">
              状态字段等于
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

    <!-- ================= 例行维护窗口弹窗 ================= -->
    <div v-if="windowDialog" class="modal-mask" @click.self="windowDialog = false">
      <div class="modal wide">
        <div class="modal-head">
          <h3>{{ windowForm.id ? '编辑例行维护' : '新增例行维护' }}</h3>
          <button class="close" @click="windowDialog = false">×</button>
        </div>
        <div class="modal-body">
          <div class="form-row">
            <label>名称 *</label>
            <input v-model="windowForm.name" class="wide-input" placeholder="如：A厅每日凌晨保养">
            <label class="cb"><input type="checkbox" v-model="windowForm.enabled"> 启用</label>
          </div>

          <fieldset>
            <legend>时间</legend>
            <div class="form-row">
              <label>重复</label>
              <select v-model="windowForm.repeat_type">
                <option value="daily">每天</option>
                <option value="weekly">每周</option>
                <option value="monthly">每月</option>
                <option value="once">指定日期</option>
              </select>
              <label>从</label><input v-model="windowForm.start_time" class="time" placeholder="02:00">
              <label>到</label><input v-model="windowForm.end_time" class="time" placeholder="04:00">
            </div>
            <p class="field-hint">结束时间小于开始时间表示跨零点，例如 23:00 → 01:00。</p>

            <div class="form-row" v-if="windowForm.repeat_type === 'weekly'">
              <label>星期</label>
              <label v-for="d in weekdayOptions" :key="d.v" class="chip"
                     :class="{ on: windowForm.weekdays.includes(String(d.v)) }">
                <input type="checkbox" :checked="hasWeekday(d.v)" @change="toggleWeekday(d.v)"> {{ d.label }}
              </label>
            </div>
            <div class="form-row" v-if="windowForm.repeat_type === 'monthly'">
              <label>每月几号</label>
              <input v-model="windowForm.month_days" class="wide-input" placeholder="如 1,15（逗号分隔）">
            </div>
            <div class="form-row" v-if="windowForm.repeat_type === 'once'">
              <label>日期</label>
              <input v-model="windowForm.once_date" type="date">
            </div>
          </fieldset>

          <fieldset>
            <legend>适用房间</legend>
            <label class="radio-line">
              <input type="radio" value="all" v-model="windowTableMode"> 该环境全部房间
            </label>
            <label class="radio-line">
              <input type="radio" value="list" v-model="windowTableMode"> 指定房间
            </label>
            <div v-if="windowTableMode === 'list'">
              <div class="form-row">
                <input v-model="windowTableInput" class="wide-input"
                       placeholder="房间号，逗号分隔，如 N013-2,E015">
              </div>
              <p class="field-hint">
                以<strong>房间号</strong>为准 —— 一个桌台可能有多个房间（如 N13 下有 N013 和 N013-2），
                它们各自独立维护。<br>
                也可以填<strong>桌台号</strong>（如 N13），表示该桌台下的<strong>全部房间</strong>，省得一个个列。
              </p>
              <div class="room-picker">
                <div class="rp-head">
                  <input v-model="roomPickerQuery" class="rp-search"
                         placeholder="搜房间号 / 桌台号" @keydown.enter.prevent>
                  <span class="hint">
                    已选 <strong>{{ pickedRooms.length }}</strong> 个
                    · 共 {{ filteredPickerRooms.length }} 个启用房间
                  </span>
                  <button v-if="pickedRooms.length" class="btn-link danger" @click.prevent="clearPickedRooms">清空</button>
                </div>
                <div class="rp-list">
                  <button v-for="r in filteredPickerRooms" :key="r.room_id"
                          class="chip rp-item" :class="{ on: pickedRooms.includes(r.room_no) }"
                          :title="'桌台 ' + r.table_no + (r.maintaining ? '（当前维护中）' : '')"
                          @click.prevent="toggleRoomPick(r.room_no)">
                    {{ r.room_no }}
                    <span class="rp-table">{{ r.table_no }}</span>
                    <span v-if="r.maintaining" class="rp-dot" title="当前维护中">●</span>
                  </button>
                  <div v-if="!filteredPickerRooms.length" class="hint" style="padding:10px">
                    <template v-if="!allRooms.length">
                      还没采集到房间 —— 先去「环境配置」填好地址并采集一次，或直接在上方手填房间号。
                    </template>
                    <template v-else>没有匹配的房间</template>
                  </div>
                </div>
              </div>
            </div>
          </fieldset>

          <fieldset>
            <legend>告警方式</legend>
            <label class="radio-line">
              <input type="radio" value="annotate" v-model="windowForm.action">
              照常告警，但标明是例行维护（推荐）
            </label>
            <label class="radio-line">
              <input type="radio" value="suppress" v-model="windowForm.action">
              窗口内完全不告警
            </label>
            <label class="line" style="margin-top:8px">
              <input type="checkbox" v-model="windowForm.overrun_alert">
              超出窗口仍未恢复时照常告警（强烈建议开启）
            </label>
            <p class="field-hint">
              例行保养本身是预期的，但<strong>拖过计划结束时间还没恢复</strong>说明出了状况 ——
              这种情况会以「例行维护已超时」的措辞单独报出来，并且不等下一个告警间隔，立刻发一条。
            </p>
          </fieldset>

          <div class="form-row">
            <label>备注</label>
            <input v-model="windowForm.remark" class="wide-input">
          </div>
        </div>
        <div class="modal-foot">
          <div class="spacer"></div>
          <button class="btn btn-secondary" @click="windowDialog = false">取消</button>
          <button class="btn btn-primary" @click="saveWindow" :disabled="windowSaving">
            {{ windowSaving ? '保存中…' : '保存' }}
          </button>
        </div>
      </div>
    </div>

    <!-- ================= 手动添加站点 ================= -->
    <div v-if="addSitesDialog" class="modal-mask" @click.self="addSitesDialog = false">
      <div class="modal wide">
        <div class="modal-head">
          <h3>手动添加站点</h3>
          <button class="close" @click="addSitesDialog = false">×</button>
        </div>
        <div class="modal-body">
          <p class="field-hint">
            自动发现只能拿到<strong>维护过的桌台涉及的站点</strong>。
            在这里可以提前把已知站点录进来，不用等它出现在某次维护里。
            <strong>人工录入优先</strong>：你填的名字不会被后续采集覆盖。
          </p>
          <div class="tabs sub-tabs">
            <button class="tab" :class="{ active: addMode === 'batch' }" @click="addMode = 'batch'">批量粘贴</button>
            <button class="tab" :class="{ active: addMode === 'single' }" @click="addMode = 'single'">单条添加</button>
          </div>
          <template v-if="addMode === 'batch'">
            <p class="field-hint">
              每行一个站点，格式 <code>siteId,站点名</code>。
              逗号、制表符、空格都认；只填 siteId 不填名字也行。
            </p>
            <textarea v-model="addSitesRaw" rows="10" class="raw-input" spellcheck="false"
              placeholder="1156362225550845952,BPUat"></textarea>
            <div class="form-row">
              <label class="cb"><input type="checkbox" v-model="addSitesWatched"> 导入后直接标为关注</label>
            </div>
          </template>
          <template v-else>
            <div class="form-row">
              <label>siteId *</label>
              <input v-model="addSiteForm.site_id" class="wide-input" placeholder="如 1156362225550845952">
            </div>
            <div class="form-row">
              <label>站点名称</label>
              <input v-model="addSiteForm.site_name" class="wide-input" placeholder="如 BPUat">
            </div>
            <div class="form-row">
              <label>备注</label>
              <input v-model="addSiteForm.remark" class="wide-input">
            </div>
            <label class="cb"><input type="checkbox" v-model="addSiteForm.watched"> 关注这个站点</label>
          </template>
          <div v-if="addSitesResult" class="test-result ok">
            <div class="tr-head">✅ 导入完成</div>
            <div>共 {{ addSitesResult.total }} 条：新增 {{ addSitesResult.added }} 个、更新 {{ addSitesResult.updated }} 个</div>
            <div class="field-hint" v-if="addSitesResult.updated">
              更新的是之前被自动发现的站点，已升级为「手动」并用你填的名字。
            </div>
          </div>
        </div>
        <div class="modal-foot">
          <div class="spacer"></div>
          <button class="btn btn-secondary" @click="addSitesDialog = false">关闭</button>
          <button class="btn btn-primary" @click="submitAddSites" :disabled="addSitesSaving">
            {{ addSitesSaving ? '导入中…' : '导入' }}
          </button>
        </div>
      </div>
    </div>

    <!-- ================= 编辑站点弹窗 ================= -->
    <div v-if="siteDialog" class="modal-mask" @click.self="siteDialog = false">
      <div class="modal">
        <div class="modal-head">
          <h3>编辑站点</h3>
          <button class="close" @click="siteDialog = false">×</button>
        </div>
        <div class="modal-body">
          <div class="form-row"><label>siteId</label><span class="mono small dim">{{ siteForm.site_id }}</span></div>
          <div class="form-row">
            <label>站点名称</label>
            <input v-model="siteForm.site_name" class="wide-input" placeholder="如：泰坦体育">
          </div>
          <label class="cb"><input type="checkbox" v-model="siteForm.watched"> 关注这个站点</label>
          <p class="field-hint">
            只有关注的站点会显示在桌台列表的「影响站点」里；
            告警范围选了「仅关注站点」时，只有它们受影响才发 Lark。
          </p>
          <div class="form-row"><label>备注</label><input v-model="siteForm.remark" class="wide-input"></div>
        </div>
        <div class="modal-foot">
          <div class="spacer"></div>
          <button class="btn btn-secondary" @click="siteDialog = false">取消</button>
          <button class="btn btn-primary" @click="saveSite">保存</button>
        </div>
      </div>
    </div>

    <!-- ================= 桌台站点详情 ================= -->
    <div v-if="roomSitesDialog" class="modal-mask" @click.self="roomSitesDialog = false">
      <div class="modal wide">
        <div class="modal-head">
          <h3>桌台 {{ roomSites.table_no }} 受影响站点（共 {{ roomSites.total }} 个）</h3>
          <button class="close" @click="roomSitesDialog = false">×</button>
        </div>
        <div class="modal-body">
          <div class="site-group">
            <div class="sg-title">★ 关注的（{{ roomSites.watched.length }}）</div>
            <div v-if="!roomSites.watched.length" class="empty-block small">
              这次维护没有涉及任何关注的站点。
              告警范围选了「仅关注站点」的话，它不会发 Lark。
            </div>
            <div v-else class="chip-list">
              <span v-for="x in roomSites.watched" :key="x.site_id" class="chip on" :title="x.site_id">
                {{ x.site_name || x.site_id }}
              </span>
            </div>
          </div>

          <div class="site-group">
            <div class="sg-title">其余站点（{{ roomSites.others.length }}）</div>
            <p class="field-hint">未关注的站点不会出现在列表里，也不会触发告警。要关注去「站点管理」打★。</p>
            <div class="chip-list">
              <span v-for="x in roomSites.others" :key="x.site_id" class="chip" :title="x.site_id">
                {{ x.site_name || x.site_id }}
              </span>
            </div>
          </div>
        </div>
        <div class="modal-foot">
          <button class="btn btn-secondary" @click="activeTab = 'sites'; roomSitesDialog = false; loadSites()">
            去站点管理
          </button>
          <div class="spacer"></div>
          <button class="btn btn-secondary" @click="roomSitesDialog = false">关闭</button>
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

.tag { display: inline-block; padding: 2px 8px; border-radius: 10px; font-size: 12px; white-space: nowrap; }
.tag-enable { background: rgba(16, 185, 129, .15); color: var(--success); }
.tag-disable { background: rgba(113, 128, 150, .2); color: var(--text-secondary); }
.tag-maintain { background: rgba(245, 158, 11, .18); color: var(--warning); font-weight: 600; }
.tag-normal { background: rgba(16, 185, 129, .12); color: var(--success); }
.tag-unknown { background: var(--bg-hover); color: var(--text-muted); }
.tag-routine { background: rgba(59, 130, 246, .18); color: var(--primary); font-weight: 600; }
.tag-overrun { background: rgba(239, 68, 68, .18); color: var(--danger); font-weight: 600; }
.row-routine { background: rgba(59, 130, 246, .06); }
.site-link { text-align: left; padding: 0; }
.star { font-size: 16px; padding: 0 4px; color: var(--text-muted); }
.star.on { color: var(--warning); }
.site-group { margin-bottom: 18px; }
.sg-title { font-size: 13px; font-weight: 600; margin-bottom: 8px; color: var(--text-primary); }
.sub-tabs { margin-bottom: 12px; border-bottom-width: 1px; }
.sub-tabs .tab { padding: 6px 14px; font-size: 13px; }
.raw-input {
  width: 100%; padding: 10px; border-radius: 6px; border: 1px solid var(--border-color);
  background: var(--bg-input); color: var(--text-primary);
  font-family: ui-monospace, Menlo, Consolas, monospace; font-size: 12px; line-height: 1.7;
}
.raw-input::placeholder { color: var(--text-muted); }
.room-picker { border: 1px solid var(--border-color); border-radius: 8px; overflow: hidden; margin-top: 8px; }
.rp-head {
  display: flex; align-items: center; gap: 10px; padding: 8px 10px;
  background: var(--bg-hover); border-bottom: 1px solid var(--border-color);
}
.rp-search {
  flex: 0 0 200px; padding: 5px 9px; border-radius: 6px;
  border: 1px solid var(--border-color); background: var(--bg-input);
  color: var(--text-primary); font-size: 13px;
}
.rp-search::placeholder { color: var(--text-muted); }
.rp-list { display: flex; flex-wrap: wrap; gap: 6px; padding: 10px; max-height: 210px; overflow-y: auto; }
.rp-item { display: inline-flex; align-items: center; gap: 6px; }
.rp-table { font-size: 11px; color: var(--text-muted); }
.rp-item.on .rp-table { color: var(--primary); }
.rp-dot { color: var(--warning); font-size: 9px; }
.routine-chip { margin-right: 4px; font-weight: 500; }
.sc-sub { font-size: 11px; color: var(--text-muted); margin-top: 4px; cursor: help; }
.svc-toggle { font-size: 14px; color: var(--text-muted); padding: 0 6px; }
.svc-toggle.on { color: var(--success); font-weight: 700; }

.btn { padding: 6px 14px; border-radius: 6px; border: 1px solid transparent; cursor: pointer; font-size: 13px; }
.btn-primary { background: var(--primary); color: #fff; }
.btn-primary:hover:not(:disabled) { background: var(--primary-dark); }
.btn-secondary { background: var(--bg-hover); border-color: var(--border-color); color: var(--text-primary); }
.btn:disabled { opacity: .5; cursor: not-allowed; }
.btn-link { background: none; border: none; color: var(--primary); cursor: pointer; font-size: 13px; padding: 0 6px; }
.btn-link.danger { color: var(--danger); }

.pager {
  display: flex; align-items: center; justify-content: center; gap: 10px;
  margin-top: 16px; font-size: 13px; color: var(--text-secondary); flex-wrap: wrap;
}
.pg-total { color: var(--text-muted); }
.pg-cur { min-width: 92px; text-align: center; color: var(--text-primary); }
.pg-size, .pg-input {
  padding: 5px 8px; border-radius: 6px; border: 1px solid var(--border-color);
  background: var(--bg-input); color: var(--text-primary); font-size: 13px;
}
.pg-jump { display: inline-flex; align-items: center; gap: 6px; }
.pg-input { width: 64px; }
.action-bar { display: flex; align-items: center; gap: 12px; margin-bottom: 12px; }
.action-bar .hint, .hint { font-size: 12px; color: var(--text-secondary); }

/* 规则面板 */
.rule-panel { display: flex; flex-direction: column; gap: 16px; }
.panel {
  padding: 16px; border-radius: 8px; border: 1px solid var(--border-color);
  background: var(--bg-card); color: var(--text-primary);
}
.panel h3 { margin: 0 0 12px; font-size: 15px; color: var(--text-primary); }
.rule-sep { border: none; border-top: 1px solid var(--border-color); margin: 14px 0 10px; }
.sub-h { margin: 0 0 8px; font-size: 13px; color: var(--text-primary); }
.num-input.narrow { width: 64px; margin: 0 4px; }
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

/* 顶部提示条：未确认清单 / 待复核 */
.banner {
  display: flex; align-items: center; justify-content: space-between; gap: 12px;
  padding: 10px 14px; border-radius: 8px; margin-bottom: 12px;
  font-size: 13px; color: var(--text-primary); border: 1px solid var(--border-color);
  background: var(--bg-hover);
}
.banner.warn { border-color: var(--warning); }
.banner.info { border-color: var(--primary); }
.banner.sm { font-size: 12px; color: var(--text-secondary); margin: 10px 0; display: block; }

/* 统计按在用 / 非在用分成两组 */
.stat-group {
  flex: 2; min-width: 260px; padding: 12px 16px;
  border: 1px solid var(--border-color); border-radius: 10px; background: var(--bg-card);
}
.stat-group.primary { border-width: 2px; }
.stat-group.muted { background: var(--bg-hover); }
.grp-title { font-size: 12px; font-weight: 600; color: var(--text-secondary); margin-bottom: 6px; }
.grp-body { display: flex; align-items: center; gap: 18px; }
.grp-main { text-align: center; min-width: 78px; }
.grp-main.bad .sc-num { color: var(--danger); }
.grp-main .sc-label { font-size: 12px; color: var(--text-secondary); }
.grp-split { display: flex; flex-direction: column; gap: 4px; font-size: 12px; color: var(--text-secondary); }
.grp-item b { color: var(--text-primary); }
.grp-item.bad, .grp-item.bad b { color: var(--danger); }

/* 桌台列表页签：在用 / 非在用 / 全部 */
.room-tabs { display: flex; gap: 6px; margin: 14px 0 4px; }
.rt {
  padding: 6px 14px; border-radius: 6px; cursor: pointer; font-size: 13px;
  border: 1px solid var(--border-color); background: var(--bg-card); color: var(--text-secondary);
}
.rt.on { border-color: var(--primary); color: var(--primary); font-weight: 600; }

/* 批量确认对话框 */
.cfm-row {
  display: flex; align-items: center; justify-content: space-between; gap: 16px;
  padding: 12px 0; border-bottom: 1px solid var(--border-color);
}
.cfm-row.all { border-bottom: none; }
.cfm-info { display: flex; flex-direction: column; gap: 3px; font-size: 13px; color: var(--text-primary); }
.cfm-info .dim { font-size: 12px; color: var(--text-muted); }
.cfm-btns { display: flex; gap: 6px; flex-shrink: 0; }
.warn-line { font-size: 12px; color: var(--warning); max-width: 380px; line-height: 1.5; }
.num-input {
  width: 92px; padding: 6px 8px; border-radius: 6px;
  border: 1px solid var(--border-color); background: var(--bg-input); color: var(--text-primary);
}
.preset-group { display: inline-flex; gap: 2px; margin-left: 10px; }
.preset-group .btn-link { color: var(--text-secondary); }
.preset-group .btn-link.on { color: var(--primary); font-weight: 600; }
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
