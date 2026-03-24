<template>
  <div>
    <div class="stats-grid">
      <div class="stat-card">
        <div class="label">告警规则</div>
        <div class="value primary">{{ stats.rules_total || 0 }}</div>
        <div class="text-sm text-secondary">启用: {{ stats.rules_enabled || 0 }}</div>
      </div>
      <div class="stat-card">
        <div class="label">今日告警</div>
        <div class="value warning">{{ stats.today_alerts || 0 }}</div>
        <div class="text-sm text-secondary">
          成功: {{ stats.today_success || 0 }} / 失败: {{ stats.today_failed || 0 }}
        </div>
      </div>
      <div class="stat-card">
        <div class="label">ES 连接</div>
        <div class="value info">{{ stats.es_connections || 0 }}</div>
        <div class="text-sm text-secondary">活跃: {{ stats.es_active || 0 }}</div>
      </div>
      <div class="stat-card">
        <div class="label">Lark 配置</div>
        <div class="value success">{{ stats.lark_configs || 0 }}</div>
        <div class="text-sm text-secondary">活跃: {{ stats.lark_active || 0 }}</div>
      </div>
    </div>

    <div class="card">
      <div class="card-header">
        <div class="card-title">快速操作</div>
      </div>
      <div class="flex gap-4">
        <router-link to="/alert-rules/create" class="btn btn-primary">
          <Plus :size="16" /> 新建告警规则
        </router-link>
        <router-link to="/es-connections" class="btn btn-outline">
          <Database :size="16" /> 管理 ES 连接
        </router-link>
        <router-link to="/lark-configs" class="btn btn-outline">
          <Send :size="16" /> 管理 Lark 配置
        </router-link>
        <router-link to="/alert-logs" class="btn btn-outline">
          <FileText :size="16" /> 查看告警日志
        </router-link>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import api from '../api'
import { Plus, Database, Send, FileText } from 'lucide-vue-next'

const stats = ref({})

onMounted(async () => {
  try {
    const res = await api.get('/stats')
    if (res.code === 0) stats.value = res.data
  } catch (e) { /* ignore */ }
})
</script>
