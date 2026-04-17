<template>
  <div class="app-layout">
    <Sidebar />
    <div class="main-area">
      <header class="top-header">
        <div class="header-breadcrumb">
          <span class="breadcrumb-item">发布管理平台</span>
          <span class="breadcrumb-sep">/</span>
          <span class="breadcrumb-item">{{ pageGroup }}</span>
          <span class="breadcrumb-sep">/</span>
          <span class="breadcrumb-current">{{ pageTitle }}</span>
        </div>
      </header>
      <main class="page-content">
        <router-view />
      </main>
    </div>
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import Sidebar from './Sidebar.vue'

const route = useRoute()

const titleMap = {
  '/dashboard': { group: '仪表盘', title: '概览' },
  '/projects': { group: '部署管理', title: '应用管理' },
  '/modules': { group: '部署管理', title: '模块详情' },
  '/secrets': { group: '部署管理', title: 'Secret 管理' },
  '/deployments': { group: '部署管理', title: '发布历史' },
  '/chart-templates': { group: '配置', title: 'Chart 模板' },
  '/env-templates': { group: '配置', title: '环境变量模板' },
  '/environments': { group: '配置', title: '环境字典' },
  '/contacts': { group: '通知', title: '通知人' },
  '/lark-configs': { group: '通知', title: 'Lark 群' },
  '/global-config': { group: '系统', title: '全局配置' }
}

const pageInfo = computed(() => {
  for (const [path, info] of Object.entries(titleMap)) {
    if (route.path.startsWith(path)) return info
  }
  return { group: '', title: '首页' }
})
const pageGroup = computed(() => pageInfo.value.group)
const pageTitle = computed(() => pageInfo.value.title)
</script>

<style scoped>
.app-layout { min-height: 100vh; background: #f3f4f6; }
.main-area { margin-left: 200px; min-height: 100vh; transition: margin-left 0.2s; }

/* 当 sidebar 折叠时, 让主内容区也跟着移动 — 用 CSS 变量替代更优雅, 但这里用 :has 简单处理 */
.app-layout:has(.sidebar.collapsed) .main-area { margin-left: 64px; }

.top-header {
  height: 52px;
  background: white;
  padding: 0 24px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  border-bottom: 1px solid #e5e7eb;
  position: sticky;
  top: 0;
  z-index: 50;
}

.header-breadcrumb { display: flex; align-items: center; gap: 8px; color: #6b7280; font-size: 13px; }
.breadcrumb-item { color: #6b7280; }
.breadcrumb-sep { color: #d1d5db; }
.breadcrumb-current { color: #1f2937; font-weight: 600; }

.page-content { padding: 20px 24px; }
</style>
