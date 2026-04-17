<template>
  <div class="alert-stats-page">
    <div class="page-header">
      <h2>监控告警统计</h2>
      <p class="page-desc">从夜莺(N9E)获取告警数据，统计有效告警并管理维护窗口</p>
    </div>

    <!-- 数据来源 -->
    <div class="card source-card">
      <div class="card-top">
        <h3>数据来源</h3>
      </div>
      <div class="date-section">
        <div class="filter-row">
          <div class="date-field" style="max-width:220px" v-if="n9eConnections.length">
            <label>N9E 连接</label>
            <select class="input" v-model="selectedConnId">
              <option v-for="c in n9eConnections" :key="c.id" :value="c.id">{{ c.name }}</option>
            </select>
          </div>
          <div class="date-field" style="max-width:260px" v-if="busiGroups.length">
            <label>业务组筛选</label>
            <select class="input" v-model="selectedGroup">
              <option value="">全部业务组</option>
              <option v-for="g in busiGroups" :key="g.id" :value="g.name">{{ g.name }}</option>
            </select>
          </div>
          <div class="date-field">
            <label>告警级别</label>
            <div class="severity-filter">
              <label v-for="s in [1,2,3]" :key="s" class="sev-check">
                <input type="checkbox" :value="s" v-model="selectedSeverities" />
                <span :class="'severity s' + s">S{{ s }}</span>
              </label>
            </div>
          </div>
          <div class="quick-dates" style="align-self:flex-end">
            <button v-for="q in quickDates" :key="q.label"
              :class="['btn', 'btn-sm', { 'btn-primary': activeQuick === q.label }]"
              @click="applyQuickDate(q)">
              {{ q.label }}
            </button>
          </div>
        </div>
        <div class="date-range">
          <div class="date-field">
            <label>开始日期</label>
            <input type="date" class="input" v-model="startDate" />
          </div>
          <span class="date-sep">~</span>
          <div class="date-field">
            <label>结束日期</label>
            <input type="date" class="input" v-model="endDate" />
          </div>
          <button v-if="!fetching" class="btn btn-primary" @click="fetchAlertStats">获取数据</button>
          <button v-else class="btn btn-danger" @click="cancelFetch">取消</button>
          <button v-if="fetchDone && !fetching" class="btn" @click="clearData">清除数据</button>
        </div>
      </div>

      <div v-if="fetching" class="fetch-status">
        <div class="progress-bar-wrap">
          <div class="progress-bar" :style="{ width: fetchProgress + '%' }"></div>
        </div>
        <span class="progress-text">{{ fetchStatusText }}</span>
      </div>
    </div>

    <!-- Toast 提示 -->
    <transition name="toast-fade">
      <div v-if="toast.show" :class="['toast', toast.type]" @click="toast.show = false">
        <span>{{ toast.message }}</span>
      </div>
    </transition>

    <!-- 维护窗口管理 (隐藏) -->
    <div v-if="false" class="card table-card">
      <div class="card-top">
        <h3>维护窗口管理</h3>
        <button class="btn btn-sm btn-primary" @click="newMW = defaultMW(); showAddMW = true">+ 添加维护窗口</button>
      </div>

      <!-- 添加维护窗口表单 -->
      <div v-if="showAddMW" class="mw-form">
        <div class="mw-form-row">
          <div class="date-field">
            <label>项目名称</label>
            <input type="text" class="input" v-model="newMW.project" placeholder="如：G01" />
          </div>
          <div class="date-field" style="max-width:120px">
            <label>环境</label>
            <select class="input" v-model="newMW.environment">
              <option>PROD</option>
              <option>UAT</option>
            </select>
          </div>
          <div class="date-field" style="max-width:120px">
            <label>重复模式</label>
            <select class="input" v-model="newMW.repeat_mode">
              <option value="once">单次</option>
              <option value="weekly">每周</option>
            </select>
          </div>
          <div class="date-field" style="max-width:140px">
            <label>维护类型</label>
            <select class="input" v-model="newMW.maintenance_type">
              <option>例行维护</option>
              <option>临时维护</option>
              <option>紧急维护</option>
            </select>
          </div>
          <!-- 每周重复 -->
          <template v-if="newMW.repeat_mode === 'weekly'">
            <div class="date-field" style="max-width:100px">
              <label>星期</label>
              <select class="input" v-model="newMW.weekday">
                <option v-for="(d, i) in ['一','二','三','四','五','六','日']" :key="i" :value="i + 1">周{{ d }}</option>
              </select>
            </div>
            <div class="date-field" style="max-width:110px">
              <label>开始时间</label>
              <input type="time" class="input" v-model="newMW.time_start" />
            </div>
            <div class="date-field" style="max-width:110px">
              <label>结束时间</label>
              <input type="time" class="input" v-model="newMW.time_end" />
            </div>
          </template>
          <!-- 单次 -->
          <template v-else>
            <div class="date-field">
              <label>开始时间</label>
              <input type="datetime-local" class="input" v-model="newMW.start_time" />
            </div>
            <div class="date-field">
              <label>结束时间</label>
              <input type="datetime-local" class="input" v-model="newMW.end_time" />
            </div>
          </template>
          <div class="date-field" style="flex:1">
            <label>匹配规则名（可选，逗号分隔，不填匹配所有）</label>
            <input type="text" class="input" v-model="newMW.match_rules" placeholder="TcpPortDown, ApplicationInstanceDown" />
          </div>
        </div>
        <!-- 维护操作记录 -->
        <div class="mw-ops-section">
          <div class="mw-ops-header">
            <label class="sub-title">维护操作记录（可选）</label>
            <button class="btn btn-xs" @click="newMW.operations.push({ time_start: '', time_end: '', ip: '', content: '' })">+ 添加</button>
          </div>
          <div v-for="(op, oi) in newMW.operations" :key="oi" class="mw-op-row">
            <input type="time" class="input input-sm" v-model="op.time_start" style="width:100px" />
            <span class="date-sep">~</span>
            <input type="time" class="input input-sm" v-model="op.time_end" style="width:100px" />
            <input type="text" class="input input-sm" v-model="op.ip" placeholder="IP" style="width:140px" />
            <input type="text" class="input input-sm" v-model="op.content" placeholder="操作内容（如：重启xxx服务）" style="flex:1" />
            <button class="btn-icon danger" @click="newMW.operations.splice(oi, 1)">&times;</button>
          </div>
        </div>
        <div class="date-field" style="margin-top:8px">
          <label>备注</label>
          <input type="text" class="input" v-model="newMW.note" placeholder="可选" />
        </div>
        <div class="mw-form-actions">
          <button class="btn btn-sm btn-primary" @click="createMW">保存</button>
          <button class="btn btn-sm" @click="showAddMW = false">取消</button>
        </div>
      </div>

      <div class="table-wrapper" v-if="maintenanceWindows.length">
        <table>
          <thead>
            <tr>
              <th style="width:80px">项目</th>
              <th style="width:60px">环境</th>
              <th style="width:80px">重复</th>
              <th style="width:90px">维护类型</th>
              <th style="width:160px">时间</th>
              <th style="width:160px">匹配规则</th>
              <th>备注</th>
              <th style="width:60px"></th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="mw in maintenanceWindows" :key="mw.id">
              <td>{{ mw.project }}</td>
              <td>{{ mw.environment || 'PROD' }}</td>
              <td>{{ mw.repeat_mode === 'weekly' ? '每周' : '单次' }}</td>
              <td><span :class="['mw-tag', mw.maintenance_type === '紧急维护' ? 'urgent' : mw.maintenance_type === '临时维护' ? 'temp' : 'routine']">{{ mw.maintenance_type }}</span></td>
              <td>{{ formatMWTime(mw) }}</td>
              <td><span class="match-rules">{{ mw.match_rules || '全部' }}</span></td>
              <td>{{ mw.note || '-' }}</td>
              <td class="action-cell" style="white-space:nowrap">
                <button class="btn-icon" @click="editMW(mw)" title="编辑">&#9998;</button>
                <button class="btn-icon danger" @click="deleteMW(mw.id)" title="删除">&times;</button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <p v-else class="no-data">暂无维护窗口</p>
    </div>

    <!-- 统计摘要 -->
    <div class="card summary-card" v-if="fetchDone">
      <p class="summary-text">
        本周告警 <strong>{{ totalAlerts }}</strong> 条，有效告警 <strong>{{ filteredAlerts.length }}</strong> 条，处理完成率
        <strong :class="completionRate >= 100 ? 'rate-ok' : 'rate-warn'">{{ completionRate.toFixed(0) }}%</strong>
      </p>
    </div>

    <!-- 有效告警表格 -->
    <div class="card table-card" v-if="fetchDone">
      <div class="card-top">
        <h3>有效告警 <span class="row-count" v-if="filteredAlerts.length">({{ filteredAlerts.length }})</span></h3>
        <div class="top-actions">
          <span v-if="selectedIndexes.length" class="selected-count">已选 {{ selectedIndexes.length }} 条</span>
          <button class="btn btn-sm" @click="showBatchForm = !showBatchForm">{{ selectedIndexes.length ? '批量操作(选中)' : '批量操作(全部)' }}</button>
          <button class="btn btn-sm" @click="showMarkMW = !showMarkMW">标记维护</button>
        </div>
      </div>

      <!-- 批量操作表单 -->
      <div v-if="showBatchForm" class="mw-form">
        <p class="mw-hint">{{ selectedIndexes.length ? `将应用到选中的 ${selectedIndexes.length} 条告警` : '将应用到全部告警' }}</p>
        <div class="mw-form-row">
          <div class="date-field" style="max-width:140px">
            <label>处理状态</label>
            <select class="input" v-model="batchData.status">
              <option value="">不修改</option>
              <option>已处理</option>
              <option>处理中</option>
            </select>
          </div>
          <div class="date-field">
            <label>处理人</label>
            <input type="text" class="input" v-model="batchData.handler" placeholder="不填则不修改" />
          </div>
          <div class="date-field" style="flex:1">
            <label>备注</label>
            <input type="text" class="input" v-model="batchData.note" placeholder="不填则不修改" />
          </div>
        </div>
        <div class="mw-form-actions">
          <button class="btn btn-sm btn-primary" @click="applyBatch">应用</button>
          <button class="btn btn-sm" @click="showBatchForm = false">取消</button>
        </div>
      </div>

      <!-- 标记维护表单 -->
      <div v-if="showMarkMW" class="mw-form">
        <p class="mw-hint">指定项目和时间段，该时间段内的所有告警将合并为 1 条维护告警</p>
        <div class="mw-form-row">
          <div class="date-field">
            <label>项目名称</label>
            <input type="text" class="input" v-model="markMW.project" placeholder="如：XX项目" />
          </div>
          <div class="date-field" style="max-width:140px">
            <label>维护类型</label>
            <select class="input" v-model="markMW.maintenance_type">
              <option>例行维护</option>
              <option>紧急维护</option>
            </select>
          </div>
          <div class="date-field">
            <label>开始时间</label>
            <input type="datetime-local" class="input" v-model="markMW.start_time" />
          </div>
          <div class="date-field">
            <label>结束时间</label>
            <input type="datetime-local" class="input" v-model="markMW.end_time" />
          </div>
        </div>
        <div class="mw-form-actions">
          <button class="btn btn-sm btn-primary" @click="markAsMaintenance">确认标记</button>
          <button class="btn btn-sm" @click="showMarkMW = false">取消</button>
        </div>
      </div>

      <div class="table-wrapper">
        <table>
          <thead>
            <tr>
              <th style="width:36px"><input type="checkbox" @change="toggleSelectAll($event.target.checked)" :checked="selectedIndexes.length === filteredAlerts.length && filteredAlerts.length > 0" /></th>
              <th style="min-width:200px">告警</th>
              <th style="width:60px">级别</th>
              <th style="width:60px">次数</th>
              <th style="min-width:80px">实例</th>
              <th style="min-width:100px">影响</th>
              <th style="min-width:80px">处理人</th>
              <th style="width:80px">处理状态</th>
              <th style="min-width:140px">备注</th>
            </tr>
          </thead>
          <tbody>
            <template v-for="(alert, i) in filteredAlerts" :key="i">
              <tr :class="{ 'row-maintenance': alert.is_maintenance, 'row-recovered': alert.is_recovered && !alert.is_maintenance, 'row-selected': selectedIndexes.includes(i) }">
                <td><input type="checkbox" :checked="selectedIndexes.includes(i)" @change="toggleSelect(i)" /></td>
                <td class="clickable" @click="toggleExpand(i)">
                  <div class="alert-name">{{ alert.rule_name }} <span v-if="alert.raw_count > 1" class="expand-icon">{{ expandedIndex === i ? '▾' : '▸' }}</span></div>
                  <div class="alert-meta">{{ formatTime(alert.trigger_time) }}{{ alert.recover_time ? ' ~ ' + formatTime(alert.recover_time) : '' }}</div>
                </td>
                <td><span :class="'severity s' + alert.severity">S{{ alert.severity }}</span></td>
                <td>{{ alert.raw_count }}</td>
                <td><div class="alert-meta">{{ alert.instance || '-' }}</div></td>
                <td><input class="cell-input" v-model="alert.impact" :placeholder="alert.is_maintenance ? '无影响' : ''" /></td>
                <td><input class="cell-input" v-model="alert.handler" /></td>
                <td>
                  <select class="cell-input" v-model="alert.status">
                    <option value="">请选择</option>
                    <option>已处理</option>
                    <option>处理中</option>
                  </select>
                </td>
                <td><input class="cell-input" v-model="alert.note" :placeholder="alert.is_maintenance ? alert.maintenance_type : ''" /></td>
              </tr>
              <!-- 展开详情 -->
              <tr v-if="expandedIndex === i && alert._details?.length" class="detail-row">
                <td colspan="9">
                  <div class="detail-list">
                    <div class="detail-item" v-for="(d, j) in alert._details" :key="j">
                      <span class="detail-time">{{ formatTime(d.trigger_time) }}{{ d.recover_time ? ' ~ ' + formatTime(d.recover_time) : ' (未恢复)' }}</span>
                      <span class="detail-value" v-if="d.trigger_value">触发值: {{ d.trigger_value }}</span>
                      <span :class="d.is_recovered ? 'detail-ok' : 'detail-active'">{{ d.is_recovered ? '已恢复' : '活跃' }}</span>
                    </div>
                  </div>
                  <!-- 原始告警信息 -->
                  <div class="detail-original" v-if="alert._raw">
                    <div class="detail-section">
                      <span class="detail-label">告警描述：</span>
                      <span>{{ alert._raw.rule_note || '-' }}</span>
                    </div>
                    <div class="detail-section" v-if="alert._raw.tags?.length">
                      <span class="detail-label">标签：</span>
                      <span class="tag-item" v-for="t in alert._raw.tags" :key="t">{{ t }}</span>
                    </div>
                    <div class="detail-section" v-if="alert._raw.annotations && Object.keys(alert._raw.annotations).length">
                      <span class="detail-label">注解：</span>
                      <span v-for="(v, k) in alert._raw.annotations" :key="k" class="tag-item">{{ k }}={{ v }}</span>
                    </div>
                    <div class="detail-section">
                      <span class="detail-label">业务组：</span><span>{{ alert.group_name }}</span>
                      <span class="detail-label" style="margin-left:16px">数据源：</span><span>{{ alert._raw.cluster || '-' }}</span>
                    </div>
                  </div>
                </td>
              </tr>
            </template>
            <tr v-if="!filteredAlerts.length">
              <td colspan="9" class="no-data">该时间段内无有效告警</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <!-- 预览 -->
    <div class="card preview-card" v-if="fetchDone && filteredAlerts.length">
      <div class="card-top">
        <h3>报告预览</h3>
      </div>
      <div class="preview-content">
        <p class="summary-text">- 本周告警 {{ totalAlerts }} 条，有效告警 {{ filteredAlerts.length }} 条，处理完成率 {{ completionRate.toFixed(0) }}%。</p>
        <table class="preview-table">
          <thead>
            <tr>
              <th>告警</th>
              <th>影响</th>
              <th>处理人</th>
              <th>处理状态</th>
              <th>备注</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="(a, i) in filteredAlerts" :key="'p' + i">
              <td>{{ a.rule_name }}</td>
              <td>{{ a.impact || (a.is_maintenance ? '无影响' : '') }}</td>
              <td>{{ a.handler || '' }}</td>
              <td>{{ a.status || (a.is_maintenance ? '已处理' : '') }}</td>
              <td>{{ a.note || (a.is_maintenance ? a.maintenance_type : '') }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import api from '@/api'
import { processAlertsLocal as processAlertsComposable } from '@/composables/useAlertStats'

// N9E 连接
const n9eConnections = ref([])
const selectedConnId = ref('')

// 日期
const startDate = ref('')
const endDate = ref('')
const activeQuick = ref('')
const quickDates = [
  { label: '今天', days: 0 },
  { label: '近7天', days: 7 },
  { label: '本周', type: 'week' },
  { label: '本月', type: 'month' },
]

function applyQuickDate(q) {
  activeQuick.value = q.label
  const now = new Date()
  endDate.value = now.toISOString().split('T')[0]
  if (q.type === 'week') {
    const day = now.getDay() || 7
    const mon = new Date(now)
    mon.setDate(now.getDate() - day + 1)
    startDate.value = mon.toISOString().split('T')[0]
  } else if (q.type === 'month') {
    startDate.value = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-01`
  } else {
    const d = new Date(now)
    d.setDate(now.getDate() - q.days)
    startDate.value = d.toISOString().split('T')[0]
  }
}

// 业务组
const busiGroups = ref([])
const selectedGroup = ref('')
const selectedGroupId = computed(() => {
  if (!selectedGroup.value) return ''
  const g = busiGroups.value.find(b => b.name === selectedGroup.value)
  return g ? g.id : ''
})

async function loadBusiGroups() {
  try {
    const params = selectedConnId.value ? `?conn_id=${selectedConnId.value}` : ''
    const res = await api.get(`/api/n9e/busi-groups${params}`)
    busiGroups.value = res.data?.data || []
  } catch (e) { /* ignore */ }
}

// Toast 提示
const toast = ref({ show: false, message: '', type: 'error' })
let toastTimer = null
function showToast(message, type = 'error') {
  if (toastTimer) clearTimeout(toastTimer)
  toast.value = { show: true, message, type }
  toastTimer = setTimeout(() => { toast.value.show = false }, 4000)
}

// 告警数据
const fetching = ref(false)
const fetchDone = ref(false)
const fetchProgress = ref(0)
const fetchStatusText = ref('')
const totalAlerts = ref(0)
const cachedRawEvents = ref([])  // 缓存原始事件，用于维护窗口变动时重算
let fetchCancelled = false

function cancelFetch() {
  fetchCancelled = true
  fetching.value = false
  fetchStatusText.value = '已取消'
}
const allEffectiveAlerts = ref([])
const selectedSeverities = ref([1, 2])

const filteredAlerts = computed(() => {
  let list = allEffectiveAlerts.value
  if (selectedGroup.value) {
    list = list.filter(a => a.group_name === selectedGroup.value)
  }
  // 级别过滤（多选时 API 无法过滤，前端补过滤）
  if (selectedSeverities.value.length > 0 && selectedSeverities.value.length < 3) {
    list = list.filter(a => selectedSeverities.value.includes(a.severity) || a.is_maintenance)
  }
  return list
})

const completionRate = computed(() => {
  if (!filteredAlerts.value.length) return 0
  const handled = filteredAlerts.value.filter(a => a.status === '已处理').length
  return (handled / filteredAlerts.value.length) * 100
})

// 选择 & 展开
const selectedIndexes = ref([])
const expandedIndex = ref(-1)

function toggleSelect(i) {
  const idx = selectedIndexes.value.indexOf(i)
  if (idx >= 0) selectedIndexes.value.splice(idx, 1)
  else selectedIndexes.value.push(i)
}
function toggleSelectAll(checked) {
  if (checked) selectedIndexes.value = filteredAlerts.value.map((_, i) => i)
  else selectedIndexes.value = []
}
function toggleExpand(i) {
  expandedIndex.value = expandedIndex.value === i ? -1 : i
}

// 批量操作表单
const showBatchForm = ref(false)
const batchData = ref({ status: '', handler: '', note: '' })

function applyBatch() {
  const targets = selectedIndexes.value.length
    ? selectedIndexes.value.map(i => filteredAlerts.value[i])
    : filteredAlerts.value
  for (const a of targets) {
    if (batchData.value.status) a.status = batchData.value.status
    if (batchData.value.handler) a.handler = batchData.value.handler
    if (batchData.value.note) a.note = batchData.value.note
  }
  selectedIndexes.value = []
  showBatchForm.value = false
  batchData.value = { status: '', handler: '', note: '' }
  showToast(`已更新 ${targets.length} 条告警`, 'success')
}

async function fetchAlertStats() {
  if (!startDate.value || !endDate.value) return
  fetching.value = true
  fetchDone.value = false
  fetchCancelled = false
  fetchProgress.value = 0
  fetchStatusText.value = '正在获取告警总数...'

  try {
    const stime = Math.floor(new Date(startDate.value).getTime() / 1000)
    const etime = Math.floor(new Date(endDate.value + 'T23:59:59').getTime() / 1000)
    const connParam = selectedConnId.value ? `&conn_id=${selectedConnId.value}` : ''
    const bgidParam = selectedGroupId.value ? `&bgid=${selectedGroupId.value}` : ''
    // 第一页：获取总数
    const firstRes = await api.get(`/api/n9e/alert-events?stime=${stime}&etime=${etime}&limit=500&p=1${connParam}${bgidParam}`)
    const firstData = firstRes.data?.data || firstRes.data
    const total = firstData.total || 0
    let allEvents = firstData.list || []
    totalAlerts.value = total

    if (total === 0) {
      fetchStatusText.value = '该时间段无告警数据'
      allEffectiveAlerts.value = []
      fetchDone.value = true
      fetching.value = false
      return
    }

    fetchProgress.value = Math.min(Math.round((allEvents.length / total) * 100), 100)
    fetchStatusText.value = `已获取 ${allEvents.length} / ${total} 条`

    // 后续分页
    const totalPages = Math.ceil(total / 500)
    for (let p = 2; p <= totalPages; p++) {
      if (fetchCancelled) return
      const res = await api.get(`/api/n9e/alert-events?stime=${stime}&etime=${etime}&limit=500&p=${p}${connParam}${bgidParam}`)
      const d = res.data?.data || res.data
      const list = d.list || []
      allEvents = allEvents.concat(list)
      fetchProgress.value = Math.min(Math.round((allEvents.length / total) * 100), 100)
      fetchStatusText.value = `已获取 ${allEvents.length} / ${total} 条`
    }

    // 前端去重 + 维护窗口合并
    fetchStatusText.value = '正在分析有效告警...'
    fetchProgress.value = 95
    cachedRawEvents.value = allEvents
    allEffectiveAlerts.value = processAlertsLocal(allEvents, maintenanceWindows.value)
    fetchProgress.value = 100
    fetchStatusText.value = `完成：有效告警 ${allEffectiveAlerts.value.length} 条`
    fetchDone.value = true
    showToast(`获取完成：共 ${total} 条告警，有效 ${allEffectiveAlerts.value.length} 条`, 'success')
  } catch (e) {
    showToast('获取告警数据失败: ' + (e.response?.data?.error || e.message), 'error')
  } finally {
    fetching.value = false
  }
}

// 维护窗口
const maintenanceWindows = ref([])
const showAddMW = ref(false)
const newMW = ref({ project: '', environment: 'PROD', repeat_mode: 'once', maintenance_type: '例行维护', weekday: 1, time_start: '02:00', time_end: '06:00', start_time: '', end_time: '', match_rules: '', operations: [], note: '' })

async function loadMW() {
  try {
    const res = await api.get('/api/maintenance-windows')
    maintenanceWindows.value = res.data?.data || []
  } catch (e) { /* ignore */ }
}

const defaultMW = () => ({ project: '', environment: 'PROD', repeat_mode: 'once', maintenance_type: '例行维护', weekday: 1, time_start: '02:00', time_end: '06:00', start_time: '', end_time: '', match_rules: '', operations: [], note: '' })

// 用缓存的原始事件重算有效告警（不发请求）
function recalcEffectiveAlerts() {
  if (cachedRawEvents.value.length === 0) return
  allEffectiveAlerts.value = processAlertsLocal(cachedRawEvents.value, maintenanceWindows.value)
}

// 清除已获取的数据
function clearData() {
  cachedRawEvents.value = []
  allEffectiveAlerts.value = []
  totalAlerts.value = 0
  fetchDone.value = false
  selectedIndexes.value = []
  expandedIndex.value = -1
  showToast('已清除数据', 'success')
}

async function createMW() {
  try {
    if (newMW.value.id) {
      await api.put(`/api/maintenance-windows/${newMW.value.id}`, newMW.value)
    } else {
      await api.post('/api/maintenance-windows', newMW.value)
    }
    newMW.value = defaultMW()
    showAddMW.value = false
    await loadMW()
    recalcEffectiveAlerts()
  } catch (e) {
    showToast('保存失败: ' + (e.response?.data?.error || e.message))
  }
}

function editMW(mw) {
  newMW.value = {
    id: mw.id,
    project: mw.project || '',
    environment: mw.environment || 'PROD',
    repeat_mode: mw.repeat_mode || 'once',
    maintenance_type: mw.maintenance_type || '例行维护',
    weekday: mw.weekday || 1,
    time_start: mw.time_start || '02:00',
    time_end: mw.time_end || '06:00',
    start_time: mw.start_time ? mw.start_time.replace(' ', 'T').slice(0, 16) : '',
    end_time: mw.end_time ? mw.end_time.replace(' ', 'T').slice(0, 16) : '',
    match_rules: mw.match_rules || '',
    operations: Array.isArray(mw.operations) ? [...mw.operations] : [],
    note: mw.note || '',
  }
  showAddMW.value = true
}

async function deleteMW(id) {
  try {
    await api.delete(`/api/maintenance-windows/${id}`)
    await loadMW()
    recalcEffectiveAlerts()
  } catch (e) {
    showToast('删除失败: ' + (e.response?.data?.error || e.message))
  }
}

// 标记维护
const showMarkMW = ref(false)
const markMW = ref({ project: '', maintenance_type: '紧急维护', start_time: '', end_time: '' })

async function markAsMaintenance() {
  if (!markMW.value.project || !markMW.value.start_time || !markMW.value.end_time) {
    showToast('请填写项目名称和时间段')
    return
  }
  try {
    await api.post('/api/n9e/mark-maintenance', {
      project: markMW.value.project,
      maintenance_type: markMW.value.maintenance_type,
      start_time: markMW.value.start_time.replace('T', ' ') + ':00',
      end_time: markMW.value.end_time.replace('T', ' ') + ':00',
    })
    showMarkMW.value = false
    markMW.value = { project: '', maintenance_type: '紧急维护', start_time: '', end_time: '' }
    await loadMW()
    recalcEffectiveAlerts()
  } catch (e) {
    showToast('标记失败: ' + (e.response?.data?.error || e.message))
  }
}

// 工具函数
function formatTime(ts) {
  if (!ts) return ''
  return new Date(ts * 1000).toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })
}

const weekdayNames = ['', '周一', '周二', '周三', '周四', '周五', '周六', '周日']
function formatMWTime(mw) {
  if (mw.repeat_mode === 'weekly') {
    return `${weekdayNames[mw.weekday] || '周?'} ${mw.time_start || '??:??'} ~ ${mw.time_end || '??:??'}`
  }
  const fmtDT = (s) => {
    if (!s) return '?'
    const d = new Date(s)
    if (isNaN(d)) return s
    return `${String(d.getMonth()+1).padStart(2,'0')}/${String(d.getDate()).padStart(2,'0')} ${String(d.getHours()).padStart(2,'0')}:${String(d.getMinutes()).padStart(2,'0')}`
  }
  return `${fmtDT(mw.start_time)} ~ ${fmtDT(mw.end_time)}`
}

// 加载连接列表
async function loadConnections() {
  try {
    const res = await api.get('/api/connections/public')
    const all = res.data?.data || []
    n9eConnections.value = all.filter(c => c.type === 'n9e')
    if (n9eConnections.value.length) {
      const def = n9eConnections.value.find(c => c.is_default)
      selectedConnId.value = def ? def.id : n9eConnections.value[0].id
    }
  } catch (e) { /* ignore */ }
}

// 从 tags 中提取实例 IP
function extractInstance(event) {
  if (event.annotations?.instance) return event.annotations.instance
  const tags = event.tags || []
  for (const t of tags) {
    if (t.startsWith('instance=')) return t.split('=')[1]
  }
  return event.target_ident || ''
}

// 使用 composable 统一的处理逻辑
function processAlertsLocal(events, windows) {
  return processAlertsComposable(events, windows)
}

// 旧的本地实现已废弃（保留代码便于回溯，实际不会执行）
function _deprecatedProcessAlertsLocal(events, windows) {
  const mwParsed = []
  for (const mw of (windows || [])) {
    const project = (mw.project || '').toUpperCase()
    const environment = (mw.environment || 'PROD').toUpperCase()
    const type = mw.maintenance_type
    // 解析匹配规则
    const matchRules = (mw.match_rules || '').split(',').map(s => s.trim().toUpperCase()).filter(Boolean)
    // 解析操作记录中的 IP
    const ops = (mw.operations || []).map(op => ({
      ip: (op.ip || '').trim(),
      timeStart: op.time_start || '',
      timeEnd: op.time_end || '',
      content: op.content || '',
    }))

    if (mw.repeat_mode === 'weekly' && mw.weekday && mw.time_start && mw.time_end) {
      // 展开每周重复：找出查询范围内所有匹配的星期
      // 遍历事件的时间范围，找出所有匹配的日期
      const now = new Date()
      const rangeStart = new Date(now.getFullYear(), 0, 1) // 从年初开始
      const rangeEnd = new Date(now.getFullYear(), 11, 31) // 到年底
      const [sh, sm] = (mw.time_start || '00:00').split(':').map(Number)
      const [eh, em] = (mw.time_end || '23:59').split(':').map(Number)
      for (let d = new Date(rangeStart); d <= rangeEnd; d.setDate(d.getDate() + 1)) {
        const jsDay = d.getDay() === 0 ? 7 : d.getDay() // 转成 1=Mon...7=Sun
        if (jsDay === mw.weekday) {
          const start = new Date(d)
          start.setHours(sh, sm, 0, 0)
          const end = new Date(d)
          end.setHours(eh, em, 0, 0)
          mwParsed.push({ project, environment, type, matchRules, ops, start: start.getTime() / 1000, end: end.getTime() / 1000 })
        }
      }
    } else {
      // 单次
      const start = new Date(mw.start_time).getTime() / 1000
      const end = new Date(mw.end_time).getTime() / 1000
      if (start && end) {
        mwParsed.push({ project, environment, type, matchRules, ops, start, end })
      }
    }
  }

  // 先分维护/非维护
  const mwEvents = []    // 维护窗口内的事件
  const nonMWEvents = [] // 非维护的事件

  for (const e of events) {
    let matchedMW = null
    const groupUpper = (e.group_name || '').toUpperCase()
    const ruleUpper = (e.rule_name || '').toUpperCase()
    const inst = extractInstance(e)
    for (const mw of mwParsed) {
      // 时间 + 项目 + 环境 匹配
      if (e.trigger_time >= mw.start && e.trigger_time <= mw.end &&
          groupUpper.includes(mw.project) && groupUpper.includes(mw.environment)) {
        // 匹配规则名过滤（不填则匹配所有）
        if (mw.matchRules.length > 0 && !mw.matchRules.some(r => ruleUpper.includes(r))) continue
        // 操作记录 IP 过滤（有操作记录则按 IP 精确匹配）
        if (mw.ops.length > 0 && !mw.ops.some(op => inst.includes(op.ip) && op.ip)) continue
        matchedMW = mw
        break
      }
    }
    if (matchedMW) {
      mwEvents.push({ event: e, mw: matchedMW })
    } else {
      nonMWEvents.push(e)
    }
  }

  // 维护窗口内：按 项目+窗口 合并为 1 条
  const mwAlerts = {}
  for (const { event, mw } of mwEvents) {
    const key = `${mw.project}_${mw.start}_${mw.end}`
    if (mwAlerts[key]) {
      mwAlerts[key].raw_count++
      if (!mwAlerts[key]._rules.has(event.rule_name)) {
        mwAlerts[key]._rules.add(event.rule_name)
      }
    } else {
      mwAlerts[key] = {
        rule_name: `${mw.project}维护`,
        severity: event.severity,
        trigger_time: mw.start,
        recover_time: mw.end,
        is_recovered: 1,
        group_name: event.group_name,
        instance: '',
        raw_count: 1,
        impact: '无影响',
        handler: '',
        status: '已处理',
        note: mw.type,
        is_maintenance: true,
        maintenance_type: mw.type,
        _rules: new Set([event.rule_name]),
      }
    }
  }
  // 把规则名拼上
  for (const a of Object.values(mwAlerts)) {
    const rules = [...a._rules]
    a.rule_name += ` (${rules.slice(0, 3).join(', ')}${rules.length > 3 ? '...' : ''})`
    delete a._rules
  }

  // 非维护：按 规则名 + 实例 去重，保存详情
  const dedupMap = {}
  for (const e of nonMWEvents) {
    const inst = extractInstance(e)
    const key = `${e.rule_name}__${inst}`
    const detail = { trigger_time: e.trigger_time, recover_time: e.recover_time, is_recovered: e.is_recovered, trigger_value: e.trigger_value }
    if (dedupMap[key]) {
      dedupMap[key].raw_count++
      dedupMap[key]._details.push(detail)
      if (e.trigger_time < dedupMap[key].trigger_time) dedupMap[key].trigger_time = e.trigger_time
      if (e.recover_time > dedupMap[key].recover_time) {
        dedupMap[key].recover_time = e.recover_time
      }
      if (!e.is_recovered) dedupMap[key].is_recovered = 0
    } else {
      dedupMap[key] = {
        rule_name: e.rule_name,
        severity: e.severity,
        trigger_time: e.trigger_time,
        recover_time: e.recover_time,
        is_recovered: e.is_recovered,
        group_name: e.group_name,
        instance: inst,
        raw_count: 1,
        _details: [detail],
        _raw: { rule_note: e.rule_note, tags: e.tags, annotations: e.annotations, cluster: e.cluster, target_ident: e.target_ident },
        impact: '',
        handler: '',
        status: e.is_recovered ? '已处理' : '',
        note: '',
        is_maintenance: false,
        maintenance_type: '',
      }
    }
  }

  // TcpPortDown 桌台归类：按 ID 前缀系列合并
  const deskAlerts = {}  // 按前缀分组
  const otherAlerts = [] // 非桌台告警
  for (const a of Object.values(dedupMap)) {
    if (a.rule_name.toUpperCase().includes('TCPPORTDOWN')) {
      // 从标签提取 id
      const idTag = (a._raw?.tags || []).find(t => /^id=/i.test(t))
      if (idTag) {
        const rawId = idTag.split('=')[1] || ''
        // 提取纯 ID：AGEU_D054 → D054, D050 → D050, N331 → N331
        const pureId = rawId.includes('_') ? rawId.split('_').pop() : rawId
        // 前缀字母：D054 → D, N331 → N
        const prefix = (pureId.match(/^([A-Za-z]+)/)?.[1] || '').toUpperCase()
        if (prefix) {
          const key = `__desk_${prefix}`
          if (deskAlerts[key]) {
            deskAlerts[key].raw_count += a.raw_count
            deskAlerts[key]._ids.add(pureId)
            deskAlerts[key]._details = deskAlerts[key]._details.concat(a._details || [])
            if (a.trigger_time < deskAlerts[key].trigger_time) deskAlerts[key].trigger_time = a.trigger_time
            if (a.recover_time > deskAlerts[key].recover_time) deskAlerts[key].recover_time = a.recover_time
            if (!a.is_recovered) deskAlerts[key].is_recovered = 0
          } else {
            deskAlerts[key] = {
              ...a,
              _ids: new Set([pureId]),
              _details: [...(a._details || [])],
            }
          }
          continue
        }
      }
    }
    otherAlerts.push(a)
  }
  // 合并桌台告警
  for (const a of Object.values(deskAlerts)) {
    const ids = [...a._ids].sort()
    const prefix = (ids[0]?.match(/^([A-Za-z]+)/)?.[1] || '').toUpperCase()
    if (ids.length === 1) {
      a.rule_name = `TcpPortDown - ${ids[0]}`
      a.note = `${ids[0]} 桌台维护`
    } else {
      a.rule_name = `TcpPortDown - ${prefix}系列 (${ids.length}台)`
      a.note = `${prefix}系列桌台维护 (${ids.slice(0, 5).join(', ')}${ids.length > 5 ? '...' : ''})`
    }
    a.impact = '无影响'
    a.status = '已处理'
    a.instance = ids.length === 1 ? a.instance : `${ids.length}台`
    delete a._ids
    otherAlerts.push(a)
  }

  const result = [...Object.values(mwAlerts), ...otherAlerts]
  result.sort((a, b) => b.trigger_time - a.trigger_time)
  return result
}

onMounted(async () => {
  await loadConnections()
  loadMW()
  loadBusiGroups()
  // 默认选中本周
  applyQuickDate({ label: '本周', type: 'week' })
})
</script>

<style scoped>
.alert-stats-page { max-width: 1200px; margin: 0 auto; padding: 24px; }
.page-header { margin-bottom: 24px; }
.page-header h2 { font-size: 20px; font-weight: 700; color: var(--text-primary); margin: 0 0 4px; }
.page-desc { font-size: 13px; color: var(--text-muted); margin: 0; }

.card { background: var(--bg-secondary); border: 1px solid var(--border); border-radius: 12px; padding: 20px; margin-bottom: 16px; }
.card-top { display: flex; align-items: center; justify-content: space-between; margin-bottom: 16px; }
.card-top h3 { font-size: 15px; font-weight: 600; color: var(--text-primary); margin: 0; display: flex; align-items: center; gap: 8px; }
.row-count { font-size: 12px; color: var(--text-muted); font-weight: 400; }

.date-section { display: flex; flex-direction: column; gap: 12px; }
.filter-row { display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap; }
.date-range { display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap; }
.date-field { display: flex; flex-direction: column; gap: 4px; }
.date-field label { font-size: 12px; color: var(--text-muted); font-weight: 500; }
.date-sep { color: var(--text-muted); padding-bottom: 8px; }

.input { padding: 8px 12px; background: var(--bg-input); border: 1px solid var(--border); border-radius: 8px; color: var(--text-primary); font-size: 13px; outline: none; transition: border-color 200ms; }
.input:focus { border-color: var(--primary-light); }

.btn { padding: 8px 16px; border: 1px solid var(--border); border-radius: 8px; background: var(--bg-secondary); color: var(--text-primary); cursor: pointer; font-size: 13px; transition: all 200ms; }
.btn:hover { background: var(--bg-hover); }
.btn-sm { padding: 6px 12px; font-size: 12px; }
.btn-primary { background: var(--primary); color: white; border-color: var(--primary); }
.btn-primary:hover { opacity: 0.9; }
.btn-primary:disabled { opacity: 0.5; cursor: not-allowed; }
.btn-danger { background: #dc2626; color: white; border-color: #dc2626; }
.btn-danger:hover { background: #b91c1c; }

.quick-dates { display: flex; gap: 4px; }

.fetch-status { margin-top: 12px; }
.progress-bar-wrap { width: 100%; height: 6px; background: var(--bg-tertiary); border-radius: 3px; overflow: hidden; margin-bottom: 6px; }
.progress-bar { height: 100%; background: var(--primary); border-radius: 3px; transition: width 0.3s ease; }
.progress-text { font-size: 12px; color: var(--text-muted); }

.toast { position: fixed; top: 20px; right: 20px; z-index: 9999; padding: 12px 20px; border-radius: 8px; font-size: 13px; max-width: 420px; cursor: pointer; box-shadow: 0 4px 12px rgba(0,0,0,0.3); }
.toast.error { background: #991b1b; color: #fecaca; border: 1px solid #dc2626; }
.toast.success { background: #166534; color: #bbf7d0; border: 1px solid #22c55e; }
.toast-fade-enter-active, .toast-fade-leave-active { transition: all 0.3s ease; }
.toast-fade-enter-from, .toast-fade-leave-to { opacity: 0; transform: translateY(-10px); }

.summary-card { }
.summary-text { font-size: 14px; color: var(--text-primary); margin: 0; line-height: 1.6; }
.rate-ok { color: #22c55e; }
.rate-warn { color: #f59e0b; }

.table-wrapper { overflow-x: auto; }
table { width: 100%; border-collapse: collapse; font-size: 13px; }
th { text-align: left; padding: 10px 12px; background: var(--bg-tertiary); color: var(--text-secondary); font-weight: 600; font-size: 12px; text-transform: uppercase; letter-spacing: 0.03em; white-space: nowrap; border-bottom: 1px solid var(--border); }
td { padding: 10px 12px; border-bottom: 1px solid var(--border-light, rgba(255,255,255,0.06)); vertical-align: top; }

.cell-input { width: 100%; padding: 6px 8px; background: transparent; border: 1px solid transparent; border-radius: 6px; color: var(--text-primary); font-size: 13px; outline: none; transition: all 200ms; }
.cell-input:hover { border-color: var(--border); }
.cell-input:focus { border-color: var(--primary-light); background: var(--bg-input); }
select.cell-input { cursor: pointer; }

.alert-name { font-weight: 500; color: var(--text-primary); }
.alert-meta { font-size: 11px; color: var(--text-muted); margin-top: 2px; }

.severity { display: inline-block; padding: 2px 8px; border-radius: 4px; font-size: 11px; font-weight: 600; }
.severity.s1 { background: rgba(239,68,68,0.15); color: #ef4444; }
.severity.s2 { background: rgba(245,158,11,0.15); color: #f59e0b; }
.severity.s3 { background: rgba(59,130,246,0.15); color: #3b82f6; }

.row-maintenance { background: rgba(34,197,94,0.05); }

.mw-form { background: var(--bg-tertiary); border-radius: 8px; padding: 16px; margin-bottom: 16px; }
.mw-form-row { display: flex; gap: 12px; flex-wrap: wrap; }
.mw-form-actions { display: flex; gap: 8px; margin-top: 12px; }
.mw-hint { font-size: 12px; color: var(--text-muted); margin: 0 0 12px; }

.mw-tag { display: inline-block; padding: 2px 8px; border-radius: 4px; font-size: 11px; font-weight: 500; }
.mw-tag.routine { background: rgba(59,130,246,0.15); color: #3b82f6; }
.mw-tag.temp { background: rgba(168,85,247,0.15); color: #a855f7; }
.mw-tag.urgent { background: rgba(245,158,11,0.15); color: #f59e0b; }

.no-data { color: var(--text-muted); font-size: 13px; padding: 12px 0; text-align: center; }

.btn-icon { background: none; border: none; cursor: pointer; font-size: 18px; line-height: 1; padding: 4px; border-radius: 4px; transition: all 200ms; }
.btn-icon.danger { color: var(--text-muted); }
.btn-icon.danger:hover { color: #ef4444; background: rgba(239,68,68,0.1); }
.action-cell { text-align: center; }
.top-actions { display: flex; gap: 8px; align-items: center; }
.severity-filter { display: flex; gap: 8px; align-items: center; }
.sev-check { display: flex; align-items: center; gap: 4px; cursor: pointer; font-size: 12px; }
.sev-check input { accent-color: var(--primary-light); }
.input-sm { padding: 5px 10px; font-size: 12px; }
.row-recovered { opacity: 0.7; }
.row-selected { background: rgba(59,130,246,0.08); }
.selected-count { font-size: 12px; color: var(--primary-light); font-weight: 500; }
.clickable { cursor: pointer; }
.expand-icon { font-size: 10px; color: var(--text-muted); margin-left: 4px; }
.detail-row td { padding: 0 12px 12px; background: var(--bg-tertiary); }
.detail-list { max-height: 200px; overflow-y: auto; }
.detail-item { display: flex; gap: 16px; align-items: center; padding: 4px 8px; font-size: 12px; color: var(--text-muted); border-bottom: 1px solid var(--border-light, rgba(255,255,255,0.04)); }
.detail-time { min-width: 200px; }
.detail-value { color: var(--text-secondary); }
.detail-ok { color: #22c55e; font-size: 11px; }
.detail-active { color: #ef4444; font-size: 11px; font-weight: 500; }
.detail-original { margin-top: 8px; padding: 10px; background: var(--bg-input); border-radius: 6px; font-size: 12px; }
.detail-section { margin-bottom: 6px; display: flex; flex-wrap: wrap; gap: 6px; align-items: center; }
.detail-section:last-child { margin-bottom: 0; }
.detail-label { color: var(--text-muted); font-weight: 500; }
.tag-item { display: inline-block; padding: 1px 6px; background: rgba(255,255,255,0.06); border-radius: 3px; font-size: 11px; font-family: var(--font-mono); }
.match-rules { font-size: 11px; font-family: var(--font-mono); color: var(--text-muted); }
.mw-ops-section { margin-top: 10px; }
.mw-ops-header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 6px; }
.mw-op-row { display: flex; gap: 8px; align-items: center; margin-bottom: 4px; }
.btn-xs { padding: 3px 8px; font-size: 11px; border: 1px solid var(--border); border-radius: 6px; background: var(--bg-secondary); color: var(--text-primary); cursor: pointer; }

.preview-card { }
.preview-content { padding: 16px; background: var(--bg-tertiary); border-radius: 8px; }
.preview-table { width: 100%; border-collapse: collapse; margin-top: 12px; font-size: 13px; }
.preview-table th, .preview-table td { padding: 8px 12px; border: 1px solid var(--border); text-align: left; }
.preview-table th { background: var(--bg-secondary); font-weight: 600; font-size: 12px; }
</style>
