<template>
  <div class="app">
    <aside class="sidebar">
      <div class="logo">
        <div class="logo-icon">
          <svg viewBox="0 0 24 24" fill="none" stroke="#fff" stroke-width="2.5" width="16" height="16" stroke-linecap="round" stroke-linejoin="round">
            <path d="m12 15-3-3a22 22 0 0 1 2-3.95A12.88 12.88 0 0 1 22 2c0 2.72-.78 7.5-6 11a22.35 22.35 0 0 1-4 2z"/>
          </svg>
        </div>
        <div>
          <div class="logo-text">Deploy Center</div>
          <div class="logo-sub">GitOps Console</div>
        </div>
      </div>

      <el-menu
        :default-active="$route.path"
        :default-openeds="['deploy', 'config']"
        background-color="#1f2937"
        text-color="#cbd5e1"
        active-text-color="#ffffff"
        router
        class="menu"
      >
        <el-sub-menu index="deploy">
          <template #title>
            <el-icon><Upload /></el-icon>
            <span>发布管理</span>
          </template>
          <el-menu-item index="/deploy">部署控制台</el-menu-item>
          <el-menu-item index="/history">发布历史</el-menu-item>
        </el-sub-menu>

        <el-sub-menu index="config">
          <template #title>
            <el-icon><Setting /></el-icon>
            <span>配置管理</span>
          </template>
          <el-menu-item index="/projects">项目配置</el-menu-item>
          <el-menu-item index="/settings">系统设置</el-menu-item>
        </el-sub-menu>
      </el-menu>

      <div class="sidebar-footer">
        <span>v30</span>
        <span>{{ today }}</span>
      </div>
    </aside>

    <main class="main">
      <div class="topbar">
        <div class="crumb">部署中心 / <b>{{ $route.meta.title }}</b></div>
        <div class="topbar-right">
          <span>当前用户</span>
          <span class="user-chip">system</span>
          <span class="ver-badge">v30</span>
        </div>
      </div>
      <div class="content">
        <RouterView />
      </div>
    </main>
  </div>
</template>

<script setup>
import { Upload, Setting } from '@element-plus/icons-vue'
import dayjs from 'dayjs'
const today = dayjs().format('YYYY-MM-DD')
</script>

<style scoped>
.app { display: flex; height: 100vh; }
.sidebar {
  width: 220px;
  background: var(--sidebar-bg);
  color: var(--sidebar-text);
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  border-right: 1px solid #2a3545;
}
.logo {
  padding: 18px 20px;
  border-bottom: 1px solid #2a3545;
  display: flex;
  align-items: center;
  gap: 10px;
}
.logo-icon {
  width: 28px; height: 28px;
  background: linear-gradient(135deg, #1890ff, #36cfc9);
  border-radius: 6px;
  display: flex; align-items: center; justify-content: center;
}
.logo-text { color: #fff; font-weight: 700; font-size: 14px; letter-spacing: .3px; }
.logo-sub { color: #64748b; font-size: 11px; font-family: var(--mono); }

.menu { flex: 1; border-right: none !important; overflow-y: auto; }

/* Element Plus dark menu 微调 */
.menu :deep(.el-sub-menu__title) {
  font-size: 13px;
  font-weight: 500;
  height: 44px;
  line-height: 44px;
}
.menu :deep(.el-sub-menu__title:hover) {
  background-color: #374151 !important;
}
.menu :deep(.el-menu-item) {
  font-size: 13px;
  height: 38px;
  line-height: 38px;
  padding-left: 48px !important;
  color: var(--sidebar-text);
}
.menu :deep(.el-menu-item:hover) {
  background-color: #374151 !important;
  color: #fff !important;
}
.menu :deep(.el-menu-item.is-active) {
  background-color: var(--sidebar-active) !important;
  color: #fff !important;
  font-weight: 500;
}
.menu :deep(.el-sub-menu .el-menu) {
  background-color: #18212e !important;
}

.sidebar-footer {
  padding: 12px 16px;
  border-top: 1px solid #2a3545;
  font-size: 11px;
  color: #64748b;
  display: flex;
  justify-content: space-between;
  font-family: var(--mono);
}
.main { flex: 1; display: flex; flex-direction: column; overflow: hidden; }
.topbar {
  height: 52px;
  background: #fff;
  border-bottom: 1px solid var(--border);
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 20px;
  flex-shrink: 0;
}
.crumb { font-size: 13px; color: var(--text-2); }
.crumb b { color: var(--text); font-weight: 600; }
.topbar-right { display: flex; align-items: center; gap: 14px; font-size: 12px; color: var(--text-2); }
.user-chip { display: flex; align-items: center; padding: 4px 10px; background: #f3f4f6; border-radius: 99px; font-family: var(--mono); }
.ver-badge { padding: 2px 6px; background: #eff6ff; color: #1890ff; border-radius: 4px; font-family: var(--mono); font-size: 11px; }
.content { flex: 1; overflow: auto; padding: 16px 20px; }
</style>
