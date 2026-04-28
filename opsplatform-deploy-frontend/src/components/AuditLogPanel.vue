<template>
  <div class="al-panel">
    <div class="al-hd">
      <div class="al-title">
        审计日志
        <span class="al-sub">所有写操作（除测试连接外）的完整记录 · 仅 admin 可见</span>
      </div>
      <button class="btn ghost sm" @click="reload" :disabled="loading">
        {{ loading ? '查询中…' : '🔄 刷新' }}
      </button>
    </div>

    <!-- 筛选 -->
    <div class="al-filters">
      <div class="al-f">
        <label>操作人</label>
        <input v-model="qUsername" placeholder="精确用户名" @keyup.enter="reload" />
      </div>
      <div class="al-f">
        <label>操作类型</label>
        <el-select v-model="qAction" placeholder="全部" clearable filterable
          style="width:280px;" @change="reload">
          <el-option v-for="a in actionList" :key="a.action"
            :label="actionLabel(a.action) + ' (' + a.count + ')'" :value="a.action" />
        </el-select>
      </div>
      <div class="al-f">
        <label>开始时间</label>
        <el-date-picker v-model="qSince" type="datetime" placeholder="任意"
          format="YYYY-MM-DD HH:mm" value-format="YYYY-MM-DD HH:mm:00"
          style="width:180px;" @change="reload" />
      </div>
      <div class="al-f">
        <label>结束时间</label>
        <el-date-picker v-model="qUntil" type="datetime" placeholder="任意"
          format="YYYY-MM-DD HH:mm" value-format="YYYY-MM-DD HH:mm:00"
          style="width:180px;" @change="reload" />
      </div>
      <div class="al-f flex1">
        <label>搜索关键字</label>
        <input v-model="qKeyword" placeholder="目标名称 / detail 内容（模糊）" @keyup.enter="reload" />
      </div>
      <button class="btn ghost sm" @click="resetFilters">清空筛选</button>
    </div>

    <!-- 表格 -->
    <div class="al-table-wrap">
      <table class="al-table">
        <thead>
          <tr>
            <th style="width:155px;">时间</th>
            <th style="width:110px;">操作人</th>
            <th style="width:130px;">IP</th>
            <th style="width:200px;">操作类型</th>
            <th>目标</th>
            <th style="width:80px;text-align:center;">详情</th>
          </tr>
        </thead>
        <tbody>
          <template v-for="row in list" :key="row.id">
            <tr :class="{ expanded: expandedId === row.id }">
              <td class="mono">{{ row.created_at }}</td>
              <td>
                <span class="user-chip">{{ row.username }}</span>
                <span v-if="row.auth_source === 'portal'" class="src-tag portal">portal</span>
              </td>
              <td class="mono mute">{{ row.ip || '—' }}</td>
              <td>
                <span :class="['act-tag', actClass(row.action)]" :title="row.action">{{ actionLabel(row.action) }}</span>
              </td>
              <td class="target-cell">
                <span v-if="row.target_type" class="target-type">{{ targetTypeLabel(row.target_type) }}</span>
                <span v-if="row.target_name" class="target-name">{{ row.target_name }}</span>
                <span v-else class="mute">—</span>
              </td>
              <td style="text-align:center;">
                <button v-if="row.detail" class="link-btn"
                  @click="expandedId = expandedId === row.id ? null : row.id">
                  {{ expandedId === row.id ? '▲ 收起' : '▼ 查看' }}
                </button>
                <span v-else class="mute">—</span>
              </td>
            </tr>
            <tr v-if="expandedId === row.id" class="detail-row">
              <td colspan="6">
                <pre class="detail-pre">{{ formatDetail(row.detail) }}</pre>
              </td>
            </tr>
          </template>
          <tr v-if="!loading && !list.length">
            <td colspan="6" class="empty-row">无匹配的审计记录</td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- 分页 -->
    <div class="al-foot">
      <span class="hint">共 <b>{{ total }}</b> 条 · 第 {{ page }} / {{ totalPages }} 页</span>
      <div class="page-ctrl">
        <button class="btn ghost sm" :disabled="page <= 1" @click="goPage(page - 1)">‹ 上一页</button>
        <button class="btn ghost sm" :disabled="page >= totalPages" @click="goPage(page + 1)">下一页 ›</button>
        <select v-model.number="pageSize" @change="reload" class="sel sm">
          <option :value="20">20 / 页</option>
          <option :value="50">50 / 页</option>
          <option :value="100">100 / 页</option>
        </select>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { listAuditLogs, listAuditActionTypes } from '../api'

// 操作类型 → 中文。维护新事件时在这里加一行；未登记的 action 兜底显示原 code
const ACTION_LABELS = {
  // 认证
  'auth.login.success':       '登录成功',
  'auth.login.failed':        '登录失败',
  'auth.logout':              '登出',
  'auth.portal_auth.failed':  'SSO 认证失败',
  'auth.portal_auth.denied':  'SSO 拒绝访问',
  // 用户
  'user.create':              '创建用户',
  'user.update':              '修改用户',
  'user.delete':              '删除用户',
  'user.toggle':              '启用/禁用用户',
  'user.reset_password':      '重置密码',
  // 项目
  'project.create':           '创建项目',
  'project.update':           '修改项目',
  'project.delete':           '删除项目',
  // 项目环境
  'project_env.create':       '创建项目环境',
  'project_env.update':       '修改项目环境',
  'project_env.delete':       '删除项目环境',
  'project_env.scan_modules': '同步模块',
  // ArgoCD
  'argocd_instance.create':   '创建 ArgoCD 实例',
  'argocd_instance.update':   '修改 ArgoCD 实例',
  'argocd_instance.delete':   '删除 ArgoCD 实例',
  'argocd_cache.refresh':     '刷新 ArgoCD 缓存',
  // GitLab
  'gitlab_repo.create':       '创建 GitLab 仓库',
  'gitlab_repo.update':       '修改 GitLab 仓库',
  'gitlab_repo.delete':       '删除 GitLab 仓库',
  // Lark
  'lark_bot.create':          '创建 Lark 机器人',
  'lark_bot.update':          '修改 Lark 机器人',
  'lark_bot.delete':          '删除 Lark 机器人',
  // 通知人
  'contact.create':           '创建通知人',
  'contact.update':           '修改通知人',
  'contact.delete':           '删除通知人',
  // 全局
  'global_config.update':     '修改全局配置',
  // 发布
  'deploy.update_image':       '更新镜像',
  'deploy.rollback_via_update': '编辑后回滚',
  'deploy.restart':            '重启服务',
  'deploy.rollback':           '回滚发布',
  'deploy.cancel':             '取消发布',
  'deploy.cancel.stale':       '取消过期任务',
  // 历史
  'history.cleanup':          '清理历史',
  // 模块模板
  'module_template.create':   '创建模块模板',
  'module_template.update':   '修改模块模板',
  'module_template.delete':   '删除模块模板',
}

// 目标类型 → 中文
const TARGET_TYPE_LABELS = {
  user:            '用户',
  project:         '项目',
  project_env:     '项目环境',
  argocd_instance: 'ArgoCD 实例',
  gitlab_repo:     'GitLab 仓库',
  lark_bot:        'Lark 机器人',
  contact:         '通知人',
  global_config:   '全局配置',
  deployment:      '发布记录',
  module_template: '模块模板',
}

function actionLabel(code) {
  return ACTION_LABELS[code] || code
}
function targetTypeLabel(t) {
  return TARGET_TYPE_LABELS[t] || t
}

const list = ref([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(50)
const loading = ref(false)
const expandedId = ref(null)

const qUsername = ref('')
const qAction = ref('')
const qSince = ref('')
const qUntil = ref('')
const qKeyword = ref('')
const actionList = ref([])

const totalPages = computed(() => Math.max(1, Math.ceil(total.value / pageSize.value)))

async function reload() {
  loading.value = true
  try {
    const params = { page: page.value, page_size: pageSize.value }
    if (qUsername.value) params.username = qUsername.value
    if (qAction.value) params.action = qAction.value
    if (qSince.value) params.since = qSince.value
    if (qUntil.value) params.until = qUntil.value
    if (qKeyword.value) params.q = qKeyword.value
    const r = await listAuditLogs(params)
    list.value = r.list || []
    total.value = r.total || 0
    expandedId.value = null
  } catch (e) {
    ElMessage.error(e?.response?.data?.message || e.message || '查询失败')
  } finally {
    loading.value = false
  }
}

function goPage(n) {
  if (n < 1 || n > totalPages.value) return
  page.value = n
  reload()
}

function resetFilters() {
  qUsername.value = ''
  qAction.value = ''
  qSince.value = ''
  qUntil.value = ''
  qKeyword.value = ''
  page.value = 1
  reload()
}

function formatDetail(s) {
  if (!s) return ''
  try {
    return JSON.stringify(JSON.parse(s), null, 2)
  } catch {
    return s
  }
}

// 按 action 前缀给 tag 染色：失败/危险类红、登录类紫、其他蓝
function actClass(action) {
  if (!action) return ''
  if (action.includes('.failed') || action.includes('.denied') || action.endsWith('.delete')) return 'danger'
  if (action.startsWith('auth.')) return 'auth'
  if (action.startsWith('deploy.')) return 'deploy'
  return 'normal'
}

async function loadActionTypes() {
  try {
    const r = await listAuditActionTypes()
    actionList.value = r.actions || []
  } catch { /* 忽略 */ }
}

onMounted(() => {
  loadActionTypes()
  reload()
})
</script>

<style scoped>
.al-panel { padding: 18px 22px; }

.al-hd { display: flex; justify-content: space-between; align-items: center; margin-bottom: 14px; }
.al-title { font-size: 14px; font-weight: 600; color: #1f2937; display: flex; align-items: baseline; gap: 12px; }
.al-sub { font-size: 11.5px; color: #94a3b8; font-weight: 400; }

.al-filters {
  display: flex; gap: 12px; align-items: flex-end;
  padding: 12px 14px; background: #fafbfc; border: 1px solid #e5e7eb; border-radius: 6px;
  margin-bottom: 14px; flex-wrap: wrap;
}
.al-f { display: flex; flex-direction: column; gap: 4px; }
.al-f.flex1 { flex: 1; min-width: 200px; }
.al-f label { font-size: 11px; color: #6b7280; font-weight: 500; }
.al-f input {
  padding: 5px 9px; border: 1px solid #d1d5db; border-radius: 4px;
  font-size: 12.5px; width: 160px;
}
.al-f input:focus { outline: none; border-color: #1890ff; }
.al-f.flex1 input { width: 100%; }

.al-table-wrap {
  border: 1px solid #e5e7eb; border-radius: 6px; overflow: auto;
  max-height: 600px; background: #fff;
}
.al-table { width: 100%; border-collapse: collapse; font-size: 12.5px; }
.al-table thead { position: sticky; top: 0; z-index: 1; }
.al-table th {
  background: #f9fafb; text-align: left; padding: 9px 12px;
  border-bottom: 1px solid #e5e7eb; color: #6b7280; font-weight: 500;
  font-size: 11px; text-transform: uppercase; letter-spacing: .4px;
}
.al-table td { padding: 9px 12px; border-bottom: 1px solid #f1f5f9; vertical-align: top; }
.al-table tr.expanded td { background: #f0f9ff; }
.al-table tr:hover td { background: #fafbfc; }

.mono { font-family: 'Fira Code', monospace; font-size: 11.5px; }
.mute { color: #94a3b8; }

.user-chip {
  font-family: 'Fira Code', monospace;
  background: #f3f4f6; padding: 2px 8px; border-radius: 99px;
  font-size: 11px; color: #1f2937;
}
.src-tag {
  margin-left: 4px; font-size: 9.5px; padding: 1px 6px;
  border-radius: 3px; font-family: 'Fira Code', monospace;
}
.src-tag.portal { background: #f3e8ff; color: #6b21a8; }

.act-tag {
  font-family: 'Fira Code', monospace; font-size: 11px;
  padding: 2px 8px; border-radius: 3px; font-weight: 500;
}
.act-tag.normal { background: #eff6ff; color: #1d4ed8; }
.act-tag.auth { background: #f3e8ff; color: #6b21a8; }
.act-tag.deploy { background: #ecfdf5; color: #059669; }
.act-tag.danger { background: #fef2f2; color: #b91c1c; }

.target-cell { display: flex; gap: 6px; align-items: center; flex-wrap: wrap; }
.target-type {
  background: #f3f4f6; color: #6b7280; padding: 1px 7px;
  border-radius: 3px; font-size: 11px;
}
.target-name { font-family: 'Fira Code', monospace; color: #1f2937; font-weight: 500; font-size: 12px; }

.link-btn {
  background: none; border: none; cursor: pointer;
  color: #1890ff; font-size: 11.5px; padding: 0;
}
.link-btn:hover { text-decoration: underline; }

.detail-row td { background: #fafbfc !important; padding: 0 !important; }
.detail-pre {
  margin: 0; padding: 12px 14px;
  background: #1e1e1e; color: #d4d4d4;
  font-family: 'Fira Code', monospace; font-size: 11.5px; line-height: 1.7;
  white-space: pre-wrap; word-break: break-all; max-height: 400px; overflow: auto;
}

.empty-row { text-align: center; color: #94a3b8; padding: 40px 0; }

.al-foot {
  display: flex; justify-content: space-between; align-items: center;
  margin-top: 12px; font-size: 12px; color: #6b7280;
}
.al-foot .hint b { color: #1f2937; font-family: 'Fira Code', monospace; }
.page-ctrl { display: flex; gap: 8px; align-items: center; }
.page-ctrl .sel { padding: 4px 8px; border: 1px solid #d1d5db; border-radius: 4px; font-size: 12px; background: #fff; }

.btn {
  background: #fff; border: 1px solid #e5e7eb; color: #1f2937;
  padding: 5px 12px; border-radius: 4px; font-size: 12px; cursor: pointer;
}
.btn:hover:not(:disabled) { border-color: #1890ff; color: #1890ff; }
.btn:disabled { opacity: .4; cursor: not-allowed; }
.btn.sm { padding: 4px 10px; font-size: 11.5px; }
</style>
