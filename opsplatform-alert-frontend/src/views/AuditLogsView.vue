<template>
  <div>
    <div class="card">
      <div class="card-header">
        <div class="card-title">操作日志</div>
        <div class="flex gap-2">
          <input v-model="filters.username" class="form-input" style="width: 150px;" placeholder="用户名" @keyup.enter="loadLogs" />
          <select v-model="filters.action" class="form-select" style="width: 150px;" @change="loadLogs">
            <option value="">全部操作</option>
            <optgroup label="登录">
              <option value="login">本地登录</option>
              <option value="portal_login">SSO登录</option>
            </optgroup>
            <optgroup label="告警规则">
              <option value="create_rule">创建规则</option>
              <option value="update_rule">更新规则</option>
              <option value="delete_rule">删除规则</option>
              <option value="enable_rule">启用规则</option>
              <option value="disable_rule">禁用规则</option>
            </optgroup>
            <optgroup label="连接管理">
              <option value="create_es_conn">创建ES连接</option>
              <option value="update_es_conn">更新ES连接</option>
              <option value="delete_es_conn">删除ES连接</option>
              <option value="create_loki_conn">创建Loki连接</option>
              <option value="update_loki_conn">更新Loki连接</option>
              <option value="delete_loki_conn">删除Loki连接</option>
            </optgroup>
            <optgroup label="Lark配置">
              <option value="create_lark">创建Lark</option>
              <option value="update_lark">更新Lark</option>
              <option value="delete_lark">删除Lark</option>
            </optgroup>
            <optgroup label="屏蔽管理">
              <option value="create_mute">添加屏蔽</option>
              <option value="delete_mute">取消屏蔽</option>
            </optgroup>
            <optgroup label="用户管理">
              <option value="create_user">创建用户</option>
              <option value="update_user">更新用户</option>
              <option value="delete_user">删除用户</option>
              <option value="reset_password">重置密码</option>
            </optgroup>
            <optgroup label="通知人">
              <option value="create_contact">创建通知人</option>
              <option value="delete_contact">删除通知人</option>
              <option value="batch_create_contacts">批量创建</option>
            </optgroup>
          </select>
          <button class="btn btn-outline btn-sm" @click="loadLogs">查询</button>
        </div>
      </div>

      <div v-if="loading" class="loading"><div class="spinner"></div></div>

      <div v-else class="table-wrapper">
        <table>
          <thead>
            <tr>
              <th>时间</th>
              <th>用户</th>
              <th>来源</th>
              <th>操作</th>
              <th>目标类型</th>
              <th>目标</th>
              <th>详情</th>
              <th>IP</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="log in logs" :key="log.id">
              <td class="text-sm" style="white-space: nowrap;">{{ formatTime(log.created_at) }}</td>
              <td style="font-weight: 500;">{{ log.username }}</td>
              <td>
                <span class="badge" :class="log.auth_source === 'portal' ? 'badge-success' : 'badge-info'" style="font-size: 11px;">
                  {{ log.auth_source === 'portal' ? 'SSO' : '本地' }}
                </span>
              </td>
              <td>
                <span class="badge" :class="actionClass(log.action)">{{ actionLabel(log.action) }}</span>
              </td>
              <td class="text-sm text-secondary">{{ log.target_type || '-' }}</td>
              <td class="text-sm">{{ log.target_name || '-' }}</td>
              <td class="text-sm text-secondary">{{ log.detail || '-' }}</td>
              <td class="text-sm text-secondary">{{ log.ip || '-' }}</td>
            </tr>
          </tbody>
        </table>

        <div v-if="logs.length === 0" class="empty-state" style="padding: 40px;">
          <p>暂无操作日志</p>
        </div>

        <div v-if="total > pageSize" class="flex justify-between items-center" style="padding: 12px 0;">
          <span class="text-sm text-secondary">共 {{ total }} 条</span>
          <div class="flex gap-2">
            <button class="btn btn-sm btn-outline" :disabled="page <= 1" @click="page--; loadLogs()">上一页</button>
            <span class="text-sm" style="line-height: 32px;">{{ page }} / {{ Math.ceil(total / pageSize) }}</span>
            <button class="btn btn-sm btn-outline" :disabled="page >= Math.ceil(total / pageSize)" @click="page++; loadLogs()">下一页</button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import api from '../api'

const logs = ref([])
const loading = ref(false)
const total = ref(0)
const page = ref(1)
const pageSize = 50
const filters = ref({ username: '', action: '' })

async function loadLogs() {
  loading.value = true
  try {
    const params = { page: page.value, page_size: pageSize }
    if (filters.value.action) params.action = filters.value.action
    if (filters.value.username) params.username = filters.value.username
    const res = await api.get('/audit-logs', { params })
    if (res.code === 0) {
      logs.value = res.data.list
      total.value = res.data.total
    }
  } catch (e) { /* ignore */ }
  loading.value = false
}

const actionMap = {
  login: ['登录', 'badge-info'],
  portal_login: ['SSO登录', 'badge-success'],
  create_rule: ['创建规则', 'badge-primary'],
  update_rule: ['更新规则', 'badge-warning'],
  delete_rule: ['删除规则', 'badge-danger'],
  enable_rule: ['启用规则', 'badge-success'],
  disable_rule: ['禁用规则', 'badge-danger'],
  create_es_conn: ['创建ES连接', 'badge-primary'],
  update_es_conn: ['更新ES连接', 'badge-warning'],
  delete_es_conn: ['删除ES连接', 'badge-danger'],
  toggle_es_conn: ['切换ES连接', 'badge-info'],
  create_loki_conn: ['创建Loki连接', 'badge-primary'],
  update_loki_conn: ['更新Loki连接', 'badge-warning'],
  delete_loki_conn: ['删除Loki连接', 'badge-danger'],
  toggle_loki_conn: ['切换Loki连接', 'badge-info'],
  create_lark: ['创建Lark', 'badge-primary'],
  update_lark: ['更新Lark', 'badge-warning'],
  delete_lark: ['删除Lark', 'badge-danger'],
  toggle_lark: ['切换Lark', 'badge-info'],
  create_mute: ['添加屏蔽', 'badge-warning'],
  delete_mute: ['取消屏蔽', 'badge-info'],
  create_user: ['创建用户', 'badge-primary'],
  update_user: ['更新用户', 'badge-warning'],
  delete_user: ['删除用户', 'badge-danger'],
  toggle_user: ['切换用户状态', 'badge-info'],
  reset_password: ['重置密码', 'badge-warning'],
  create_contact: ['创建通知人', 'badge-primary'],
  update_contact: ['更新通知人', 'badge-warning'],
  delete_contact: ['删除通知人', 'badge-danger'],
  batch_create_contacts: ['批量创建通知人', 'badge-primary'],
}

function actionLabel(action) {
  return actionMap[action]?.[0] || action
}
function actionClass(action) {
  return actionMap[action]?.[1] || 'badge-info'
}

function formatTime(t) {
  if (!t) return '-'
  return new Date(t).toLocaleString('zh-CN')
}

onMounted(loadLogs)
</script>
