<template>
  <Teleport to="body">
    <transition name="modal-fade">
      <div v-if="show" class="logs-overlay">
        <div class="logs-modal">
          <!-- 头部 -->
          <div class="logs-head">
            <div class="head-l">
              <el-icon><Document /></el-icon>
              <span class="title">容器启动日志 · {{ app }}</span>
            </div>
            <button class="head-close" @click="$emit('close')">
              <el-icon><Close /></el-icon>
            </button>
          </div>

          <!-- 状态横条（健康 = 绿；非健康 = 红，错误信息更醒目） -->
          <div v-if="currentPod" class="status-bar"
            :class="{ ok: isHealthy(currentPod), bad: !isHealthy(currentPod) }">
            <el-icon v-if="isHealthy(currentPod)"><CircleCheck /></el-icon>
            <el-icon v-else><Warning /></el-icon>
            <span>
              <template v-if="!isHealthy(currentPod)">
                <b>{{ currentPod.status_reason || currentPod.health || '异常' }}</b>
              </template>
              <template v-else-if="currentPod.status_reason">
                该容器 <b>{{ currentPod.status_reason }}</b>
              </template>
              <template v-else>
                状态：<b>{{ currentPod.health || '未知' }}</b>
              </template>
              <template v-if="currentPod.restart_count && currentPod.restart_count !== '0'">
                · 重启 <b>{{ currentPod.restart_count }}</b> 次
              </template>
              <template v-if="currentPod.containers_ready">
                · <b>{{ currentPod.containers_ready }}</b> ready
              </template>
            </span>
          </div>

          <!-- Pod 选择器 -->
          <div v-if="pods.length > 1" class="pod-selector">
            <span class="lbl">Pod 选择：</span>
            <button v-for="p in pods" :key="p.name"
              :class="['pod-chip', { on: p.name === podName, fail: p.health !== 'Healthy' }]"
              @click="selectPod(p.name)">
              <span class="dot"></span>
              <span class="name">{{ shortPodName(p.name) }}</span>
              <span v-if="p.status_reason" class="sub">{{ p.status_reason }}</span>
            </button>
          </div>

          <!-- 工具栏 -->
          <div class="logs-toolbar">
            <div class="tb-l">
              <span class="lbl">来源：</span>
              <button
                :class="['mode-btn', { on: mode === 'previous', dim: !tabHas('previous') }]"
                @click="switchMode('previous')" title="上一次崩溃前的容器日志（默认）">
                ⏮ 上次崩溃前
                <span v-if="!tabHas('previous')" class="empty-mark">空</span>
              </button>
              <button
                :class="['mode-btn', { on: mode === 'events', dim: !tabHas('events') }]"
                @click="switchMode('events')" title="K8s 事件（FailedScheduling / ImagePullBackOff 等无日志故障的线索）">
                📋 K8s 事件
                <span v-if="!tabHas('events')" class="empty-mark">空</span>
              </button>
              <button
                :class="['mode-btn', { on: mode === 'current', dim: !tabHas('current') }]"
                @click="switchMode('current')" title="当前容器最近日志">
                📺 当前容器
                <span v-if="!tabHas('current')" class="empty-mark">空</span>
              </button>
              <span v-if="logsSource === 'archive'" class="src-badge archive" title="发布失败时已存档，pod 被替换也能看">
                归档快照
              </span>
            </div>
            <div class="tb-r" v-if="mode !== 'events'">
              <span class="lbl">尾部行数：</span>
              <select v-model.number="tailLines" @change="reload" class="sel">
                <option :value="200">200</option>
                <option :value="500">500</option>
                <option :value="1000">1000</option>
                <option :value="2000">2000</option>
              </select>
            </div>
          </div>

          <!-- 搜索栏（仅 logs 模式显示） -->
          <div class="logs-search" v-if="mode !== 'events'">
            <el-icon><Search /></el-icon>
            <input v-model="searchQuery" placeholder="搜索关键字（panic / error / exception ...）"
              @keyup.enter="search" @input="onSearchInput" class="search-input" />
            <span v-if="searchMatches.length" class="match-count">
              {{ searchIdx + 1 }} / {{ searchMatches.length }}
            </span>
            <span v-else-if="searchQuery && !loading" class="no-match">无匹配</span>
            <button class="tb-btn" @click="prevMatch" :disabled="!searchMatches.length" title="上一个">
              <el-icon><ArrowUp /></el-icon>
            </button>
            <button class="tb-btn" @click="nextMatch" :disabled="!searchMatches.length" title="下一个">
              <el-icon><ArrowDown /></el-icon>
            </button>
            <span class="quick-keys">
              <button v-for="k in ['panic','error','fatal','exception','failed']" :key="k"
                class="quick-key" @click="quickSearch(k)">{{ k }}</button>
            </span>
            <button class="tb-btn primary" @click="jumpToError" title="自动定位最近一段错误">
              <el-icon><DArrowRight style="transform:rotate(90deg)" /></el-icon> 跳到错误段
            </button>
          </div>

          <!-- 日志/事件 区 -->
          <div ref="logArea" :class="['logs-area', mode === 'events' ? 'events-bg' : '']" v-loading="loading">
            <!-- Events tab：表格 -->
            <div v-if="mode === 'events' && !loading">
              <div v-if="!events.length" class="logs-empty">
                <el-icon size="32"><InfoFilled /></el-icon>
                <p>无 K8s 事件</p>
                <p class="empty-sub">{{ noEventsHint }}</p>
              </div>
              <table v-else class="events-table">
                <thead>
                  <tr>
                    <th style="width:80px;">类型</th>
                    <th style="width:180px;">原因</th>
                    <th>消息</th>
                    <th style="width:60px;text-align:center;">次数</th>
                    <th style="width:140px;">最近时间</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="(ev, idx) in events" :key="idx" :class="ev.type === 'Warning' ? 'ev-warn' : 'ev-norm'">
                    <td><span :class="['ev-type', ev.type === 'Warning' ? 'warn' : 'normal']">{{ ev.type }}</span></td>
                    <td class="mono">{{ ev.reason }}</td>
                    <td class="ev-msg">{{ ev.message }}</td>
                    <td style="text-align:center;font-family:'Fira Code',monospace;">{{ ev.count }}</td>
                    <td class="ev-time">{{ formatEventTime(ev.last_at) }}</td>
                  </tr>
                </tbody>
              </table>
            </div>

            <!-- Logs tab（previous / current）：文本 -->
            <pre v-else-if="logs && !loading" class="logs-pre" v-html="renderedLogs"></pre>
            <div v-else-if="!loading && error" class="logs-empty">
              <el-icon size="32"><Warning /></el-icon>
              <p>{{ error }}</p>
              <p class="empty-sub">{{ errorHint }}</p>
            </div>
            <div v-else-if="!loading && !logs" class="logs-empty">
              <el-icon size="32"><InfoFilled /></el-icon>
              <p>无可用日志</p>
              <p class="empty-sub">{{ noLogsHint }}</p>
            </div>
          </div>

          <!-- 底部 -->
          <div class="logs-foot">
            <div class="foot-l">
              <span v-if="logs" class="foot-info">
                共 <b>{{ totalLines }}</b> 行
                <span v-if="lastFetchedAt"> · 最后拉取于 {{ lastFetchedAtText }}</span>
              </span>
            </div>
            <div class="foot-r">
              <button class="tb-btn" @click="copyLogs" :disabled="!logs">
                <el-icon><DocumentCopy /></el-icon> 复制
              </button>
              <button class="tb-btn" @click="reload" :disabled="loading">
                <el-icon><Refresh /></el-icon> 刷新
              </button>
              <button class="tb-btn primary-fill" @click="$emit('close')">关闭</button>
            </div>
          </div>
        </div>
      </div>
    </transition>
  </Teleport>
</template>

<script setup>
import { ref, computed, watch, nextTick } from 'vue'
import { ElMessage } from 'element-plus'
import {
  Document, Close, CircleCheck, Warning, Search, ArrowUp, ArrowDown,
  Refresh, DocumentCopy, InfoFilled, DArrowRight,
} from '@element-plus/icons-vue'
import { getDeploymentPodLogs, getDeploymentPodEvents, getDeploymentArchivedPods } from '../api'

const props = defineProps({
  show: Boolean,
  deploymentId: { type: [Number, String], required: true },
  app: { type: String, required: true },
  // 失败信息（可选，用来设置默认 pod 选择）
  initialPodName: String,
})
const emit = defineEmits(['close'])

const pods = ref([])
const podName = ref('')
// mode: 'previous' (默认 — 失败排查首选) | 'current' (当前容器最近输出) | 'events' (k8s 事件)
const mode = ref('previous')
const tailLines = ref(200)
const logs = ref('')
const events = ref([])           // mode=events 时填充
const logsSource = ref('')       // 'archive' / 'live'
const loading = ref(false)
const error = ref('')
const errorHint = ref('')
const lastFetchedAt = ref(null)
// 每个 pod 各 tab 是否有归档内容；从 getDeploymentArchivedPods 拿
const podAvailability = ref({})  // { podName: { has_previous, has_current, has_events } }

const searchQuery = ref('')
const searchMatches = ref([]) // 行号数组
const searchIdx = ref(0)

const currentPod = computed(() => pods.value.find(p => p.name === podName.value) || null)
const totalLines = computed(() => logs.value ? logs.value.split('\n').length : 0)
const lastFetchedAtText = computed(() => {
  if (!lastFetchedAt.value) return ''
  const s = Math.floor((Date.now() - lastFetchedAt.value) / 1000)
  if (s < 60) return s + 's 前'
  return Math.floor(s / 60) + 'm 前'
})

const noLogsHint = computed(() => {
  if (mode.value === 'previous') {
    return '该 pod 没有归档「上次崩溃前」日志（容器没崩过，或失败决策时拉取失败）。'
  }
  return '该 pod 没有归档「当前容器」日志（fail 决策时容器无 stdout 输出）。'
})

const noEventsHint = computed(() => {
  return '该 pod 没有归档 k8s 事件（fail 决策时无 Warning/Normal 事件，或 ArgoCD 事件接口未返回）。'
})

// tabHas 判某 tab 在当前 pod 是否有归档内容；用于按钮 dim 灰显 + 自动跳避空
//   v103：历史只读归档，所以严格按 has_xxx flag 判断，没归档 = 该 tab 空
function tabHas(tab) {
  if (!currentPod.value) return false
  const av = podAvailability.value[currentPod.value.name]
  if (!av) return false
  if (tab === 'previous') return !!av.has_previous
  if (tab === 'current') return !!av.has_current
  if (tab === 'events') return !!av.has_events
  return false
}

// pickInitialMode 根据当前 pod 的可用 tab 选默认 mode
//   优先 previous（用户核心需求）；previous 没内容时按 events → current 顺延
function pickInitialMode() {
  if (tabHas('previous')) return 'previous'
  if (tabHas('events')) return 'events'
  if (tabHas('current')) return 'current'
  return 'previous'
}

function isHealthy(p) {
  if (!p) return false
  return p.health === 'Healthy' && (!p.status_reason || p.status_reason === 'Running')
}

function formatEventTime(iso) {
  if (!iso) return ''
  const t = new Date(iso).getTime()
  if (!t || isNaN(t)) return ''
  const sec = Math.floor((Date.now() - t) / 1000)
  if (sec < 60) return sec + 's 前'
  if (sec < 3600) return Math.floor(sec / 60) + 'm 前'
  if (sec < 86400) return Math.floor(sec / 3600) + 'h 前'
  return new Date(iso).toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })
}

// ---- 加载 pods ----
// 优先级：归档列表（pod 可能已被 k8s 收走，实时拿不到了）→ argocd 实时
async function loadPods() {
  if (!props.deploymentId || !props.app) return
  try {
    // 1. 先查归档（失败时已存的 pod 快照）
    // v103：历史「查看日志」严格只读归档。
    //   不再 merge 实时 pod chip（实时拉的是当前活着的 pod，不是这次 deploy 失败的 pod，
    //   展示给用户是误导）。
    let archived = []
    try {
      const ar = await getDeploymentArchivedPods(props.deploymentId, props.app)
      archived = ar.pods || []
    } catch { /* 忽略 */ }

    const availability = {}
    pods.value = archived.map(a => {
      availability[a.pod] = {
        has_previous: !!a.has_previous,
        has_current: !!a.has_current,
        has_events: !!a.has_events,
      }
      return {
        name: a.pod,
        namespace: '',
        health: 'Archived',
        status_reason: '已归档（' + a.captured_at + '）',
        restart_count: '',
        containers_ready: '',
        archived: true,
      }
    })
    podAvailability.value = availability

    if (pods.value.length === 0) {
      error.value = '该次发布无归档日志'
      errorHint.value = '可能原因：本次发布成功（成功的发布不归档）、MinIO 未配置、或后端 < v94。失败的发布请检查后端 [log-archive] 行日志诊断。'
      return
    }

    podName.value = props.initialPodName || pods.value[0].name
    mode.value = pickInitialMode()
    await fetchData()
  } catch (e) {
    error.value = '加载归档列表失败'
    errorHint.value = e?.response?.data?.message || e.message || ''
  }
}

// ---- 拉数据（按 mode 路由） ----
async function fetchData() {
  if (!podName.value || !currentPod.value) return
  loading.value = true
  error.value = ''
  errorHint.value = ''
  logs.value = ''
  events.value = []
  logsSource.value = ''
  try {
    if (mode.value === 'events') {
      const r = await getDeploymentPodEvents(props.deploymentId, {
        app: props.app,
        pod: podName.value,
        namespace: currentPod.value.namespace || '',
        pod_uid: currentPod.value.uid || '',
      })
      events.value = r.events || []
      logsSource.value = r.source || ''
    } else {
      const r = await getDeploymentPodLogs(props.deploymentId, {
        app: props.app,
        pod: podName.value,
        namespace: currentPod.value.namespace || '',
        previous: mode.value === 'previous',
        tail_lines: tailLines.value,
      })
      logs.value = r.logs || ''
      logsSource.value = r.source || ''
      // 拿到日志后自动跳到错误段
      nextTick(() => { if (logs.value) jumpToError(true) })
    }
    lastFetchedAt.value = Date.now()
  } catch (e) {
    const raw = e?.response?.data?.message || e.message || ''
    if (mode.value === 'previous' && /previous terminated container.*not found/i.test(raw)) {
      error.value = '该容器从未崩溃过'
      errorHint.value = '没有「上次崩溃前」的日志。试试「📋 K8s 事件」或「📺 当前容器」。'
    } else {
      error.value = mode.value === 'events' ? '拉取事件失败' : '拉取日志失败'
      errorHint.value = raw || '请稍后重试'
    }
  } finally {
    loading.value = false
  }
}

function selectPod(name) {
  if (podName.value === name) return
  podName.value = name
  // 切 pod 时按新 pod 的可用性重置 mode（避免选到空 tab）
  mode.value = pickInitialMode()
  fetchData()
}

function switchMode(toMode) {
  if (mode.value === toMode) return
  mode.value = toMode
  fetchData()
}

function reload() {
  fetchData()
}

// ---- 搜索 ----
function onSearchInput() {
  if (!searchQuery.value) {
    searchMatches.value = []
    searchIdx.value = 0
  }
}
function search() {
  if (!searchQuery.value || !logs.value) {
    searchMatches.value = []
    return
  }
  const q = searchQuery.value.toLowerCase()
  const lines = logs.value.split('\n')
  const matches = []
  lines.forEach((line, i) => {
    if (line.toLowerCase().includes(q)) matches.push(i)
  })
  searchMatches.value = matches
  searchIdx.value = 0
  if (matches.length > 0) scrollToLine(matches[0])
}
function quickSearch(kw) {
  searchQuery.value = kw
  search()
}
function nextMatch() {
  if (!searchMatches.value.length) return
  searchIdx.value = (searchIdx.value + 1) % searchMatches.value.length
  scrollToLine(searchMatches.value[searchIdx.value])
}
function prevMatch() {
  if (!searchMatches.value.length) return
  searchIdx.value = (searchIdx.value - 1 + searchMatches.value.length) % searchMatches.value.length
  scrollToLine(searchMatches.value[searchIdx.value])
}

const ERROR_REGEX = /\b(panic|fatal|error|exception|failed|traceback)\b/i
function jumpToError(silent = false) {
  if (!logs.value) return
  const lines = logs.value.split('\n')
  // 从尾部往上找第一个匹配
  let hit = -1
  for (let i = lines.length - 1; i >= 0; i--) {
    if (ERROR_REGEX.test(lines[i])) { hit = i; break }
  }
  if (hit < 0) {
    if (!silent) ElMessage.info('未在已加载日志中发现典型错误关键字')
    return
  }
  scrollToLine(hit)
}

const logArea = ref(null)
function scrollToLine(lineIdx) {
  nextTick(() => {
    if (!logArea.value) return
    const target = logArea.value.querySelector(`[data-line="${lineIdx}"]`)
    if (target) {
      target.scrollIntoView({ behavior: 'smooth', block: 'center' })
    }
  })
}

// ---- 渲染（高亮搜索词 + 错误关键字） ----
const renderedLogs = computed(() => {
  if (!logs.value) return ''
  const lines = logs.value.split('\n')
  const q = searchQuery.value
  const currentMatchLine = searchMatches.value[searchIdx.value]
  return lines.map((line, idx) => {
    let html = escapeHtml(line)
    // 错误关键字红色高亮
    html = html.replace(
      /\b(panic|fatal|error|exception|failed|traceback)\b/gi,
      '<span class="hl-err">$1</span>'
    )
    // 搜索词黄底
    if (q) {
      const re = new RegExp('(' + escapeRegExp(q) + ')', 'gi')
      html = html.replace(re, '<span class="hl-search">$1</span>')
    }
    const cls = idx === currentMatchLine ? 'log-line current-match' : 'log-line'
    return `<div class="${cls}" data-line="${idx}">${html || '&nbsp;'}</div>`
  }).join('')
})

function escapeHtml(s) {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
}
function escapeRegExp(s) {
  return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

function shortPodName(name) {
  if (!name) return ''
  // pod 名通常是 deployment-hashA-hashB 格式，截短中间 hashA
  const parts = name.split('-')
  if (parts.length < 3) return name
  return parts.slice(0, -2).join('-') + '...' + parts.slice(-1).join('-')
}

async function copyLogs() {
  let text = ''
  if (mode.value === 'events') {
    if (!events.value.length) return
    text = events.value.map(e => `[${e.type}] ${e.reason} (×${e.count})  ${e.message}  · ${formatEventTime(e.last_at)}`).join('\n')
  } else {
    if (!logs.value) return
    text = logs.value
  }
  try {
    await navigator.clipboard.writeText(text)
    ElMessage.success(mode.value === 'events' ? '事件已复制到剪贴板' : '日志已复制到剪贴板')
  } catch {
    ElMessage.error('复制失败，请手动选中')
  }
}

// 弹窗打开时加载
watch(() => props.show, (v) => {
  if (v) {
    pods.value = []
    podName.value = ''
    logs.value = ''
    events.value = []
    logsSource.value = ''
    error.value = ''
    searchQuery.value = ''
    searchMatches.value = []
    mode.value = 'previous'   // 默认 — loadPods 完成后 pickInitialMode 会按可用性微调
    tailLines.value = 200
    podAvailability.value = {}
    loadPods()
  }
})
</script>

<style scoped>
.logs-overlay {
  position: fixed; inset: 0;
  background: rgba(15, 23, 42, .55);
  backdrop-filter: blur(2px);
  display: flex; align-items: center; justify-content: center;
  z-index: 2000;
}
.logs-modal {
  width: 880px; max-width: 95vw;
  height: 720px; max-height: 92vh;
  background: #fff; border-radius: 12px;
  display: flex; flex-direction: column; overflow: hidden;
  box-shadow: 0 12px 40px rgba(0,0,0,.25);
}

.logs-head {
  display: flex; justify-content: space-between; align-items: center;
  padding: 14px 18px; border-bottom: 1px solid #e5e7eb;
  background: #f8fafc;
}
.head-l { display: flex; align-items: center; gap: 8px; font-size: 14px; }
.head-l .title { font-weight: 600; color: #1f2937; font-family: 'Fira Code', monospace; }
.head-close {
  background: none; border: none; cursor: pointer; padding: 4px;
  color: #6b7280; border-radius: 4px;
}
.head-close:hover { background: #e5e7eb; color: #1f2937; }

.status-bar {
  padding: 10px 18px;
  display: flex; align-items: center; gap: 8px;
  font-size: 13px;
}
.status-bar.ok { background: #ecfdf5; color: #047857; }
.status-bar.warn { background: #fffbeb; color: #92400e; }
.status-bar.bad { background: #fef2f2; color: #b91c1c; font-weight: 500; }
.status-bar.bad b { color: #991b1b; }
.status-bar b { font-family: 'Fira Code', monospace; }

.pod-selector {
  display: flex; align-items: center; gap: 8px;
  padding: 10px 18px; border-bottom: 1px solid #f1f5f9;
  flex-wrap: wrap; font-size: 12.5px;
}
.pod-selector .lbl { color: #6b7280; flex-shrink: 0; }
.pod-chip {
  display: inline-flex; align-items: center; gap: 6px;
  padding: 5px 12px; border: 1px solid #e5e7eb; border-radius: 99px;
  background: #fff; cursor: pointer; font-size: 12px;
  transition: all .15s;
}
.pod-chip:hover { border-color: #1890ff; }
.pod-chip.on { border-color: #1890ff; background: #eff6ff; color: #1d4ed8; }
.pod-chip.on.fail { border-color: #ef4444; background: #fef2f2; color: #b91c1c; }
.pod-chip .dot { width: 6px; height: 6px; border-radius: 50%; background: #10b981; }
.pod-chip.fail .dot { background: #ef4444; }
.pod-chip .name { font-family: 'Fira Code', monospace; }
.pod-chip .sub { font-size: 10.5px; color: #94a3b8; }
.pod-chip.fail .sub { color: #fca5a5; }

.logs-toolbar {
  display: flex; justify-content: space-between; align-items: center;
  padding: 10px 18px; border-bottom: 1px solid #f1f5f9;
  font-size: 12.5px; gap: 10px; flex-wrap: wrap;
}
.tb-l, .tb-r { display: flex; align-items: center; gap: 8px; }
.lbl { color: #6b7280; }
.src-tag { font-weight: 500; color: #1f2937; font-size: 12.5px; }
.mode-btn {
  padding: 4px 12px; border: 1px solid #d1d5db; border-radius: 4px;
  background: #fff; cursor: pointer; font-size: 12px; color: #374151;
  transition: all .12s;
}
.mode-btn:hover { border-color: #1890ff; color: #1890ff; }
.mode-btn.on { background: #1890ff; border-color: #1890ff; color: #fff; }
.mode-btn.dim { color: #94a3b8; background: #fafbfc; }
.mode-btn.dim.on { background: #94a3b8; border-color: #94a3b8; color: #fff; }
.mode-btn .empty-mark {
  margin-left: 4px; font-size: 9.5px; padding: 0 4px;
  background: #e5e7eb; color: #6b7280; border-radius: 99px;
  font-family: 'Fira Code', monospace;
}
.mode-btn.on .empty-mark { background: rgba(255,255,255,.25); color: #fff; }
.src-badge {
  font-size: 11px; padding: 2px 8px; border-radius: 99px;
  font-weight: 500; display: inline-flex; align-items: center; gap: 3px;
}
.src-badge.archive { background: #ecfdf5; color: #059669; border: 1px solid #a7f3d0; }
.src-badge.live { background: #eff6ff; color: #1d4ed8; border: 1px solid #93c5fd; }
.radio { display: inline-flex; align-items: center; gap: 4px; cursor: pointer; }
.radio input { margin: 0; cursor: pointer; }
.sel {
  padding: 4px 10px; border: 1px solid #d1d5db; border-radius: 4px;
  background: #fff; font-size: 12.5px;
}

.logs-search {
  display: flex; align-items: center; gap: 6px;
  padding: 8px 18px; border-bottom: 1px solid #f1f5f9;
  background: #fafbfc; flex-wrap: wrap;
}
.logs-search > .el-icon { color: #6b7280; }
.search-input {
  flex: 1; min-width: 200px;
  padding: 5px 10px; border: 1px solid #d1d5db; border-radius: 4px;
  background: #fff; font-size: 12.5px;
}
.search-input:focus { outline: none; border-color: #1890ff; }
.match-count, .no-match { font-size: 11px; color: #6b7280; font-family: 'Fira Code', monospace; }
.no-match { color: #ef4444; }
.tb-btn {
  display: inline-flex; align-items: center; gap: 4px;
  padding: 4px 10px; border: 1px solid #d1d5db; border-radius: 4px;
  background: #fff; cursor: pointer; font-size: 12px; color: #374151;
}
.tb-btn:hover:not(:disabled) { border-color: #1890ff; color: #1890ff; }
.tb-btn:disabled { opacity: .4; cursor: not-allowed; }
.tb-btn.primary { background: #eff6ff; border-color: #93c5fd; color: #1d4ed8; }
.tb-btn.primary:hover { background: #dbeafe; }
.tb-btn.primary-fill { background: #1890ff; border-color: #1890ff; color: #fff; }
.tb-btn.primary-fill:hover { background: #0f7ce6; }
.quick-keys { display: inline-flex; gap: 4px; margin-left: 6px; }
.quick-key {
  padding: 2px 8px; border: 1px solid #e5e7eb; border-radius: 99px;
  background: #fff; cursor: pointer; font-size: 11px; color: #6b7280;
  font-family: 'Fira Code', monospace;
}
.quick-key:hover { border-color: #ef4444; color: #ef4444; }

.logs-area {
  flex: 1; overflow: auto;
  background: #1e1e1e; color: #d4d4d4;
}
.logs-area.events-bg { background: #fff; color: #1f2937; }

.events-table {
  width: 100%; border-collapse: collapse;
  font-size: 12.5px; font-family: 'Inter', sans-serif;
}
.events-table thead th {
  position: sticky; top: 0; z-index: 1;
  background: #f9fafb; border-bottom: 1px solid #e5e7eb;
  text-align: left; padding: 8px 12px;
  color: #6b7280; font-weight: 500; font-size: 11px;
  text-transform: uppercase; letter-spacing: .4px;
}
.events-table tbody td {
  padding: 9px 12px; border-bottom: 1px solid #f1f5f9;
  vertical-align: top;
}
.events-table tr.ev-warn td { background: #fef9f9; }
.events-table tr.ev-warn td:first-child { border-left: 3px solid #ef4444; padding-left: 9px; }
.events-table tr.ev-norm td:first-child { border-left: 3px solid #d1d5db; padding-left: 9px; }
.ev-type {
  display: inline-block; padding: 2px 8px; border-radius: 99px;
  font-size: 10.5px; font-weight: 600; font-family: 'Fira Code', monospace;
}
.ev-type.warn { background: #fef2f2; color: #b91c1c; }
.ev-type.normal { background: #f3f4f6; color: #374151; }
.events-table .mono { font-family: 'Fira Code', monospace; font-size: 12px; color: #1f2937; font-weight: 500; }
.events-table .ev-msg { color: #374151; line-height: 1.55; }
.events-table .ev-time { color: #6b7280; font-family: 'Fira Code', monospace; font-size: 11.5px; }
.logs-pre {
  margin: 0; padding: 10px 14px;
  font-family: 'Fira Code', 'Consolas', monospace;
  font-size: 12.5px; line-height: 1.7;
  white-space: pre-wrap; word-break: break-all;
}
.logs-pre :deep(.log-line) {
  display: block; padding: 0 6px;
  border-left: 2px solid transparent;
}
.logs-pre :deep(.log-line.current-match) {
  background: rgba(252, 211, 77, .15);
  border-left-color: #fbbf24;
}
.logs-pre :deep(.hl-err) {
  color: #fca5a5; font-weight: 600;
}
.logs-pre :deep(.hl-search) {
  background: #fde047; color: #1f2937;
  border-radius: 2px;
}

.logs-empty {
  display: flex; flex-direction: column; align-items: center; justify-content: center;
  height: 100%; gap: 10px; color: #94a3b8;
}
.logs-empty p { margin: 0; font-size: 14px; }
.logs-empty .empty-sub { font-size: 12px; max-width: 480px; text-align: center; line-height: 1.6; }

.logs-foot {
  display: flex; justify-content: space-between; align-items: center;
  padding: 10px 18px; border-top: 1px solid #e5e7eb;
  background: #fafbfc;
}
.foot-l .foot-info { font-size: 12px; color: #6b7280; }
.foot-l .foot-info b { font-family: 'Fira Code', monospace; color: #374151; }
.foot-r { display: flex; gap: 8px; }

.modal-fade-enter-active, .modal-fade-leave-active { transition: opacity .15s ease; }
.modal-fade-enter-from, .modal-fade-leave-to { opacity: 0; }
.modal-fade-enter-active .logs-modal,
.modal-fade-leave-active .logs-modal { transition: transform .2s ease; }
.modal-fade-enter-from .logs-modal,
.modal-fade-leave-to .logs-modal { transform: scale(.96); }
</style>
