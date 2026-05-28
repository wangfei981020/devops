<template>
  <div class="app">
    <aside class="sidebar">
      <div class="brand">
        <div class="brand-icon">G</div>
        <div class="brand-n">GKE 版本监控</div>
      </div>
      <nav class="nav">
        <RouterLink to="/" class="nav-item" active-class="active" exact-active-class="active">
          <el-icon class="ico"><Files /></el-icon>
          <span>集群列表</span>
        </RouterLink>
        <RouterLink to="/settings" class="nav-item" active-class="active">
          <el-icon class="ico"><Setting /></el-icon>
          <span>设置</span>
        </RouterLink>
      </nav>
      <div class="sidebar-foot">v1</div>
    </aside>
    <main class="main">
      <div class="topbar">
        <div class="crumb">GKE 版本监控 / <b>{{ $route.meta.title || '集群列表' }}</b></div>
      </div>
      <div class="content">
        <RouterView />
      </div>
    </main>
  </div>
</template>
<script setup>
import { Files, Setting } from '@element-plus/icons-vue'
</script>
<style scoped>
.app { display: flex; height: 100vh; }

.sidebar { width: 220px; background: var(--sidebar-bg); flex-shrink: 0; display: flex; flex-direction: column; }
.brand { padding: 18px 20px; border-bottom: 1px solid rgba(255, 255, 255, 0.06); display: flex; align-items: center; gap: 10px; }
.brand-icon { width: 32px; height: 32px; background: var(--primary); border-radius: 6px; color: #fff; font-weight: 700; font-size: 18px; display: flex; align-items: center; justify-content: center; flex-shrink: 0; }
.brand-n { color: #fff; font-weight: 600; font-size: 14px; }

.nav { padding: 12px 0; flex: 1; overflow-y: auto; }
.nav-item {
  display: flex; align-items: center; padding: 10px 16px; gap: 10px;
  color: var(--sidebar-text); font-size: 13px; font-weight: 500;
  text-decoration: none; border-left: 2px solid transparent; transition: all .12s;
}
.nav-item .ico { font-size: 15px; opacity: .85; }
.nav-item:hover { background: var(--sidebar-hover); color: #fff; }
.nav-item.active { background: var(--sidebar-active-bg); border-left-color: var(--primary); color: #fff; }

.sidebar-foot { padding: 12px 16px; border-top: 1px solid rgba(255, 255, 255, 0.06); font-size: 11px; color: rgba(255, 255, 255, 0.4); font-family: var(--mono); }

.main { flex: 1; display: flex; flex-direction: column; overflow: hidden; }
.topbar { height: 52px; background: var(--bg-card); border-bottom: 1px solid var(--border); display: flex; align-items: center; padding: 0 24px; flex-shrink: 0; }
.crumb { font-size: 13px; color: var(--text-2); }
.crumb b { color: var(--text); font-weight: 600; margin-left: 2px; }
.content { flex: 1; overflow: auto; padding: 20px 24px; }
</style>
