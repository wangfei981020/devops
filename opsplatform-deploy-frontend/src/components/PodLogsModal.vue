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

          <!-- 状态横条（pod 异常状态摘要） -->
          <div v-if="currentPod" class="status-bar"
            :class="{ ok: currentPod.health === 'Healthy', warn: currentPod.health !== 'Healthy' }">
            <el-icon v-if="currentPod.health === 'Healthy'"><CircleCheck /></el-icon>
            <el-icon v-else><Warning /></el-icon>
            <span>
              <template v-if="currentPod.status_reason">该容器 <b>{{ currentPod.status_reason }}</b></template>
              <template v-else>状态：<b>{{ currentPod.health || '未知' }}</b></template>
              <template v-if="currentPod.restart_count && currentPod.restart_count !== '0'">
                · 已重启 <b>{{ currentPod.restart_count }}</b> 次
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
              <span class="src-tag">上一次崩溃前的日志</span>
              <span v-if="logsSource === 'archive'" class="src-badge archive" title="发布失败时已存档到 MinIO，pod 被替换也能看">
                📦 归档快照
              </span>
              <span v-else-if="logsSource === 'live'" class="src-badge live" title="实时从 ArgoCD 拉取（pod 还在）">
                ⚡ 实时拉取
              </span>
            </div>
            <div class="tb-r">
              <span class="lbl">尾部行数：</span>
              <select v-model.number="tailLines" @change="reload" class="sel">
                <option :value="200">200</option>
                <option :value="500">500</option>
                <option :value="1000">1000</option>
                <option :value="2000">2000</option>
              </select>
            </div>
          </div>

          <!-- 搜索栏 -->
          <div class="logs-search">
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

          <!-- 日志区 -->
          <div ref="logArea" class="logs-area" v-loading="loading">
            <pre v-if="logs && !loading" class="logs-pre" v-html="renderedLogs"></pre>
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
import { getDeploymentPods, getDeploymentPodLogs, getDeploymentArchivedPods } from '../api'

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
const previous = ref(true) // 固定 true，发布工具只关心崩溃前日志
const tailLines = ref(200)
const logs = ref('')
const logsSource = ref('') // 'archive' 或 'live'
const loading = ref(false)
const error = ref('')
const errorHint = ref('')
const lastFetchedAt = ref(null)

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
  if (!currentPod.value) return ''
  const r = currentPod.value.status_reason || ''
  if (r.includes('ImagePullBackOff') || r.includes('ErrImagePull')) {
    return '容器从未启动成功（镜像拉取失败）。请检查镜像名 / Harbor 凭证 / 节点网络。'
  }
  if (r.includes('Pending')) {
    return 'Pod 还在 Pending 状态（资源不足 / 节点选择器 / PVC 等）。'
  }
  if (previous.value) {
    return '没有上一次容器日志（可能是首次部署）。可切换到「当前容器」查看。'
  }
  return '当前容器无输出。'
})

// ---- 加载 pods ----
// 优先级：归档列表（pod 可能已被 k8s 收走，实时拿不到了）→ argocd 实时
async function loadPods() {
  if (!props.deploymentId || !props.app) return
  try {
    // 1. 先查归档（失败时已存的 pod 快照）
    let archived = []
    try {
      const ar = await getDeploymentArchivedPods(props.deploymentId, props.app)
      archived = ar.pods || []
    } catch { /* 忽略 */ }

    // 2. 再拉实时 pod 列表（可能拿不到，比如 pod 已被替换）
    let livePods = []
    try {
      const r = await getDeploymentPods(props.deploymentId, props.app)
      livePods = r.pods || []
    } catch { /* 忽略 */ }

    // 3. 合并：归档的 pod 标记为「已存档」，实时 pod 用真实状态
    const map = {}
    for (const a of archived) {
      map[a.pod] = {
        name: a.pod,
        namespace: '', // 归档不需要 namespace
        health: 'Archived',
        status_reason: '已归档（' + a.captured_at + '）',
        restart_count: '',
        containers_ready: '',
        archived: true,
      }
    }
    for (const lp of livePods) {
      if (map[lp.name]) {
        // 同时存在 → 用实时状态覆盖（更新 health / restart count）
        Object.assign(map[lp.name], lp, { archived: true })
      } else {
        map[lp.name] = { ...lp, archived: false }
      }
    }
    pods.value = Object.values(map)
    if (pods.value.length === 0) {
      error.value = '未找到任何 pod'
      errorHint.value = '该次发布没有失败 pod 的归档日志，且 ArgoCD 也没有当前 pod 资源'
      return
    }
    // 默认选第一个失败/归档的
    const fail = pods.value.find(p => p.archived || (p.health && p.health !== 'Healthy'))
    podName.value = props.initialPodName || (fail ? fail.name : pods.value[0].name)
    await fetchLogs()
  } catch (e) {
    error.value = '加载 pod 列表失败'
    errorHint.value = e?.response?.data?.message || e.message || ''
  }
}

// ---- 拉日志 ----
async function fetchLogs() {
  if (!podName.value || !currentPod.value) return
  loading.value = true
  error.value = ''
  errorHint.value = ''
  try {
    const r = await getDeploymentPodLogs(props.deploymentId, {
      app: props.app,
      pod: podName.value,
      namespace: currentPod.value.namespace || '',
      previous: previous.value,
      tail_lines: tailLines.value,
    })
    logs.value = r.logs || ''
    logsSource.value = r.source || ''
    lastFetchedAt.value = Date.now()
    // 拿到日志后自动跳到错误段
    nextTick(() => {
      if (logs.value) jumpToError(true /*silent*/)
    })
  } catch (e) {
    logs.value = ''
    error.value = '拉取日志失败'
    errorHint.value = e?.response?.data?.message || e.message || '请稍后重试'
  } finally {
    loading.value = false
  }
}

function selectPod(name) {
  if (podName.value === name) return
  podName.value = name
  fetchLogs()
}

function reload() {
  fetchLogs()
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
  if (!logs.value) return
  try {
    await navigator.clipboard.writeText(logs.value)
    ElMessage.success('日志已复制到剪贴板')
  } catch {
    ElMessage.error('复制失败，请手动选中日志区')
  }
}

// 弹窗打开时加载
watch(() => props.show, (v) => {
  if (v) {
    pods.value = []
    podName.value = ''
    logs.value = ''
    logsSource.value = ''
    error.value = ''
    searchQuery.value = ''
    searchMatches.value = []
    previous.value = true
    tailLines.value = 200
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
