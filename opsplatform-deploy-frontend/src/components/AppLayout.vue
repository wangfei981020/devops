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
      <nav class="nav">
        <div class="nav-group">WORKSPACE</div>
        <RouterLink to="/deploy" class="nav-item" active-class="active">
          <el-icon><Upload /></el-icon>部署控制台
        </RouterLink>
        <RouterLink to="/projects" class="nav-item" active-class="active">
          <el-icon><FolderOpened /></el-icon>项目配置
        </RouterLink>
        <RouterLink to="/history" class="nav-item" active-class="active">
          <el-icon><Clock /></el-icon>发布历史
        </RouterLink>
        <div class="nav-group">SYSTEM</div>
        <RouterLink to="/settings" class="nav-item" active-class="active">
          <el-icon><Setting /></el-icon>系统设置
        </RouterLink>
      </nav>
      <div class="sidebar-footer">
        <span>v20</span>
        <span>{{ today }}</span>
      </div>
    </aside>
    <main class="main">
      <div class="topbar">
        <div class="crumb">部署中心 / <b>{{ $route.meta.title }}</b></div>
        <div class="topbar-right">
          <span>当前用户</span>
          <span class="user-chip">system</span>
          <span class="ver-badge">v20</span>
        </div>
      </div>
      <div class="content">
        <RouterView />
      </div>
    </main>
  </div>
</template>

<script setup>
import { Upload, FolderOpened, Clock, Setting } from '@element-plus/icons-vue'
import dayjs from 'dayjs'
const today = dayjs().format('YYYY-MM-DD')
</script>

<style scoped>
.app { display: flex; height: 100vh; }
.sidebar { width: 220px; background: var(--sidebar-bg); color: var(--sidebar-text); flex-shrink: 0; display: flex; flex-direction: column; }
.logo { padding: 18px 20px; border-bottom: 1px solid #2a3545; display: flex; align-items: center; gap: 10px; }
.logo-icon { width: 28px; height: 28px; background: linear-gradient(135deg, #1890ff, #36cfc9); border-radius: 6px; display: flex; align-items: center; justify-content: center; }
.logo-text { color: #fff; font-weight: 700; font-size: 14px; letter-spacing: .3px; }
.logo-sub { color: #64748b; font-size: 11px; font-family: var(--mono); }
.nav { padding: 12px 0; flex: 1; }
.nav-group { padding: 0 12px; margin-bottom: 4px; color: #64748b; font-size: 10px; text-transform: uppercase; letter-spacing: 1px; margin-top: 14px; }
.nav-item { display: flex; align-items: center; gap: 10px; padding: 9px 16px; margin: 2px 8px; border-radius: 6px; cursor: pointer; transition: all .15s; font-size: 13px; color: var(--sidebar-text); text-decoration: none; }
.nav-item:hover { background: var(--sidebar-hover); color: #fff; }
.nav-item.active { background: var(--sidebar-active); color: #fff; font-weight: 500; }
.sidebar-footer { padding: 12px 16px; border-top: 1px solid #2a3545; font-size: 11px; color: #64748b; display: flex; justify-content: space-between; font-family: var(--mono); }
.main { flex: 1; display: flex; flex-direction: column; overflow: hidden; }
.topbar { height: 52px; background: #fff; border-bottom: 1px solid var(--border); display: flex; align-items: center; justify-content: space-between; padding: 0 20px; flex-shrink: 0; }
.crumb { font-size: 13px; color: var(--text-2); }
.crumb b { color: var(--text); font-weight: 600; }
.topbar-right { display: flex; align-items: center; gap: 14px; font-size: 12px; color: var(--text-2); }
.user-chip { display: flex; align-items: center; padding: 4px 10px; background: #f3f4f6; border-radius: 99px; font-family: var(--mono); }
.ver-badge { padding: 2px 6px; background: #eff6ff; color: #1890ff; border-radius: 4px; font-family: var(--mono); font-size: 11px; }
.content { flex: 1; overflow: auto; padding: 16px 20px; }
</style>
