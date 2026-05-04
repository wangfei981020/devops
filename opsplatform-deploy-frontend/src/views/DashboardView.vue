<template>
  <div class="dash" v-loading="loading">
    <!-- ===== SSO 登录提示（portal 用户，5s 自动消失） ===== -->
    <transition name="fade">
      <div v-if="showPortalNotice" class="portal-notice">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="18" height="18">
          <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/>
          <polyline points="9 12 11 14 15 10"/>
        </svg>
        <span>SSO 登录成功，欢迎 <b>{{ auth.user?.username }}</b></span>
        <button class="notice-close" @click="showPortalNotice = false">&times;</button>
      </div>
    </transition>

    <!-- ===== KPI ===== -->
    <div class="kpi-row">
      <div class="kpi">
        <div class="k-head">
          <div class="k-icon" style="background:#eff6ff;color:#1d4ed8">
            <el-icon><Folder /></el-icon>
          </div>
          <div class="k-label">项目 / 环境</div>
        </div>
        <div class="k-value">
          <span class="main">{{ stats.kpi?.projects || 0 }}</span>
          <span class="divide">/</span>
          <span class="main">{{ stats.kpi?.envs_total || 0 }}</span>
        </div>
        <div class="k-sub">
          <span v-if="auth.hasAnyUatEnv" class="tag uat">UAT {{ stats.kpi?.envs_uat || 0 }}</span>
          <span v-if="auth.hasAnyProdEnv" class="tag prod">PROD {{ stats.kpi?.envs_prod || 0 }}</span>
        </div>
      </div>

      <div class="kpi">
        <div class="k-head">
          <div class="k-icon" style="background:#ecfdf5;color:#059669">
            <el-icon><Box /></el-icon>
          </div>
          <div class="k-label">K8s 模块总数</div>
        </div>
        <div class="k-value">
          <span class="main">{{ stats.kpi?.k8s_modules_total || 0 }}</span>
        </div>
        <div class="k-sub mono">实时根据扫描结果统计</div>
      </div>

      <div class="kpi">
        <div class="k-head">
          <div class="k-icon" style="background:#f5f3ff;color:#7c3aed">
            <el-icon><Connection /></el-icon>
          </div>
          <div class="k-label">VM 服务总数</div>
        </div>
        <div class="k-value">
          <span class="main">{{ stats.kpi?.vm_services_total || 0 }}</span>
        </div>
        <div class="k-sub mono">实时根据扫描结果统计</div>
      </div>

      <div class="kpi">
        <div class="k-head">
          <div class="k-icon" style="background:#fff7ed;color:#ea580c">
            <el-icon><Upload /></el-icon>
          </div>
          <div class="k-label">近 24h 发布</div>
        </div>
        <div class="k-value">
          <span class="main">{{ stats.kpi?.deployments_24h || 0 }}</span>
          <span class="unit">次</span>
        </div>
        <div class="k-sub">
          <span class="tag ok">成功 {{ stats.kpi?.deployments_24h_success || 0 }}</span>
          <span class="tag fail">失败 {{ stats.kpi?.deployments_24h_failed || 0 }}</span>
        </div>
      </div>

      <div class="kpi">
        <div class="k-head">
          <div class="k-icon" style="background:#fef2f2;color:#dc2626">
            <el-icon><User /></el-icon>
          </div>
          <div class="k-label">当前用户</div>
        </div>
        <div class="k-value">
          <span class="main user-name">{{ auth.user?.display_name || auth.user?.username || '-' }}</span>
        </div>
        <div class="k-sub mono">
          <span class="tag">{{ auth.user?.role || 'user' }}</span>
          <span class="tag" :class="auth.user?.auth_source === 'portal' ? 'prod' : ''">{{ auth.user?.auth_source || 'local' }}</span>
        </div>
      </div>
    </div>

    <!-- ===== 主区 ===== -->
    <div class="main-grid">
      <!-- 最近发布 -->
      <div class="card">
        <div class="card-head">
          <div class="card-title">
            <el-icon><Clock /></el-icon>
            最近发布
          </div>
          <RouterLink to="/history" class="link-more">查看全部 →</RouterLink>
        </div>
        <div class="card-body">
          <div v-if="!stats.recent_deployments?.length" class="empty">暂无发布记录</div>
          <div v-else class="dep-list">
            <div v-for="d in stats.recent_deployments" :key="d.id" class="dep-row" @click="goHistory(d.id)">
              <div class="dep-l">
                <span :class="'env-dot ' + d.env_type"></span>
                <div class="dep-info">
                  <div class="dep-top">
                    <span :class="'kind-chip ' + (d.kind || 'k8s')">{{ (d.kind || 'k8s') === 'vm' ? 'VM' : 'K8s' }}</span>
                    <span class="dep-env mono">{{ d.project_env_name }}</span>
                    <span class="dep-action">{{ actionLabel(d.action) }}</span>
                    <span class="dep-mods">{{ d.module_count }} 个{{ (d.kind || 'k8s') === 'vm' ? '服务' : '模块' }}</span>
                  </div>
                  <div class="dep-bot">
                    <span :class="'status ' + d.status">{{ statusLabel(d.status) }}</span>
                    <span class="dot">·</span>
                    <span class="op">{{ d.operator }}</span>
                    <span class="dot">·</span>
                    <span class="time">{{ fromNow(d.created_at) }}</span>
                    <span v-if="d.duration_sec > 0" class="dot">·</span>
                    <span v-if="d.duration_sec > 0" class="dur">{{ d.duration_sec }}s</span>
                  </div>
                </div>
              </div>
              <el-icon class="chev"><ArrowRight /></el-icon>
            </div>
          </div>
        </div>
      </div>

      <!-- 我的最近 -->
      <div class="card" v-if="showMyRecent">
        <div class="card-head">
          <div class="card-title">
            <el-icon><UserFilled /></el-icon>
            我的最近发布
          </div>
          <span class="card-sub">仅显示你的操作</span>
        </div>
        <div class="card-body">
          <div v-if="!stats.my_recent_deployments?.length" class="empty">你还没有发布过</div>
          <div v-else class="dep-list">
            <div v-for="d in stats.my_recent_deployments" :key="d.id" class="dep-row" @click="goHistory(d.id)">
              <div class="dep-l">
                <span :class="'env-dot ' + d.env_type"></span>
                <div class="dep-info">
                  <div class="dep-top">
                    <span :class="'kind-chip ' + (d.kind || 'k8s')">{{ (d.kind || 'k8s') === 'vm' ? 'VM' : 'K8s' }}</span>
                    <span class="dep-env mono">{{ d.project_env_name }}</span>
                    <span class="dep-action">{{ actionLabel(d.action) }}</span>
                    <span class="dep-mods">{{ d.module_count }} 个{{ (d.kind || 'k8s') === 'vm' ? '服务' : '模块' }}</span>
                  </div>
                  <div class="dep-bot">
                    <span :class="'status ' + d.status">{{ statusLabel(d.status) }}</span>
                    <span class="dot">·</span>
                    <span class="time">{{ fromNow(d.created_at) }}</span>
                    <span v-if="d.duration_sec > 0" class="dot">·</span>
                    <span v-if="d.duration_sec > 0" class="dur">{{ d.duration_sec }}s</span>
                  </div>
                </div>
              </div>
              <el-icon class="chev"><ArrowRight /></el-icon>
            </div>
          </div>
        </div>
      </div>

      <!-- 环境健康 -->
      <div class="card">
        <div class="card-head">
          <div class="card-title">
            <el-icon><Monitor /></el-icon>
            环境列表
          </div>
          <RouterLink to="/projects" class="link-more">去项目配置 →</RouterLink>
        </div>
        <div class="card-body">
          <div v-if="!stats.envs?.length" class="empty">还没有项目环境，去「项目配置」添加</div>
          <div v-else class="env-list">
            <div v-for="e in stats.envs" :key="(e.kind || 'k8s') + '-' + e.id" class="env-row">
              <span :class="'kind-chip ' + (e.kind || 'k8s')">{{ (e.kind || 'k8s') === 'vm' ? 'VM' : 'K8s' }}</span>
              <span :class="'env-chip ' + e.env_type">{{ e.env_type.toUpperCase() }}</span>
              <span class="env-name mono">{{ e.name }}</span>
              <span class="env-mods">{{ e.modules_total }} {{ (e.kind || 'k8s') === 'vm' ? '服务' : '模块' }}</span>
              <span class="env-sep"></span>
              <span class="env-last">
                <span v-if="e.last_deploy_at">
                  <span :class="'status ' + (e.last_deploy_status || 'pending')">
                    {{ statusLabel(e.last_deploy_status) || '-' }}
                  </span>
                  <span class="dot">·</span>
                  {{ fromNow(e.last_deploy_at) }}
                </span>
                <span v-else class="gray">从未发布</span>
              </span>
              <span class="env-scanned mono" v-if="e.last_scanned_at">
                扫描 {{ fromNow(e.last_scanned_at) }}
              </span>
              <span class="env-scanned gray" v-else>未扫描</span>
            </div>
          </div>
        </div>
      </div>

      <!-- 快捷入口 -->
      <div class="card">
        <div class="card-head">
          <div class="card-title">
            <el-icon><Lightning /></el-icon>
            快捷入口
          </div>
        </div>
        <div class="card-body">
          <div class="quick-grid">
            <RouterLink v-if="auth.hasMenu('console')" to="/deploy" class="quick-btn">
              <div class="q-icon" style="background:#eff6ff;color:#1d4ed8"><el-icon><Upload /></el-icon></div>
              <div>
                <div class="q-title">部署控制台</div>
                <div class="q-sub">批量更新镜像 / 重启服务</div>
              </div>
            </RouterLink>
            <RouterLink v-if="auth.hasMenu('history')" to="/history" class="quick-btn">
              <div class="q-icon" style="background:#f0fdf4;color:#059669"><el-icon><Clock /></el-icon></div>
              <div>
                <div class="q-title">发布历史</div>
                <div class="q-sub">按项目/环境/时间筛选</div>
              </div>
            </RouterLink>
            <RouterLink v-if="auth.hasMenu('projects')" to="/projects" class="quick-btn">
              <div class="q-icon" style="background:#fff7ed;color:#ea580c"><el-icon><Folder /></el-icon></div>
              <div>
                <div class="q-title">项目配置</div>
                <div class="q-sub">项目树 / 环境管理</div>
              </div>
            </RouterLink>
            <RouterLink v-if="auth.hasMenu('settings')" to="/settings" class="quick-btn">
              <div class="q-icon" style="background:#fef2f2;color:#dc2626"><el-icon><Setting /></el-icon></div>
              <div>
                <div class="q-title">系统设置</div>
                <div class="q-sub">全局凭证 / Lark / 用户</div>
              </div>
            </RouterLink>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { Folder, Box, Connection, Upload, User, UserFilled, Clock, Monitor, Lightning, Setting, ArrowRight } from '@element-plus/icons-vue'
import dayjs from 'dayjs'
import { getDashboardStats } from '../api'
import { useAuthStore } from '../stores/auth'

const auth = useAuthStore()
const router = useRouter()
const loading = ref(false)
const stats = ref({ kpi: {}, recent_deployments: [], my_recent_deployments: [], envs: [] })
const showPortalNotice = ref(auth.user?.auth_source === 'portal')

const showMyRecent = computed(() => auth.user?.username && auth.user.username !== 'system')

async function load() {
  loading.value = true
  try {
    stats.value = (await getDashboardStats()) || {}
  } finally { loading.value = false }
}

function fromNow(s) {
  if (!s) return ''
  const d = dayjs(s)
  const diff = dayjs().diff(d, 'minute')
  if (diff < 1) return '刚刚'
  if (diff < 60) return diff + ' 分钟前'
  if (diff < 60 * 24) return Math.floor(diff / 60) + ' 小时前'
  const days = Math.floor(diff / (60 * 24))
  if (days < 30) return days + ' 天前'
  return d.format('YYYY-MM-DD')
}
function statusLabel(s) {
  return { success: '成功', partial: '部分', failed: '失败', pending: '进行中', no_change: '无变化' }[s] || s || '-'
}
function actionLabel(a) {
  return {
    update_image: '更新镜像', restart: '重启服务', rollback: '回滚',
    vm_rsync: 'VM rsync', vm_update_version: 'VM 更新',
  }[a] || a
}
function goHistory(id) {
  router.push({ path: '/history', query: { id } })
}

onMounted(() => {
  load()
  if (showPortalNotice.value) {
    setTimeout(() => { showPortalNotice.value = false }, 5000)
  }
})
</script>

<style scoped>
.dash { display: flex; flex-direction: column; gap: 14px; }

/* ===== SSO 登录提示 ===== */
.portal-notice {
  display: flex; align-items: center; gap: 10px;
  padding: 10px 16px;
  background: rgba(16, 185, 129, 0.08);
  border: 1px solid rgba(16, 185, 129, 0.3);
  border-radius: var(--radius);
  color: var(--success); font-size: 13px;
}
.portal-notice b { font-family: var(--mono); font-weight: 600; }
.notice-close {
  margin-left: auto;
  background: none; border: none;
  color: var(--success); cursor: pointer;
  font-size: 20px; line-height: 1; opacity: 0.6; padding: 0 4px;
}
.notice-close:hover { opacity: 1; }
.fade-enter-active, .fade-leave-active { transition: opacity 0.35s ease; }
.fade-enter-from, .fade-leave-to { opacity: 0; }

/* ===== KPI ===== */
.kpi-row { display: grid; grid-template-columns: repeat(5, 1fr); gap: 12px; }
@media (max-width: 1280px) { .kpi-row { grid-template-columns: repeat(3, 1fr); } }
@media (max-width: 768px)  { .kpi-row { grid-template-columns: repeat(2, 1fr); } }
.kpi { background: var(--bg-card); border: 1px solid var(--border); border-radius: var(--radius); padding: 16px 18px; }
.k-head { display: flex; align-items: center; gap: 10px; margin-bottom: 10px; }
.k-icon { width: 32px; height: 32px; border-radius: 6px; display: flex; align-items: center; justify-content: center; }
.k-icon .el-icon { font-size: 16px; }
.k-label { font-size: 12px; color: var(--text-3); font-weight: 500; text-transform: uppercase; letter-spacing: .5px; }
.k-value { display: flex; align-items: baseline; gap: 4px; margin-bottom: 8px; line-height: 1; }
.k-value .main { font: 700 26px var(--mono); color: var(--text); }
.k-value .divide { font: 500 20px var(--mono); color: var(--text-3); margin: 0 2px; }
.k-value .unit { font: 500 13px var(--body); color: var(--text-3); }
.k-value .user-name { font: 600 18px var(--body); }
.k-sub { font-size: 11.5px; color: var(--text-3); display: flex; gap: 6px; flex-wrap: wrap; align-items: center; }
.k-sub.mono { font-family: var(--mono); }

.tag { display: inline-flex; padding: 2px 8px; border-radius: 99px; font: 500 11px var(--mono); background: var(--bg-hover); color: var(--text-2); }
.tag.uat { background: #ecfdf5; color: #059669; }
.tag.prod { background: #fef2f2; color: #dc2626; }
.tag.ok { background: #ecfdf5; color: #059669; }
.tag.fail { background: #fef2f2; color: #dc2626; }

/* ===== 卡片 ===== */
.main-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 14px; }
.card { background: var(--bg-card); border: 1px solid var(--border); border-radius: var(--radius); display: flex; flex-direction: column; overflow: hidden; }
.card-head { display: flex; align-items: center; justify-content: space-between; padding: 14px 18px; border-bottom: 1px solid var(--border-soft); }
.card-title { font: 600 14px var(--body); color: var(--text); display: flex; align-items: center; gap: 8px; }
.card-title .el-icon { font-size: 15px; color: var(--primary); }
.card-sub { font-size: 11.5px; color: var(--text-3); }
.link-more { font-size: 12px; color: var(--primary); text-decoration: none; }
.link-more:hover { text-decoration: underline; }
.card-body { flex: 1; padding: 8px 0; }
.empty { text-align: center; padding: 40px; color: var(--text-3); font-size: 12.5px; }

/* ===== 发布列表 ===== */
.dep-list { display: flex; flex-direction: column; }
.dep-row {
  display: flex; align-items: center; justify-content: space-between;
  padding: 10px 18px; cursor: pointer; transition: background .12s;
  border-bottom: 1px solid var(--border-soft);
}
.dep-row:last-child { border-bottom: none; }
.dep-row:hover { background: var(--bg-hover); }
.dep-l { display: flex; align-items: center; gap: 12px; flex: 1; min-width: 0; }
.env-dot { width: 8px; height: 8px; border-radius: 50%; flex-shrink: 0; }
.env-dot.uat { background: var(--success); }
.env-dot.prod { background: var(--danger); }
.dep-info { flex: 1; min-width: 0; }
.dep-top { display: flex; align-items: center; gap: 10px; font-size: 13px; }
.dep-env { font-weight: 500; color: var(--text); font-family: var(--mono); font-size: 12.5px; }
.dep-action { color: var(--text-2); font-size: 12px; }
.dep-mods { font-size: 11.5px; color: var(--text-3); font-family: var(--mono); }
.dep-bot { display: flex; align-items: center; gap: 6px; font-size: 11.5px; color: var(--text-3); margin-top: 3px; }
.dep-bot .op { color: var(--text-2); font-family: var(--mono); }
.dep-bot .dot { color: var(--border); }
.dep-bot .dur { font-family: var(--mono); }
.chev { color: var(--text-3); font-size: 13px; }

.status { display: inline-block; padding: 1px 7px; border-radius: 3px; font: 500 10.5px var(--mono); }
.status.success { background: #ecfdf5; color: #059669; }
.status.failed { background: #fef2f2; color: #dc2626; }
.status.partial { background: #fff7ed; color: #ea580c; }
.status.pending { background: #eff6ff; color: #1d4ed8; }
.status.no_change { background: var(--bg-hover); color: var(--text-3); }

/* ===== 环境列表 ===== */
.env-list { display: flex; flex-direction: column; }
.env-row {
  display: flex; align-items: center; gap: 12px;
  padding: 10px 18px; border-bottom: 1px solid var(--border-soft);
  font-size: 12.5px;
}
.env-row:last-child { border-bottom: none; }
.env-row:hover { background: var(--bg-hover); }
.env-chip { padding: 2px 7px; border-radius: 3px; font: 600 10px var(--mono); letter-spacing: .4px; }
.env-chip.uat { background: #ecfdf5; color: #059669; }
.env-chip.prod { background: #fef2f2; color: #dc2626; }
.kind-chip { padding: 1px 6px; border-radius: 3px; font: 600 9.5px var(--mono); letter-spacing: .3px; line-height: 1.5; }
.kind-chip.k8s { background: #eff6ff; color: #1d4ed8; }
.kind-chip.vm { background: #f5f3ff; color: #7c3aed; }
.env-name { font-weight: 500; color: var(--text); min-width: 140px; }
.env-mods { color: var(--text-3); font-family: var(--mono); font-size: 11.5px; }
.env-sep { flex: 1; }
.env-last { color: var(--text-2); font-size: 12px; display: flex; align-items: center; gap: 6px; }
.env-scanned { color: var(--text-3); font-size: 11px; min-width: 120px; text-align: right; }
.gray { color: var(--text-3); }

/* ===== 快捷入口 ===== */
.quick-grid { display: grid; grid-template-columns: repeat(2, 1fr); gap: 10px; padding: 8px 18px; }
.quick-btn {
  display: flex; align-items: center; gap: 12px;
  padding: 14px; border: 1px solid var(--border);
  border-radius: var(--radius); text-decoration: none;
  background: var(--bg-card); transition: all .15s; cursor: pointer;
}
.quick-btn:hover { border-color: var(--primary); background: #fbfdff; transform: translateY(-1px); }
.q-icon { width: 36px; height: 36px; border-radius: 7px; display: flex; align-items: center; justify-content: center; flex-shrink: 0; }
.q-icon .el-icon { font-size: 17px; }
.q-title { font: 600 13.5px var(--body); color: var(--text); margin-bottom: 2px; }
.q-sub { font: 400 11.5px var(--body); color: var(--text-3); }
</style>
