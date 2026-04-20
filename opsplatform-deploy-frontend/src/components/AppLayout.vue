<template>
  <div class="app">
    <aside class="sidebar">
      <div class="brand">
        <div class="brand-logo">D</div>
        <div class="brand-n">Deploy</div>
        <div class="brand-t">v{{ version }}</div>
      </div>

      <nav class="nav">
        <div class="group">
          <div class="group-title opened">
            <el-icon class="ico"><Upload /></el-icon>
            <span>发布管理</span>
            <el-icon class="chev"><ArrowRight /></el-icon>
          </div>
          <RouterLink to="/deploy" class="sub-item" active-class="active">部署控制台</RouterLink>
          <RouterLink to="/history" class="sub-item" active-class="active">发布历史</RouterLink>
        </div>

        <div class="group">
          <div class="group-title opened">
            <el-icon class="ico"><Setting /></el-icon>
            <span>配置管理</span>
            <el-icon class="chev"><ArrowRight /></el-icon>
          </div>
          <RouterLink to="/projects" class="sub-item" active-class="active">项目配置</RouterLink>
          <RouterLink to="/settings" class="sub-item" active-class="active">系统设置</RouterLink>
        </div>
      </nav>

      <div class="sidebar-foot">
        <span>system</span>
        <span>v{{ version }}</span>
      </div>
    </aside>

    <main class="main">
      <div class="topbar">
        <div class="crumb">部署中心 / <b>{{ $route.meta.title }}</b></div>
        <div class="topbar-r">
          <span>当前用户</span>
          <span class="user-chip">
            <el-icon><User /></el-icon>
            system
          </span>
          <span class="ver-badge">v{{ version }}</span>
        </div>
      </div>
      <div class="content">
        <RouterView />
      </div>
    </main>
  </div>
</template>

<script setup>
import { Upload, Setting, ArrowRight, User } from '@element-plus/icons-vue'
const version = '34'
</script>

<style scoped>
.app { display: flex; height: 100vh; }

/* ===== Sidebar · 实心深色 ===== */
.sidebar {
  width: 220px;
  background: var(--sidebar-bg);
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
}
.brand {
  padding: 18px 20px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.06);
  display: flex; align-items: center; gap: 10px;
}
.brand-logo {
  width: 30px; height: 30px;
  background: var(--primary);
  border-radius: 6px;
  display: flex; align-items: center; justify-content: center;
  color: #fff; font-weight: 700; font-size: 14px;
}
.brand-n { color: #fff; font-weight: 600; font-size: 14px; }
.brand-t {
  margin-left: auto;
  font-family: var(--mono); font-size: 10px; font-weight: 500;
  color: rgba(255, 255, 255, 0.4);
  padding: 1px 6px;
  background: rgba(255, 255, 255, 0.05);
  border-radius: 3px;
}

.nav { padding: 12px 0; flex: 1; overflow-y: auto; }
.group { margin-bottom: 4px; }
.group-title {
  display: flex; align-items: center;
  padding: 10px 16px;
  color: var(--sidebar-text);
  font-size: 13px; font-weight: 500;
  gap: 10px;
  cursor: pointer;
}
.group-title:hover { background: var(--sidebar-hover); color: #fff; }
.group-title .ico { font-size: 15px; opacity: .8; }
.group-title .chev { margin-left: auto; opacity: .5; font-size: 12px; transition: transform .15s; }
.group-title.opened .chev { transform: rotate(90deg); }

.sub-item {
  display: block;
  padding: 8px 16px 8px 42px;
  color: var(--sidebar-text-sub);
  font-size: 12.5px;
  text-decoration: none;
  border-left: 2px solid transparent;
  transition: all .12s;
}
.sub-item:hover { color: #fff; background: var(--sidebar-hover); }
.sub-item.active {
  color: #fff;
  background: var(--sidebar-active-bg);
  border-left-color: var(--primary);
  font-weight: 500;
}

.sidebar-foot {
  padding: 12px 16px;
  border-top: 1px solid rgba(255, 255, 255, 0.06);
  font-size: 11px;
  color: rgba(255, 255, 255, 0.4);
  font-family: var(--mono);
  display: flex; justify-content: space-between;
}

/* ===== Main ===== */
.main { flex: 1; display: flex; flex-direction: column; overflow: hidden; }
.topbar {
  height: 52px;
  background: var(--bg-card);
  border-bottom: 1px solid var(--border);
  display: flex; align-items: center; justify-content: space-between;
  padding: 0 24px;
  flex-shrink: 0;
}
.crumb { font-size: 13px; color: var(--text-2); }
.crumb b { color: var(--text); font-weight: 600; margin-left: 2px; }

.topbar-r { display: flex; align-items: center; gap: 14px; font-size: 12px; color: var(--text-3); }
.user-chip {
  display: flex; align-items: center; gap: 6px;
  padding: 4px 10px;
  background: var(--bg-hover);
  border-radius: 99px;
  font-family: var(--mono);
  color: var(--text-2);
}
.ver-badge {
  padding: 2px 7px;
  background: var(--primary-bg);
  color: var(--primary);
  border-radius: 4px;
  font-family: var(--mono);
  font-size: 11px;
  font-weight: 600;
}

.content { flex: 1; overflow: auto; padding: 20px 24px; }
</style>
