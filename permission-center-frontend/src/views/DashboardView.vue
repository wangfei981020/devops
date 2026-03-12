<script setup>
import { ref, onMounted } from 'vue'
import api from '@/api'

const stats = ref({ users: 0, roles: 0, services: 0, permissions: 0 })

onMounted(async () => {
  try {
    const [usersRes, rolesRes, servicesRes, permsRes] = await Promise.all([
      api.get('/api/admin/users'),
      api.get('/api/admin/roles'),
      api.get('/api/admin/services'),
      api.get('/api/admin/permissions')
    ])
    stats.value = {
      users: usersRes.data?.data?.length || 0,
      roles: rolesRes.data?.data?.length || 0,
      services: servicesRes.data?.data?.length || 0,
      permissions: permsRes.data?.data?.length || 0
    }
  } catch (e) {
    console.error('Failed to load stats:', e)
  }
})
</script>

<template>
  <div class="dashboard">
    <div class="stats-grid">
      <div class="stat-card">
        <div class="stat-icon">👥</div>
        <div class="stat-info">
          <div class="stat-value">{{ stats.users }}</div>
          <div class="stat-label">用户</div>
        </div>
      </div>
      <div class="stat-card">
        <div class="stat-icon">🔑</div>
        <div class="stat-info">
          <div class="stat-value">{{ stats.roles }}</div>
          <div class="stat-label">角色</div>
        </div>
      </div>
      <div class="stat-card">
        <div class="stat-icon">🔗</div>
        <div class="stat-info">
          <div class="stat-value">{{ stats.services }}</div>
          <div class="stat-label">服务</div>
        </div>
      </div>
      <div class="stat-card">
        <div class="stat-icon">🛡️</div>
        <div class="stat-info">
          <div class="stat-value">{{ stats.permissions }}</div>
          <div class="stat-label">权限</div>
        </div>
      </div>
    </div>

    <div class="info-section">
      <h3>系统说明</h3>
      <ul>
        <li><strong>用户管理</strong> — 查看和管理系统用户，分配角色</li>
        <li><strong>角色管理</strong> — 创建角色，分配权限组合</li>
        <li><strong>权限管理</strong> — 按服务管理菜单、按钮、数据权限</li>
        <li><strong>服务管理</strong> — 注册接入的服务，管理 API Key</li>
        <li><strong>审计日志</strong> — 查看所有操作记录</li>
      </ul>
    </div>
  </div>
</template>

<style scoped>
.dashboard { max-width: 1000px; }
.stats-grid {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 16px;
  margin-bottom: 32px;
}
.stat-card {
  background: #1e293b;
  border: 1px solid rgba(255,255,255,0.06);
  border-radius: 12px;
  padding: 20px;
  display: flex;
  align-items: center;
  gap: 16px;
}
.stat-icon { font-size: 32px; }
.stat-value { font-size: 28px; font-weight: 700; color: #f1f5f9; }
.stat-label { font-size: 13px; color: #64748b; margin-top: 2px; }

.info-section {
  background: #1e293b;
  border: 1px solid rgba(255,255,255,0.06);
  border-radius: 12px;
  padding: 24px;
}
.info-section h3 { margin: 0 0 16px; font-size: 16px; color: #f1f5f9; }
.info-section ul { margin: 0; padding-left: 20px; }
.info-section li { margin-bottom: 8px; font-size: 14px; color: #94a3b8; line-height: 1.6; }
.info-section strong { color: #e2e8f0; }
</style>
