<template>
  <transition name="dock-fade">
    <div v-if="store.hasInflight" class="dock" :class="['status-' + overallStatus, { expanded: store.expanded }]">
      <!-- 折叠态：mini bar -->
      <div v-if="!store.expanded" class="dock-bar" @click="store.toggleExpand">
        <div v-if="overallStatus === 'pending'" class="spin"></div>
        <el-icon v-else-if="overallStatus === 'success'" class="status-ico ok"><CircleCheckFilled /></el-icon>
        <el-icon v-else class="status-ico fail"><CircleCloseFilled /></el-icon>
        <div class="bar-text">
          <b>{{ store.count }}</b> {{ headText }}
          <span class="bar-tip">点击展开</span>
        </div>
        <el-icon class="bar-arrow"><ArrowUp /></el-icon>
      </div>

      <!-- 展开态：列表 -->
      <div v-else class="dock-panel">
        <div class="dock-head">
          <div class="head-title">
            <div v-if="overallStatus === 'pending'" class="spin"></div>
            <el-icon v-else-if="overallStatus === 'success'" class="status-ico ok"><CircleCheckFilled /></el-icon>
            <el-icon v-else class="status-ico fail"><CircleCloseFilled /></el-icon>
            <span><b>{{ store.count }}</b> {{ headText }}</span>
          </div>
          <el-icon class="head-close" @click="store.toggleExpand"><ArrowDown /></el-icon>
        </div>
        <div class="dock-body">
          <div v-for="d in store.inflight" :key="d.id" class="dock-item" :class="['st-' + d.status]">
            <div class="it-l">
              <div class="it-title">
                <span class="it-action">{{ actionLabel(d.action) }}</span>
                <span class="it-id">#{{ d.id }}</span>
              </div>
              <div class="it-meta">
                <span v-if="d.envName" class="it-env" :class="d.envType">{{ d.envName }}</span>
                <span v-if="d.modules" class="it-mod">{{ d.modules }} 个模块</span>
                <span class="it-time">{{ elapsed(d.startedAt) }}</span>
              </div>
            </div>
            <div class="it-r">
              <template v-if="d.status === 'pending'">
                <div class="dot-spin"></div>
                <button class="cancel-btn" :disabled="cancelingIds.has(d.id)" @click.stop="onCancel(d)" title="停止等待，已提交的改动不会回滚">
                  {{ cancelingIds.has(d.id) ? '取消中…' : '取消等待' }}
                </button>
              </template>
              <span v-else class="status-pill" :class="'st-' + d.status">{{ statusLabel(d.status) }}</span>
            </div>
          </div>
        </div>
        <div class="dock-foot">
          <span class="foot-tip">完成的任务会自动消失 · <router-link to="/history">查看完整发布历史</router-link></span>
        </div>
      </div>
    </div>
  </transition>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted, reactive } from 'vue'
import { ArrowUp, ArrowDown, CircleCheckFilled, CircleCloseFilled } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useDeploymentsStore } from '../stores/deployments'
import { cancelDeployment } from '../api'

const store = useDeploymentsStore()

// 正在取消中的 id 集合（防止重复点）
const cancelingIds = reactive(new Set())

async function onCancel(d) {
  if (cancelingIds.has(d.id)) return
  try {
    await ElMessageBox.confirm(
      '代码已提交仓库，后台同步会继续进行。\n取消后只是不再等待状态反馈。\n\n如果 30 分钟内仍未完成，请人工检查对应服务的 Pod 是否已正常启动。',
      '确认取消等待？',
      {
        type: 'warning',
        confirmButtonText: '确定取消',
        cancelButtonText: '继续等待',
        dangerouslyUseHTMLString: false,
        autofocus: false,
      }
    )
  } catch (_) {
    return // 用户选了"继续等待"
  }
  cancelingIds.add(d.id)
  try {
    const r = await cancelDeployment(d.id)
    if (r?.result === 'stale') {
      ElMessage.info('该任务已结束，无需取消')
    } else {
      ElMessage.success('已取消等待 · 后台同步会继续进行')
    }
  } catch (e) {
    ElMessage.error(e?.response?.data?.message || e.message || '取消失败')
  } finally {
    cancelingIds.delete(d.id)
  }
}

// 整体状态：有 pending 就算进行中；全完成看有没有失败
const overallStatus = computed(() => {
  if (store.inflight.some(d => d.status === 'pending')) return 'pending'
  if (store.inflight.some(d => d.status === 'failed' || d.status === 'partial' || d.status === 'unknown')) return 'failed'
  return 'success'
})

const headText = computed(() => {
  if (overallStatus.value === 'pending') return '个发布任务进行中'
  if (overallStatus.value === 'success') return '个任务已完成'
  return '个任务有失败'
})

const ACTION_LABEL = {
  update_image: '更新镜像',
  restart: '重启服务',
  rollback: '回滚',
}
function actionLabel(a) { return ACTION_LABEL[a] || a }

const STATUS_LABEL = {
  pending: '执行中',
  success: '成功',
  failed: '失败',
  partial: '部分成功',
  no_change: '无变更',
  unknown: '未知',
  canceled: '已取消',
}
function statusLabel(s) { return STATUS_LABEL[s] || s }

// 每秒驱动 elapsed 重新计算（让"已 25 秒"实时更新）
const tick = ref(0)
let timer = null
onMounted(() => { timer = setInterval(() => { tick.value++ }, 1000) })
onUnmounted(() => { if (timer) clearInterval(timer) })

function elapsed(start) {
  // 触发响应：tick 必须出现在表达式里
  void tick.value
  const s = Math.floor((Date.now() - start) / 1000)
  if (s < 60) return `已 ${s}s`
  return `已 ${Math.floor(s / 60)}m ${s % 60}s`
}
</script>

<style scoped>
.dock {
  position: fixed; top: 60px; right: 24px; z-index: 1500;
  font-family: var(--body, 'Inter', sans-serif);
  /* 不让面板挤压内容；展开后下方阴影交代层次 */
  pointer-events: none;
}
.dock > * { pointer-events: auto; }

/* 折叠条 */
.dock-bar {
  display: flex; align-items: center; gap: 10px;
  background: #fff; border: 1px solid #e5e7eb; border-radius: 999px;
  padding: 9px 14px 9px 12px;
  box-shadow: 0 6px 20px rgba(15, 23, 42, .12);
  cursor: pointer; transition: all .15s;
  font-size: 12.5px; color: #1f2937;
}
.dock-bar:hover { box-shadow: 0 8px 24px rgba(24, 144, 255, .2); border-color: #1890ff; }
.dock-bar b { color: #1890ff; font-family: var(--mono, 'Fira Code', monospace); font-weight: 700; margin: 0 2px; }

/* 根据整体状态调色（折叠/展开共用） */
.dock.status-success .dock-bar { border-color: #10b981; }
.dock.status-success .dock-bar b,
.dock.status-success .head-title b { color: #059669; }
.dock.status-failed .dock-bar { border-color: #ef4444; }
.dock.status-failed .dock-bar b,
.dock.status-failed .head-title b { color: #dc2626; }

.status-ico { font-size: 16px; flex-shrink: 0; }
.status-ico.ok { color: #10b981; }
.status-ico.fail { color: #ef4444; }
.bar-tip { color: #9ca3af; font-size: 11px; margin-left: 6px; }
.bar-arrow { color: #6b7280; font-size: 12px; }

/* 展开面板 */
.dock-panel {
  width: 360px; max-height: 70vh;
  background: #fff; border: 1px solid #e5e7eb; border-radius: 10px;
  box-shadow: 0 10px 30px rgba(15, 23, 42, .15);
  display: flex; flex-direction: column; overflow: hidden;
}
.dock-head {
  display: flex; align-items: center; justify-content: space-between;
  padding: 12px 16px; border-bottom: 1px solid #f1f3f5;
  background: #fafbfc;
}
.head-title { display: flex; align-items: center; gap: 10px; font-size: 13px; color: #1f2937; }
.head-title b { color: #1890ff; font-family: var(--mono, 'Fira Code', monospace); font-weight: 700; }
.head-close { color: #6b7280; cursor: pointer; font-size: 14px; }
.head-close:hover { color: #1890ff; }

.dock-body { flex: 1; overflow-y: auto; padding: 6px 0; }
.dock-item {
  display: flex; align-items: center; justify-content: space-between;
  padding: 10px 16px; border-bottom: 1px solid #f5f7fa;
  transition: background .12s;
}
.dock-item:hover { background: #fafbfc; }
.dock-item:last-child { border-bottom: none; }
.it-l { flex: 1; min-width: 0; }
.it-title { display: flex; align-items: center; gap: 8px; font-size: 12.5px; }
.it-action { color: #1f2937; font-weight: 600; }
.it-id { color: #9ca3af; font-family: var(--mono, 'Fira Code', monospace); font-size: 11px; }
.it-meta {
  display: flex; align-items: center; gap: 8px; flex-wrap: wrap;
  margin-top: 4px; font-size: 11px; color: #6b7280;
}
.it-env {
  font-family: var(--mono, 'Fira Code', monospace); font-weight: 600;
  padding: 1px 6px; border-radius: 3px; font-size: 10.5px;
}
.it-env.uat { background: #ecfdf5; color: #059669; }
.it-env.prod { background: #fef2f2; color: #dc2626; }
.it-mod { color: #6b7280; }
.it-time { color: #9ca3af; font-family: var(--mono, 'Fira Code', monospace); margin-left: auto; }
.it-r { margin-left: 12px; flex-shrink: 0; }

.status-pill {
  font-size: 10.5px; padding: 2px 8px; border-radius: 99px; font-weight: 600;
}
.status-pill.st-success { background: #ecfdf5; color: #059669; }
.status-pill.st-failed { background: #fef2f2; color: #dc2626; }
.status-pill.st-partial { background: #fffbeb; color: #b45309; }
.status-pill.st-no_change { background: #f3f4f6; color: #6b7280; }
.status-pill.st-unknown { background: #fff7ed; color: #ea580c; }
.status-pill.st-canceled { background: #f3f4f6; color: #4b5563; }

.it-r { display: flex; align-items: center; gap: 8px; }
.cancel-btn {
  font-size: 11px; padding: 3px 8px; border-radius: 4px;
  background: #fff; color: #6b7280; border: 1px solid #d1d5db;
  cursor: pointer; transition: all .12s;
}
.cancel-btn:hover:not(:disabled) { color: #dc2626; border-color: #fca5a5; background: #fef2f2; }
.cancel-btn:disabled { opacity: .5; cursor: not-allowed; }

.dock-foot {
  padding: 8px 16px; background: #fafbfc; border-top: 1px solid #f1f3f5;
  font-size: 11px; color: #9ca3af;
}
.dock-foot a { color: #1890ff; text-decoration: none; }
.dock-foot a:hover { text-decoration: underline; }

/* 旋转 spinner（小 / 中两种） */
.spin {
  width: 14px; height: 14px; border: 2px solid #bfdbfe; border-top-color: #1890ff;
  border-radius: 50%; animation: spin 1s linear infinite; flex-shrink: 0;
}
.dot-spin {
  width: 10px; height: 10px; border: 2px solid #bfdbfe; border-top-color: #1890ff;
  border-radius: 50%; animation: spin 1s linear infinite;
}
@keyframes spin { to { transform: rotate(360deg); } }

/* 入场/出场：从右侧滑入 */
.dock-fade-enter-active, .dock-fade-leave-active { transition: all .25s ease; }
.dock-fade-enter-from, .dock-fade-leave-to {
  opacity: 0;
  transform: translateX(20px);
}
</style>
