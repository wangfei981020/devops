<template>
  <router-view v-if="route.path === '/login'" />

  <div v-else class="app">
    <aside class="sidebar" :class="{ collapsed }">
      <div class="brand">
        <el-icon :size="20" color="#3b82f6"><Files /></el-icon>
        <span v-show="!collapsed" class="brand-n">CMDB</span>
        <span v-show="!collapsed" class="brand-t">{{ version }}</span>
      </div>
      <el-menu class="nav" :default-active="route.path" router :collapse="collapsed" :collapse-transition="false"
               :default-openeds="['资产管理','系统管理']"
               background-color="transparent" text-color="#c0c4d0" active-text-color="#fff">
        <template v-for="m in menus" :key="m.label || m.path">
          <el-menu-item v-if="m.type === 'item'" :index="m.path">
            <el-icon><component :is="m.icon" /></el-icon>
            <template #title>{{ m.label }}</template>
          </el-menu-item>
          <el-sub-menu v-else :index="m.label">
            <template #title><el-icon><component :is="m.icon" /></el-icon><span>{{ m.label }}</span></template>
            <el-menu-item v-for="ch in m.children" :key="ch.path" :index="ch.path">
              <el-icon><component :is="ch.icon" /></el-icon>
              <template #title>{{ ch.label }}</template>
            </el-menu-item>
          </el-sub-menu>
        </template>
      </el-menu>
      <div class="foot" @click="toggle">
        <el-icon><Fold v-if="!collapsed" /><Expand v-else /></el-icon>
        <span v-show="!collapsed">收起菜单</span>
      </div>
    </aside>

    <main class="main">
      <div class="topbar">
        <div class="crumb">CMDB / <b>{{ route.meta.title || '总览' }}</b></div>
        <el-dropdown trigger="click" @command="onCmd">
          <span class="user"><el-icon><User /></el-icon> {{ auth.user?.username || 'admin' }} <el-icon><ArrowDown /></el-icon></span>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item command="logout"><el-icon><SwitchButton /></el-icon> 退出登录</el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
      </div>
      <div class="content"><router-view /></div>
    </main>
  </div>
</template>

<script setup>
import { shallowRef, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Odometer, Connection, Lock, Share, DataAnalysis, Grid, Setting, Files, User, ArrowDown, SwitchButton, Fold, Expand, Coin, Tools, List, CircleCheck, Clock, Bell, Monitor, Tickets } from '@element-plus/icons-vue'
import { useAuthStore } from './stores/auth'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const version = import.meta.env.VITE_APP_VERSION || 'dev'
const collapsed = ref(localStorage.getItem('cmdb_menu_collapsed') === '1')
function toggle() { collapsed.value = !collapsed.value; localStorage.setItem('cmdb_menu_collapsed', collapsed.value ? '1' : '0') }

const menus = shallowRef([
  { type: 'item', path: '/overview', label: '总览', icon: Odometer },
  { type: 'group', label: '资产管理', icon: Coin, children: [
    { path: '/domains', label: '域名', icon: Connection },
    { path: '/dns-records', label: 'DNS 记录', icon: List },
    { path: '/hosts', label: '主机', icon: Monitor },
    { path: '/certs', label: '证书', icon: Lock },
    { path: '/cert-inspect', label: '到期巡检', icon: CircleCheck },
    { path: '/relations', label: '关系图谱', icon: Share },
  ] },
  { type: 'item', path: '/dashboard', label: '展示台', icon: DataAnalysis },
  { type: 'group', label: '系统管理', icon: Setting, children: [
    { path: '/basic', label: '基础配置', icon: Tools },
    { path: '/models', label: '模型管理', icon: Grid },
    { path: '/notify', label: '通知', icon: Bell },
    { path: '/cron', label: '定时任务', icon: Clock },
    { path: '/task-runs', label: '执行记录', icon: Tickets },
    { path: '/settings', label: '设置', icon: Setting },
  ] },
])

function onCmd(c) {
  if (c === 'logout') { auth.logout(); router.replace('/login') }
}
</script>

<style scoped>
.app { display: flex; height: 100vh; }
.sidebar { width: 200px; background: #1f2430; flex-shrink: 0; display: flex; flex-direction: column; transition: width .2s; }
.sidebar.collapsed { width: 64px; }
.brand { display: flex; align-items: center; gap: 8px; padding: 16px 18px; border-bottom: 1px solid rgba(255,255,255,.06); height: 56px; box-sizing: border-box; }
.brand-n { color: #fff; font-weight: 700; font-size: 15px; letter-spacing: 1px; }
.brand-t { margin-left: auto; font-size: 10px; color: rgba(255,255,255,.45); background: rgba(255,255,255,.08); padding: 1px 6px; border-radius: 3px; }
.nav { flex: 1; border-right: none; padding: 8px 0; overflow: hidden; }
.nav:not(.el-menu--collapse) { width: 200px; }
.nav :deep(.el-menu-item) { height: 42px; font-size: 13.5px; border-left: 2px solid transparent; }
.nav :deep(.el-menu-item:hover) { background: rgba(255,255,255,.05); color: #fff; }
.nav :deep(.el-menu-item.is-active) { background: rgba(59,130,246,.16); border-left-color: #3b82f6; color: #fff; }
.nav :deep(.el-sub-menu__title) { height: 42px; font-size: 13.5px; }
.nav :deep(.el-sub-menu__title:hover) { background: rgba(255,255,255,.05); color: #fff; }
.nav :deep(.el-sub-menu .el-menu-item) { min-width: auto; }
/* 箭头：收起朝右(›)、展开朝下(▾)，与运维平台一致。!important 覆盖 element 默认的 180° 旋转 */
.nav :deep(.el-sub-menu__icon-arrow) { transition: transform .2s !important; transform: rotate(-90deg) !important; }
.nav :deep(.el-sub-menu.is-opened .el-sub-menu__icon-arrow) { transform: rotate(0deg) !important; }
.foot { display: flex; align-items: center; gap: 8px; padding: 12px 20px; font-size: 12px; color: rgba(255,255,255,.5); border-top: 1px solid rgba(255,255,255,.06); cursor: pointer; }
.foot:hover { color: #fff; background: rgba(255,255,255,.05); }
.main { flex: 1; display: flex; flex-direction: column; overflow: hidden; }
.topbar { height: 52px; background: #fff; border-bottom: 1px solid #e7eaf0; display: flex; align-items: center; justify-content: space-between; padding: 0 20px; }
.crumb { font-size: 13px; color: #606266; }
.crumb b { color: #1f2430; }
.user { display: flex; align-items: center; gap: 6px; cursor: pointer; font-size: 13px; color: #606266; }
.content { flex: 1; overflow: auto; background: #f4f6fa; }
</style>
