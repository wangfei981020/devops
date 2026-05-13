import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { ElNotification } from 'element-plus'
import { getDeployment, listDeployments } from '../api'

const POLL_INTERVAL_MS = 3000
const MAX_POLL_DURATION_MS = 10 * 60 * 1000
const RECOVER_SINCE = '30m'

const ACTION_LABEL = {
  update_image: '批量更新镜像',
  restart: '重启服务',
  rollback: '回滚发布',
  vm_rsync: 'VM rsync',
  vm_update_version: 'VM 更新',
  vm_rsync_and_update: 'VM rsync + 更新',
}

const STATUS_LABEL = {
  pending: '执行中',
  success: '成功',
  failed: '失败',
  partial: '部分成功',
  no_change: '无变更',
  unknown: '未知（后端可能重启过）',
}

const STATUS_TYPE = {
  success: 'success',
  failed: 'error',
  partial: 'warning',
  no_change: 'info',
  unknown: 'warning',
}

// 全局发布任务追踪：跨页面、跨刷新（onMounted 时 recover）保留 in-flight 状态。
// 设计目标：用户提交发布后离开页面 / 切走 / 误关，进度仍可追回；完成后强提示。
export const useDeploymentsStore = defineStore('deployments', () => {
  // inflight = [{id, action, envName, envType, modules, operator, startedAt, status, _timer, _started}]
  const inflight = ref([])
  const expanded = ref(false)

  const count = computed(() => inflight.value.length)
  const hasInflight = computed(() => count.value > 0)

  function findIndex(id) {
    return inflight.value.findIndex(d => d.id === id)
  }

  // startTracking 从提交动作发起；调用方传入已知 metadata 立即显示，再 polling 拿真实 status
  function startTracking(deploymentID, meta = {}) {
    if (!deploymentID || findIndex(deploymentID) >= 0) return
    const item = {
      id: deploymentID,
      action: meta.action || 'update_image',
      envName: meta.envName || '',
      // VM env_type 来自后端是大写（UAT/LPT/PROD），统一小写做 chip CSS class
      envType: (meta.envType || 'uat').toLowerCase(),
      modules: meta.modules || 0,
      operator: meta.operator || '',
      status: 'pending',
      startedAt: Date.now(),
      _started: Date.now(),
      _timer: null,
    }
    inflight.value.push(item)
    schedulePoll(item)
  }

  function schedulePoll(item) {
    if (item._timer) clearTimeout(item._timer)
    item._timer = setTimeout(() => pollOne(item), POLL_INTERVAL_MS)
  }

  async function pollOne(item) {
    // 防御：超时仍 pending，标记 unknown 但保留显示一会儿
    if (Date.now() - item._started > MAX_POLL_DURATION_MS) {
      item.status = 'unknown'
      finalize(item)
      return
    }
    try {
      const d = await getDeployment(item.id)
      // backend 字段是 snake_case，store 内统一字段
      item.status = d.status || 'pending'
      item.action = d.action || item.action
      if (d.module_names?.length) item.modules = d.module_names.length
      item.operator = d.operator || item.operator
      if (item.status === 'pending') {
        schedulePoll(item)
      } else {
        finalize(item)
      }
    } catch (_) {
      // 网络瞬时失败不要立刻终止，再试一次
      schedulePoll(item)
    }
  }

  function finalize(item) {
    if (item._timer) {
      clearTimeout(item._timer)
      item._timer = null
    }
    notifyFinal(item)
    // 完成 8s 后从列表移走，保留短时间让用户能看到结果再消失
    setTimeout(() => {
      const i = findIndex(item.id)
      if (i >= 0) inflight.value.splice(i, 1)
    }, 8000)
  }

  function notifyFinal(item) {
    const action = ACTION_LABEL[item.action] || item.action
    const status = STATUS_LABEL[item.status] || item.status
    const type = STATUS_TYPE[item.status] || 'info'
    // VM 行表达成"服务"更准确（K8s 才是模块）
    const unit = (item.action || '').startsWith('vm_') ? '服务' : '模块'
    ElNotification({
      title: `${action} · ${item.envName || '#' + item.id}`,
      message: `${status}（${item.modules} 个${unit}） · 点击「发布历史」查看详情`,
      type,
      duration: 6000,
      position: 'bottom-right',
    })
  }

  // recoverInflight 进入页面时调用，把"我自己最近 30 分钟未完成"自动接上
  // 用途：用户刚才提交后关掉浏览器/换设备/login 重进，能继续看到进度
  async function recoverInflight() {
    try {
      const r = await listDeployments({
        operator: 'me',
        status: 'pending',
        since: RECOVER_SINCE,
        page_size: 20,
      })
      const list = r?.list || []
      list.forEach(d => {
        if (findIndex(d.id) >= 0) return
        startTracking(d.id, {
          action: d.action,
          modules: (d.module_names || []).length,
          operator: d.operator,
        })
      })
    } catch (_) {
      // 静默：恢复失败不打扰用户，下次进入再试
    }
  }

  function clearAll() {
    inflight.value.forEach(it => it._timer && clearTimeout(it._timer))
    inflight.value = []
  }

  function toggleExpand() { expanded.value = !expanded.value }

  return {
    inflight, expanded, count, hasInflight,
    startTracking, recoverInflight, clearAll, toggleExpand,
  }
})
