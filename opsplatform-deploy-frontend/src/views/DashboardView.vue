<template>
  <div class="dashboard">
    <!-- Getting Started 清淡卡片 -->
    <div v-if="showOnboarding" class="getting-started">
      <div class="gs-head">
        <div>
          <div class="gs-title">快速开始</div>
          <div class="gs-sub">完成以下步骤即可发布你的第一个模块</div>
        </div>
        <div class="gs-progress">
          <span class="gs-progress-text">{{ completedSteps }} / 5</span>
          <div class="gs-progress-bar">
            <div class="gs-progress-fill" :style="{ width: (completedSteps / 5 * 100) + '%' }"></div>
          </div>
        </div>
      </div>
      <div class="gs-steps">
        <router-link v-for="(step, i) in steps" :key="i" :to="step.path"
                     class="gs-step" :class="{ 'gs-step-done': step.done, 'gs-step-active': !step.done && isActiveStep(i) }">
          <div class="gs-step-num">
            <Check v-if="step.done" :size="12" />
            <span v-else>{{ i + 1 }}</span>
          </div>
          <div class="gs-step-body">
            <div class="gs-step-title">{{ step.title }}</div>
            <div class="gs-step-desc">{{ step.desc }}</div>
          </div>
          <ChevronRight :size="14" class="text-muted" />
        </router-link>
      </div>
    </div>

    <!-- KPI -->
    <div class="kpi-bar">
      <div class="kpi kpi-blue">
        <div class="kpi-icon"><Box :size="18" /></div>
        <div class="kpi-body">
          <div class="kpi-label">应用 Projects</div>
          <div class="kpi-value">{{ stats.projects }}</div>
          <div class="kpi-foot">{{ stats.projectEnvs }} project-env 实例</div>
        </div>
      </div>
      <div class="kpi kpi-purple">
        <div class="kpi-icon"><Layers :size="18" /></div>
        <div class="kpi-body">
          <div class="kpi-label">模块 Modules</div>
          <div class="kpi-value">{{ stats.modules }}</div>
          <div class="kpi-foot">{{ stats.modulesActive }} 活跃 · {{ stats.modulesScaled }} 停机</div>
        </div>
      </div>
      <div class="kpi kpi-green">
        <div class="kpi-icon"><Rocket :size="18" /></div>
        <div class="kpi-body">
          <div class="kpi-label">7 日发布</div>
          <div class="kpi-value">{{ stats.weekDeploys }}</div>
          <div class="kpi-foot">成功 {{ stats.weekSuccess }} · 失败 {{ stats.weekFailed }}</div>
        </div>
      </div>
      <div class="kpi kpi-cyan">
        <div class="kpi-icon"><TrendingUp :size="18" /></div>
        <div class="kpi-body">
          <div class="kpi-label">成功率</div>
          <div class="kpi-value">{{ stats.successRate }}<span style="font-size: 14px">%</span></div>
          <div class="kpi-foot">近 7 日发布</div>
        </div>
      </div>
      <div class="kpi kpi-amber">
        <div class="kpi-icon"><FileCode :size="18" /></div>
        <div class="kpi-body">
          <div class="kpi-label">Chart 模板</div>
          <div class="kpi-value">{{ stats.templates }}</div>
          <div class="kpi-foot">B {{ stats.templatesBackend }} · F {{ stats.templatesFrontend }}</div>
        </div>
      </div>
      <div class="kpi kpi-red">
        <div class="kpi-icon"><KeyRound :size="18" /></div>
        <div class="kpi-body">
          <div class="kpi-label">Secret</div>
          <div class="kpi-value">{{ stats.secrets }}</div>
          <div class="kpi-foot">跨 {{ stats.projectEnvs }} 个环境</div>
        </div>
      </div>
    </div>

    <!-- Chart + 最近发布 -->
    <div class="main-grid">
      <div class="card col-span-2">
        <div class="card-header">
          <div class="card-title">发布趋势 · 近 14 天</div>
          <div class="flex gap-2 items-center">
            <span class="chip chip-green">
              <span class="dot dot-success"></span>成功 {{ chart.totalSuccess }}
            </span>
            <span class="chip chip-red">
              <span class="dot dot-danger"></span>失败 {{ chart.totalFailed }}
            </span>
          </div>
        </div>

        <!-- 有数据 -->
        <div v-if="chart.totalSuccess + chart.totalFailed > 0" class="chart-wrap">
          <svg class="chart-svg" :viewBox="`0 0 ${chart.width} ${chart.height}`" preserveAspectRatio="none">
            <line v-for="y in 4" :key="`g${y}`" :x1="chart.padL" :x2="chart.width - chart.padR"
                  :y1="(chart.height - chart.padB) - ((chart.height - chart.padT - chart.padB) / 4) * y"
                  :y2="(chart.height - chart.padB) - ((chart.height - chart.padT - chart.padB) / 4) * y"
                  stroke="#f1f5f9" stroke-dasharray="3 3" />
            <g v-for="(d, i) in chart.data" :key="`b${i}`">
              <rect :x="chart.padL + i * chart.barW + 3" :y="barY(d.success + d.failed)"
                    :width="chart.barW - 6" :height="barH(d.success + d.failed)" fill="#ef4444" rx="2" />
              <rect :x="chart.padL + i * chart.barW + 3" :y="barY(d.success)"
                    :width="chart.barW - 6" :height="barH(d.success)" fill="#22c55e" rx="2" />
            </g>
            <text v-for="(d, i) in chart.data" :key="`t${i}`"
                  :x="chart.padL + i * chart.barW + chart.barW / 2"
                  :y="chart.height - 6" text-anchor="middle" fill="#94a3b8" font-size="10" font-family="Fira Code">
              {{ d.label }}
            </text>
            <text v-for="y in 4" :key="`yl${y}`" :x="chart.padL - 6"
                  :y="(chart.height - chart.padB) - ((chart.height - chart.padT - chart.padB) / 4) * y + 3"
                  text-anchor="end" fill="#94a3b8" font-size="10" font-family="Fira Code">
              {{ Math.round(chart.yMax / 4 * y) }}
            </text>
          </svg>
        </div>

        <!-- 无数据空态 -->
        <div v-else class="chart-empty">
          <div class="chart-empty-bg">
            <div v-for="n in 14" :key="'ghost' + n" class="ghost-bar" :style="{ height: (30 + (n * 13 % 60)) + 'px' }"></div>
          </div>
          <div class="chart-empty-overlay">
            <div class="chart-empty-icon"><BarChart3 :size="32" /></div>
            <div class="chart-empty-title">暂无发布数据</div>
            <div class="chart-empty-desc">创建模块并更新镜像即可看到部署趋势</div>
            <router-link to="/modules/create" class="btn btn-primary btn-sm" style="margin-top: 8px">
              <Plus :size="12" /> 新建模块
            </router-link>
          </div>
        </div>
      </div>

      <div class="card">
        <div class="card-header">
          <div class="card-title">最近发布</div>
          <router-link to="/deployments" class="text-xs text-primary">查看全部 →</router-link>
        </div>
        <div v-if="recentDeploys.length" class="mini-list">
          <div v-for="d in recentDeploys" :key="d.id" class="mini-item">
            <span class="dot" :class="dotClass(d.status)"></span>
            <div class="mini-body">
              <div class="mini-title">
                <span class="chip" :class="actionChipClass(d.action)" style="font-size: 10px; padding: 1px 6px">{{ actionLabel(d.action) }}</span>
                <span class="mono text-bold" style="font-size: 11.5px">{{ d.module_name || '—' }}</span>
              </div>
              <div class="mini-meta">{{ formatTime(d.created_at) }} · {{ d.operator || 'system' }}</div>
            </div>
          </div>
        </div>
        <div v-else class="mini-empty">
          <div class="mini-empty-icon"><History :size="20" /></div>
          <div class="text-sm text-bold">暂无发布记录</div>
          <div class="text-xs text-muted" style="margin-top: 3px">触发第一次发布后会显示在这里</div>
          <div class="timeline-placeholder">
            <div v-for="n in 5" :key="'ph' + n" class="placeholder-line" :style="{ opacity: 1 - n * 0.18 }">
              <div class="ph-dot"></div>
              <div class="ph-bar"></div>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- 环境 + 快捷 -->
    <div class="second-grid">
      <div class="card">
        <div class="card-header">
          <div class="card-title">环境概览</div>
          <router-link to="/environments" class="text-xs text-primary">管理环境 →</router-link>
        </div>
        <div class="env-grid">
          <div v-for="env in environments" :key="env.id" class="env-cell" :style="{ borderLeftColor: envColor(env.name) }">
            <div class="env-cell-head">
              <span class="env-cell-name">{{ env.display_name || env.name }}</span>
              <span class="badge" :class="env.auto_sync ? 'badge-success' : 'badge-gray'">
                {{ env.auto_sync ? 'AUTO' : 'MANUAL' }}
              </span>
            </div>
            <div class="env-stats">
              <div class="env-stat">
                <div class="env-stat-num mono">{{ projectCountByEnv(env.id) }}</div>
                <div class="env-stat-lbl">项目</div>
              </div>
              <div class="env-stat">
                <div class="env-stat-num mono">{{ moduleCountByEnv(env.id) }}</div>
                <div class="env-stat-lbl">模块</div>
              </div>
              <div class="env-stat">
                <div class="env-stat-num mono" :class="env.argocd_url ? 'text-success' : 'text-muted'">
                  {{ env.argocd_url ? '✓' : '—' }}
                </div>
                <div class="env-stat-lbl">ArgoCD</div>
              </div>
            </div>
            <div class="env-code mono">{{ env.name }}</div>
          </div>
          <div v-if="!environments.length" class="empty-state" style="grid-column: 1/-1; padding: 20px">
            <div class="text-sm">暂无环境</div>
          </div>
        </div>
      </div>

      <div class="card">
        <div class="card-header">
          <div class="card-title">快捷操作</div>
        </div>
        <div class="quick-grid">
          <router-link to="/modules/create" class="quick-tile quick-blue">
            <div class="quick-tile-icon"><Plus :size="18" /></div>
            <div class="quick-tile-label">新建模块</div>
          </router-link>
          <router-link to="/projects" class="quick-tile quick-cyan">
            <div class="quick-tile-icon"><FolderTree :size="18" /></div>
            <div class="quick-tile-label">应用管理</div>
          </router-link>
          <router-link to="/secrets" class="quick-tile quick-amber">
            <div class="quick-tile-icon"><KeyRound :size="18" /></div>
            <div class="quick-tile-label">Secret</div>
          </router-link>
          <router-link to="/deployments" class="quick-tile quick-purple">
            <div class="quick-tile-icon"><History :size="18" /></div>
            <div class="quick-tile-label">发布历史</div>
          </router-link>
          <router-link to="/chart-templates" class="quick-tile quick-green">
            <div class="quick-tile-icon"><FileCode :size="18" /></div>
            <div class="quick-tile-label">Chart 模板</div>
          </router-link>
          <router-link to="/global-config" class="quick-tile quick-gray">
            <div class="quick-tile-icon"><Settings :size="18" /></div>
            <div class="quick-tile-label">全局配置</div>
          </router-link>
        </div>
      </div>
    </div>

    <!-- Top 项目 + 系统状态 -->
    <div class="third-grid">
      <div class="card">
        <div class="card-header">
          <div class="card-title">应用分布 · Top 5</div>
          <router-link to="/projects" class="text-xs text-primary">全部应用 →</router-link>
        </div>
        <div v-if="topProjects.length" class="bar-list">
          <div v-for="p in topProjects" :key="p.id" class="bar-row">
            <div class="bar-avatar" :style="{ background: avatarColor(p.name) }">
              {{ (p.display_name || p.name).charAt(0).toUpperCase() }}
            </div>
            <div class="bar-label">
              <div class="bar-name">{{ p.display_name || p.name }}</div>
              <div class="bar-code mono">{{ p.name }}</div>
            </div>
            <div class="bar-wrap">
              <div class="bar-fill" :style="{ width: (p.moduleCount / maxModuleCount * 100) + '%' }"></div>
            </div>
            <div class="bar-num mono">{{ p.moduleCount }}</div>
          </div>
        </div>
        <div v-else class="empty-inline">
          <Box :size="22" class="text-muted" />
          <div>
            <div class="text-sm text-bold">还没有项目</div>
            <div class="text-xs text-muted">创建第一个项目即可在此看到模块分布</div>
          </div>
          <router-link to="/projects" class="btn btn-sm btn-primary" style="margin-left: auto">新建项目</router-link>
        </div>
      </div>

      <div class="card">
        <div class="card-header">
          <div class="card-title">系统状态</div>
        </div>
        <div class="status-list">
          <div class="status-row">
            <div class="status-left">
              <div class="status-icon" :style="{ background: stats.harborConfigured ? '#dcfce7' : '#f1f5f9', color: stats.harborConfigured ? '#166534' : '#64748b' }">
                <Cloud :size="12" />
              </div>
              <div>
                <div class="status-name">Harbor Registry</div>
                <div class="status-sub text-xs text-muted">镜像仓库</div>
              </div>
            </div>
            <span class="chip" :class="stats.harborConfigured ? 'chip-green' : 'chip-gray'">
              {{ stats.harborConfigured ? '已配置' : '未配置' }}
            </span>
          </div>
          <div class="status-row">
            <div class="status-left">
              <div class="status-icon" :style="{ background: stats.gitlabConfigured ? '#dcfce7' : '#f1f5f9', color: stats.gitlabConfigured ? '#166534' : '#64748b' }">
                <GitBranch :size="12" />
              </div>
              <div>
                <div class="status-name">GitLab</div>
                <div class="status-sub text-xs text-muted">Helm chart 仓库</div>
              </div>
            </div>
            <span class="chip" :class="stats.gitlabConfigured ? 'chip-green' : 'chip-gray'">
              {{ stats.gitlabConfigured ? '已配置' : '未配置' }}
            </span>
          </div>
          <div class="status-row" v-for="env in environments" :key="'st' + env.id">
            <div class="status-left">
              <div class="status-icon" :style="{ background: env.argocd_url ? '#dcfce7' : '#f1f5f9', color: env.argocd_url ? '#166534' : '#64748b' }">
                <span class="dot" :style="{ background: envColor(env.name), boxShadow: 'none' }"></span>
              </div>
              <div>
                <div class="status-name">ArgoCD / {{ env.display_name || env.name }}</div>
                <div class="status-sub text-xs text-muted">
                  {{ env.argocd_url || '点击环境字典配置' }}
                </div>
              </div>
            </div>
            <span class="chip" :class="env.argocd_url ? 'chip-green' : 'chip-gray'">
              {{ env.argocd_url ? '已连接' : '未配置' }}
            </span>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { Plus, Box, Layers, Rocket, TrendingUp, FileCode, KeyRound, History, FolderTree, Settings, Cloud, GitBranch, Check, BarChart3, ChevronRight } from 'lucide-vue-next'
import { projectsApi, projectEnvsApi, modulesApi, chartTemplatesApi, deploymentsApi, environmentsApi, secretsApi, globalConfigApi } from '../api'

const stats = ref({
  projects: 0, projectEnvs: 0, modules: 0, modulesActive: 0, modulesScaled: 0,
  templates: 0, templatesBackend: 0, templatesFrontend: 0,
  secrets: 0, weekDeploys: 0, weekSuccess: 0, weekFailed: 0, successRate: 100,
  gitlabConfigured: false, harborConfigured: false, anyArgoConfigured: false
})
const recentDeploys = ref([])
const environments = ref([])
const allProjects = ref([])
const projectEnvs = ref([])
const modules = ref([])

const chart = ref({ width: 640, height: 180, padL: 30, padR: 14, padT: 10, padB: 22, barW: 42, yMax: 10, data: [], totalSuccess: 0, totalFailed: 0 })

const steps = computed(() => [
  { title: '配置 GitLab + Harbor', desc: '全局配置中填写 Git / 镜像仓库凭证', path: '/global-config', done: stats.value.gitlabConfigured && stats.value.harborConfigured },
  { title: '为环境配置 ArgoCD', desc: 'dev / test / uat / prod 各自一套 ArgoCD 实例', path: '/environments', done: stats.value.anyArgoConfigured },
  { title: '创建 Chart 模板', desc: '添加 test1 / test2 / test3 作为模块脚手架', path: '/chart-templates', done: stats.value.templates > 0 },
  { title: '创建第一个项目', desc: '添加项目并为其配置 项目-环境', path: '/projects', done: stats.value.projects > 0 && stats.value.projectEnvs > 0 },
  { title: '新建第一个模块发布', desc: '选模板、填镜像，触发自动 sync', path: '/modules/create', done: stats.value.modules > 0 }
])

const completedSteps = computed(() => steps.value.filter(s => s.done).length)
const showOnboarding = computed(() => completedSteps.value < 5)
function isActiveStep(i) {
  const firstUndone = steps.value.findIndex(s => !s.done)
  return i === firstUndone
}

const maxModuleCount = computed(() => Math.max(1, ...topProjects.value.map(p => p.moduleCount)))
const topProjects = computed(() => {
  const peByProj = {}
  projectEnvs.value.forEach(pe => { (peByProj[pe.project_id] = peByProj[pe.project_id] || []).push(pe.id) })
  return allProjects.value.map(p => {
    const peIds = peByProj[p.id] || []
    return { ...p, moduleCount: modules.value.filter(m => peIds.includes(m.project_env_id)).length }
  }).sort((a, b) => b.moduleCount - a.moduleCount).slice(0, 5)
})

function moduleCountByEnv(envId) {
  const peIds = projectEnvs.value.filter(pe => pe.env_id === envId).map(pe => pe.id)
  return modules.value.filter(m => peIds.includes(m.project_env_id)).length
}
function projectCountByEnv(envId) {
  return new Set(projectEnvs.value.filter(pe => pe.env_id === envId).map(pe => pe.project_id)).size
}

function envColor(name) { return { dev: '#10b981', test: '#06b6d4', uat: '#f59e0b', prod: '#ef4444' }[name] || '#64748b' }
function avatarColor(name) {
  const colors = ['#1e40af', '#6d28d9', '#db2777', '#b45309', '#15803d', '#0e7490', '#4338ca', '#b91c1c']
  let hash = 0
  for (let i = 0; i < (name || 'U').length; i++) hash = name.charCodeAt(i) + ((hash << 5) - hash)
  return colors[Math.abs(hash) % colors.length]
}
function dotClass(s) { return { success: 'dot-success', failed: 'dot-danger', pending: 'dot-warning' }[s] || 'dot-gray' }
function actionLabel(a) {
  return { create: '新建', update_image: '更镜像', update_config: '改配置', update_secret: '改Secret',
           restart: '重启', scale_zero: '停机', scale_up: '恢复', delete: '删除', rollback: '回滚' }[a] || a
}
function actionChipClass(a) {
  return { create: 'chip-green', update_image: 'chip', update_config: 'chip', update_secret: 'chip-amber',
           delete: 'chip-red', restart: 'chip-amber', rollback: 'chip-amber' }[a] || 'chip-gray'
}
function formatTime(t) {
  if (!t) return '-'
  const d = new Date(t); const diff = (Date.now() - d.getTime()) / 1000
  if (diff < 60) return '刚刚'
  if (diff < 3600) return Math.floor(diff / 60) + '分钟前'
  if (diff < 86400) return Math.floor(diff / 3600) + '小时前'
  return Math.floor(diff / 86400) + '天前'
}

function barY(v) {
  const h = chart.value.height - chart.value.padT - chart.value.padB
  return chart.value.padT + h - (h * v / chart.value.yMax)
}
function barH(v) {
  const h = chart.value.height - chart.value.padT - chart.value.padB
  return (h * v / chart.value.yMax) || 0
}

function buildChartData(deploys) {
  const days = 14
  const buckets = []
  const today = new Date(); today.setHours(0, 0, 0, 0)
  for (let i = days - 1; i >= 0; i--) {
    const d = new Date(today.getTime() - i * 86400000)
    const key = d.toISOString().slice(0, 10)
    buckets.push({ date: key, label: key.slice(5), success: 0, failed: 0 })
  }
  let totalS = 0, totalF = 0
  for (const d of deploys) {
    const key = (d.created_at || '').slice(0, 10)
    const b = buckets.find(x => x.date === key)
    if (b) {
      if (d.status === 'success') { b.success++; totalS++ }
      else if (d.status === 'failed') { b.failed++; totalF++ }
    }
  }
  const max = Math.max(1, ...buckets.map(b => b.success + b.failed))
  chart.value.data = buckets
  chart.value.yMax = max + Math.ceil(max * 0.2) || 5
  chart.value.totalSuccess = totalS
  chart.value.totalFailed = totalF
}

async function load() {
  try {
    const [projR, peR, modR, tplR, depR, envR, gcR] = await Promise.all([
      projectsApi.list(), projectEnvsApi.list(), modulesApi.list(),
      chartTemplatesApi.list(), deploymentsApi.list({ page: 1, page_size: 200 }),
      environmentsApi.list(), globalConfigApi.get()
    ])
    allProjects.value = projR.data || []
    projectEnvs.value = peR.data || []
    modules.value = modR.data || []
    environments.value = envR.data || []
    const templates = tplR.data || []
    const deploys = depR.data?.list || []

    let secretCount = 0
    for (const pe of projectEnvs.value) {
      try { const r = await secretsApi.list({ project_env_id: pe.id }); secretCount += (r.data || []).length } catch (e) {}
    }

    buildChartData(deploys)
    const weekAgo = Date.now() - 7 * 86400000
    const weekDeploys = deploys.filter(d => new Date(d.created_at).getTime() > weekAgo)
    const weekSuccess = weekDeploys.filter(d => d.status === 'success').length
    const weekFailed = weekDeploys.filter(d => d.status === 'failed').length

    const gc = gcR.data || {}
    stats.value = {
      projects: allProjects.value.length, projectEnvs: projectEnvs.value.length,
      modules: modules.value.length,
      modulesActive: modules.value.filter(m => m.status === 'active').length,
      modulesScaled: modules.value.filter(m => m.status === 'scaled_zero').length,
      templates: templates.length,
      templatesBackend: templates.filter(t => t.type === 'backend').length,
      templatesFrontend: templates.filter(t => t.type === 'frontend').length,
      secrets: secretCount,
      weekDeploys: weekDeploys.length, weekSuccess, weekFailed,
      successRate: weekDeploys.length ? Math.round(weekSuccess / weekDeploys.length * 100) : 100,
      gitlabConfigured: !!gc.gitlab_url,
      harborConfigured: !!gc.harbor_url,
      anyArgoConfigured: environments.value.some(e => e.argocd_url)
    }
    recentDeploys.value = deploys.slice(0, 6)
  } catch (e) { console.error(e) }
}

onMounted(load)
</script>

<style scoped>
.dashboard { display: flex; flex-direction: column; gap: 10px; }

/* KPI */
.kpi-bar { grid-template-columns: repeat(6, 1fr); }
@media (max-width: 1400px) { .kpi-bar { grid-template-columns: repeat(3, 1fr); } }
@media (max-width: 768px) { .kpi-bar { grid-template-columns: repeat(2, 1fr); } }

/* Main grid */
.main-grid { display: grid; grid-template-columns: 2fr 1fr; gap: 10px; }
@media (max-width: 1200px) { .main-grid { grid-template-columns: 1fr; } }

.chart-wrap { height: 220px; }
.chart-svg { width: 100%; height: 100%; }

.chart-empty { position: relative; height: 220px; overflow: hidden; border-radius: 4px; background: linear-gradient(180deg, #f8fafc 0%, #f1f5f9 100%); }
.chart-empty-bg {
  position: absolute; inset: 0; display: flex; align-items: flex-end;
  justify-content: space-around; padding: 14px;
  opacity: .35;
}
.ghost-bar { width: 18px; background: linear-gradient(180deg, #cbd5e1 0%, #e2e8f0 100%); border-radius: 2px; }
.chart-empty-overlay {
  position: absolute; inset: 0; display: flex; flex-direction: column;
  align-items: center; justify-content: center; gap: 6px;
  background: linear-gradient(180deg, rgba(248,250,252,.7) 0%, rgba(241,245,249,.92) 100%);
}
.chart-empty-icon {
  width: 46px; height: 46px; border-radius: 10px;
  background: white; color: #3b82f6;
  display: flex; align-items: center; justify-content: center;
  box-shadow: 0 2px 8px rgba(15,23,42,.08);
}
.chart-empty-title { font-size: 14px; font-weight: 700; color: #0f172a; }
.chart-empty-desc { font-size: 12px; color: #64748b; }

.mini-list { display: flex; flex-direction: column; }
.mini-item { display: flex; gap: 8px; padding: 7px 0; border-bottom: 1px solid #f1f5f9; }
.mini-item:last-child { border-bottom: none; }
.mini-item .dot { margin-top: 5px; flex-shrink: 0; }
.mini-body { flex: 1; min-width: 0; }
.mini-title { display: flex; gap: 6px; align-items: center; flex-wrap: wrap; }
.mini-meta { font-size: 10.5px; color: #94a3b8; margin-top: 2px; }

.mini-empty { padding: 20px 12px; text-align: center; }
.mini-empty-icon {
  width: 42px; height: 42px; border-radius: 10px;
  background: #eff6ff; color: #3b82f6;
  display: flex; align-items: center; justify-content: center;
  margin: 0 auto 8px;
}
.timeline-placeholder { margin-top: 14px; display: flex; flex-direction: column; gap: 5px; }
.placeholder-line { display: flex; align-items: center; gap: 6px; }
.ph-dot { width: 6px; height: 6px; border-radius: 50%; background: #e2e8f0; }
.ph-bar { flex: 1; height: 6px; border-radius: 3px; background: linear-gradient(90deg, #e2e8f0 0%, #f1f5f9 100%); }

/* Second grid */
.second-grid { display: grid; grid-template-columns: 3fr 2fr; gap: 10px; }
@media (max-width: 1200px) { .second-grid { grid-template-columns: 1fr; } }

.env-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(170px, 1fr)); gap: 8px; }
.env-cell {
  border: 1px solid #e2e8f0; border-left: 3px solid #64748b;
  border-radius: 6px; padding: 10px 12px;
  background: white;
  display: flex; flex-direction: column; gap: 8px;
}
.env-cell-head { display: flex; justify-content: space-between; align-items: center; }
.env-cell-name { font-size: 13px; font-weight: 700; color: #0f172a; }

.env-stats { display: grid; grid-template-columns: 1fr 1fr 1fr; gap: 4px; }
.env-stat { text-align: center; padding: 6px 2px; background: #f8fafc; border-radius: 4px; }
.env-stat-num { font-size: 14px; font-weight: 700; color: #0f172a; line-height: 1; }
.env-stat-lbl { font-size: 10px; color: #64748b; margin-top: 2px; }
.env-code { font-size: 10.5px; color: #94a3b8; text-align: right; padding-top: 2px; border-top: 1px dashed #f1f5f9; }

.quick-grid { display: grid; grid-template-columns: 1fr 1fr 1fr; gap: 6px; }
.quick-tile {
  padding: 14px 8px; border-radius: 6px;
  font-size: 12px; font-weight: 600;
  border: 1px solid transparent;
  display: flex; flex-direction: column; align-items: center; gap: 5px;
  transition: all .15s;
}
.quick-tile:hover { transform: translateY(-1px); box-shadow: 0 3px 8px rgba(15,23,42,.1); }
.quick-tile-icon {
  width: 34px; height: 34px; border-radius: 8px;
  display: flex; align-items: center; justify-content: center;
}
.quick-blue { background: #eff6ff; color: #1e40af; }
.quick-blue .quick-tile-icon { background: white; color: #1e40af; }
.quick-cyan { background: #ecfeff; color: #155e75; }
.quick-cyan .quick-tile-icon { background: white; color: #155e75; }
.quick-amber { background: #fffbeb; color: #92400e; }
.quick-amber .quick-tile-icon { background: white; color: #92400e; }
.quick-purple { background: #faf5ff; color: #6b21a8; }
.quick-purple .quick-tile-icon { background: white; color: #6b21a8; }
.quick-green { background: #f0fdf4; color: #15803d; }
.quick-green .quick-tile-icon { background: white; color: #15803d; }
.quick-gray { background: #f8fafc; color: #475569; }
.quick-gray .quick-tile-icon { background: white; color: #475569; }

/* Third grid */
.third-grid { display: grid; grid-template-columns: 3fr 2fr; gap: 10px; }
@media (max-width: 1200px) { .third-grid { grid-template-columns: 1fr; } }

.bar-list { display: flex; flex-direction: column; gap: 10px; }
.bar-row { display: flex; align-items: center; gap: 10px; }
.bar-avatar {
  width: 28px; height: 28px; border-radius: 6px;
  display: flex; align-items: center; justify-content: center;
  color: white; font-weight: 700; font-size: 12px;
  flex-shrink: 0; font-family: 'Fira Code', monospace;
}
.bar-label { flex: 0 0 150px; min-width: 0; }
.bar-name { font-size: 12.5px; font-weight: 600; color: #0f172a; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.bar-code { font-size: 10.5px; color: #94a3b8; }
.bar-wrap { flex: 1; height: 8px; background: #f1f5f9; border-radius: 4px; overflow: hidden; }
.bar-fill { height: 100%; background: linear-gradient(90deg, #3b82f6 0%, #1e40af 100%); border-radius: 4px; transition: width .3s; }
.bar-num { flex: 0 0 36px; text-align: right; font-size: 14px; font-weight: 700; color: #0f172a; }

.empty-inline {
  display: flex; align-items: center; gap: 12px;
  padding: 14px;
  background: #f8fafc; border: 1px dashed #cbd5e1; border-radius: 6px;
}

.status-list { display: flex; flex-direction: column; }
.status-row { display: flex; justify-content: space-between; align-items: center; padding: 9px 0; border-bottom: 1px solid #f1f5f9; gap: 10px; }
.status-row:last-child { border-bottom: none; }
.status-left { display: flex; align-items: center; gap: 10px; flex: 1; min-width: 0; }
.status-icon {
  width: 26px; height: 26px; border-radius: 6px;
  display: flex; align-items: center; justify-content: center;
  flex-shrink: 0;
}
.status-icon .dot { box-shadow: none !important; }
.status-name { font-size: 12.5px; color: #0f172a; font-weight: 500; }
.status-sub { margin-top: 1px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; max-width: 260px; }
</style>
