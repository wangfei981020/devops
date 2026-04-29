<template>
  <div class="app">
    <aside class="sidebar">
      <div class="brand">
        <div class="brand-logo">D</div>
        <div class="brand-n">Deploy</div>
        <div class="brand-t">v{{ version }}</div>
      </div>

      <nav class="nav">
        <RouterLink v-if="auth.hasMenu('dashboard')" to="/dashboard" class="top-item" active-class="active">
          <el-icon class="ico"><Odometer /></el-icon>
          <span>发布概览</span>
        </RouterLink>

        <div class="group" v-if="showPublishGroup">
          <div :class="['group-title', {opened: grp.publish}]" @click="grp.publish = !grp.publish">
            <el-icon class="ico"><Upload /></el-icon>
            <span>发布管理</span>
            <el-icon class="chev"><ArrowRight /></el-icon>
          </div>
          <template v-if="grp.publish">
            <RouterLink v-if="auth.hasMenu('console')" to="/deploy" class="sub-item" active-class="active">部署控制台</RouterLink>
            <RouterLink v-if="auth.hasMenu('history')" to="/history" class="sub-item" active-class="active">发布历史</RouterLink>
          </template>
        </div>

        <div class="group" v-if="showConfigGroup">
          <div :class="['group-title', {opened: grp.config}]" @click="grp.config = !grp.config">
            <el-icon class="ico"><Setting /></el-icon>
            <span>配置管理</span>
            <el-icon class="chev"><ArrowRight /></el-icon>
          </div>
          <template v-if="grp.config">
            <RouterLink v-if="auth.hasMenu('projects')" to="/projects" class="sub-item" active-class="active">项目配置</RouterLink>
            <RouterLink v-if="auth.hasMenu('settings')" to="/settings" class="sub-item" active-class="active">系统设置</RouterLink>
          </template>
        </div>
      </nav>

      <div class="sidebar-foot">
        <span>{{ auth.user?.username || '-' }}</span>
        <span>v{{ version }}</span>
      </div>
    </aside>

    <main class="main">
      <div class="topbar">
        <div class="crumb">部署中心 / <b>{{ $route.meta.title }}</b></div>
        <div class="topbar-r">
          <span class="lbl">当前用户</span>
          <el-dropdown trigger="click" @command="onCommand">
            <div class="user-chip">
              <el-icon><User /></el-icon>
              <span>{{ auth.user?.username || '-' }}</span>
              <span v-if="auth.user?.auth_source === 'portal'" class="src-tag">SSO</span>
              <el-icon class="ch-arrow"><ArrowDown /></el-icon>
            </div>
            <template #dropdown>
              <el-dropdown-menu>
                <el-dropdown-item disabled>
                  <div style="display:flex;flex-direction:column;line-height:1.4">
                    <span style="font-weight:600;font-family:var(--mono)">{{ auth.user?.username }}</span>
                    <span style="font-size:11px;color:var(--text-3);">
                      <span v-if="auth.user?.display_name">{{ auth.user.display_name }} · </span>{{ auth.user?.role }}
                    </span>
                  </div>
                </el-dropdown-item>
                <el-dropdown-item divided command="refresh" v-if="auth.user?.auth_source === 'portal'">
                  <el-icon><RefreshRight /></el-icon> 刷新权限
                </el-dropdown-item>
                <el-dropdown-item command="logout" divided>
                  <el-icon><SwitchButton /></el-icon> 退出登录
                </el-dropdown-item>
              </el-dropdown-menu>
            </template>
          </el-dropdown>
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
import { reactive, computed, watch } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { Upload, Setting, ArrowRight, ArrowDown, User, RefreshRight, SwitchButton, Odometer } from '@element-plus/icons-vue'
import { useAuthStore } from '../stores/auth'

const auth = useAuthStore()
const router = useRouter()
const version = '114'

const grpRaw = JSON.parse(localStorage.getItem('deploy_sidebar_groups') || '{"publish":true,"config":true}')
const grp = reactive(grpRaw)
watch(grp, (v) => localStorage.setItem('deploy_sidebar_groups', JSON.stringify(v)), { deep: true })

const showPublishGroup = computed(() => auth.hasMenu('console') || auth.hasMenu('history'))
const showConfigGroup = computed(() => auth.hasMenu('projects') || auth.hasMenu('settings'))

async function onCommand(cmd) {
  if (cmd === 'logout') {
    await auth.logout()
    ElMessage.success('已登出')
    router.replace('/login')
  } else if (cmd === 'refresh') {
    await auth.refreshPermissions()
    ElMessage.success('权限已刷新')
  }
}
</script>

<style scoped>
.app { display: flex; height: 100vh; }

.sidebar { width: 220px; background: var(--sidebar-bg); flex-shrink: 0; display: flex; flex-direction: column; }
.brand { padding: 18px 20px; border-bottom: 1px solid rgba(255, 255, 255, 0.06); display: flex; align-items: center; gap: 10px; }
.brand-logo { width: 30px; height: 30px; background: var(--primary); border-radius: 6px; display: flex; align-items: center; justify-content: center; color: #fff; font-weight: 700; font-size: 14px; }
.brand-n { color: #fff; font-weight: 600; font-size: 14px; }
.brand-t { margin-left: auto; font-family: var(--mono); font-size: 10px; font-weight: 500; color: rgba(255, 255, 255, 0.4); padding: 1px 6px; background: rgba(255, 255, 255, 0.05); border-radius: 3px; }

.nav { padding: 12px 0; flex: 1; overflow-y: auto; }

.top-item {
  display: flex; align-items: center; padding: 10px 16px; gap: 10px;
  color: var(--sidebar-text); font-size: 13px; font-weight: 500;
  text-decoration: none; border-left: 2px solid transparent; transition: all .12s;
}
.top-item .ico { font-size: 15px; opacity: .85; }
.top-item:hover { background: var(--sidebar-hover); color: #fff; }
.top-item.active { background: var(--sidebar-active-bg); border-left-color: var(--primary); color: #fff; }

.group { margin-bottom: 4px; }
.group-title { display: flex; align-items: center; padding: 10px 16px; color: var(--sidebar-text); font-size: 13px; font-weight: 500; gap: 10px; cursor: pointer; }
.group-title:hover { background: var(--sidebar-hover); color: #fff; }
.group-title .ico { font-size: 15px; opacity: .8; }
.group-title .chev { margin-left: auto; opacity: .5; font-size: 12px; transition: transform .2s; }
.group-title.opened .chev { transform: rotate(90deg); opacity: .8; }

.sub-item { display: block; padding: 8px 16px 8px 42px; color: var(--sidebar-text-sub); font-size: 12.5px; text-decoration: none; border-left: 2px solid transparent; transition: all .12s; }
.sub-item:hover { color: #fff; background: var(--sidebar-hover); }
.sub-item.active { color: #fff; background: var(--sidebar-active-bg); border-left-color: var(--primary); font-weight: 500; }

.sidebar-foot { padding: 12px 16px; border-top: 1px solid rgba(255, 255, 255, 0.06); font-size: 11px; color: rgba(255, 255, 255, 0.4); font-family: var(--mono); display: flex; justify-content: space-between; }

.main { flex: 1; display: flex; flex-direction: column; overflow: hidden; }
.topbar { height: 52px; background: var(--bg-card); border-bottom: 1px solid var(--border); display: flex; align-items: center; justify-content: space-between; padding: 0 24px; flex-shrink: 0; }
.crumb { font-size: 13px; color: var(--text-2); }
.crumb b { color: var(--text); font-weight: 600; margin-left: 2px; }
.topbar-r { display: flex; align-items: center; gap: 14px; font-size: 12px; color: var(--text-3); }
.lbl { color: var(--text-3); }
.user-chip {
  display: flex; align-items: center; gap: 6px;
  padding: 4px 10px;
  background: var(--bg-hover);
  border: 1px solid transparent;
  border-radius: 99px;
  font-family: var(--mono);
  color: var(--text-2);
  cursor: pointer;
  transition: all .15s;
}
.user-chip:hover { border-color: var(--primary); color: var(--primary); background: var(--primary-bg); }
.user-chip .ch-arrow { font-size: 11px; opacity: .6; }
.user-chip .src-tag {
  font-family: var(--body); font-size: 10px; font-weight: 600;
  padding: 1px 5px; background: var(--primary); color: #fff; border-radius: 3px;
}
.ver-badge { padding: 2px 7px; background: var(--primary-bg); color: var(--primary); border-radius: 4px; font-family: var(--mono); font-size: 11px; font-weight: 600; }

.content { flex: 1; overflow: auto; padding: 20px 24px; }
</style>
