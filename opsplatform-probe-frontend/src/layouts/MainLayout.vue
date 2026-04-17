<template>
  <el-container style="height: 100vh">
    <el-aside width="220px" class="sidebar">
      <div class="logo">探测平台</div>
      <el-menu
        :default-active="$route.path"
        router
        background-color="#001529"
        text-color="#cfd8dc"
        active-text-color="#fff">
        <el-menu-item index="/dashboard"><el-icon><DataBoard /></el-icon><span>概览</span></el-menu-item>
        <el-menu-item index="/agents"><el-icon><Cpu /></el-icon><span>Agent 管理</span></el-menu-item>
        <el-menu-item index="/agent-groups"><el-icon><Collection /></el-icon><span>Agent 分组</span></el-menu-item>
        <el-menu-item index="/targets"><el-icon><Aim /></el-icon><span>探测目标</span></el-menu-item>
        <el-menu-item index="/probe"><el-icon><Position /></el-icon><span>手动探测</span></el-menu-item>
        <el-menu-item index="/results"><el-icon><Document /></el-icon><span>探测结果</span></el-menu-item>
        <el-menu-item index="/versions"><el-icon><Box /></el-icon><span>版本管理</span></el-menu-item>
        <el-menu-item index="/upgrades"><el-icon><Upload /></el-icon><span>升级任务</span></el-menu-item>
        <el-menu-item index="/audit"><el-icon><List /></el-icon><span>审计日志</span></el-menu-item>
        <el-menu-item index="/users"><el-icon><User /></el-icon><span>用户管理</span></el-menu-item>
      </el-menu>
    </el-aside>
    <el-container>
      <el-header class="header">
        <div></div>
        <div>
          <el-dropdown>
            <span style="cursor:pointer">{{ user.username || 'guest' }} <el-icon><ArrowDown /></el-icon></span>
            <template #dropdown>
              <el-dropdown-menu>
                <el-dropdown-item @click="logout">退出登录</el-dropdown-item>
              </el-dropdown-menu>
            </template>
          </el-dropdown>
        </div>
      </el-header>
      <el-main>
        <router-view />
      </el-main>
    </el-container>
  </el-container>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import api from '../api/client'

const router = useRouter()
const user = ref({})

onMounted(async () => {
  try {
    const r = await api.get('/users/me')
    user.value = r.data || {}
  } catch (e) {}
})

async function logout() {
  try { await api.post('/logout') } catch (e) {}
  localStorage.removeItem('probe_logged_in')
  router.push('/login')
}
</script>

<style scoped>
.sidebar {
  background: linear-gradient(180deg, #0f1e37 0%, #1a2942 100%);
  color: #fff;
  box-shadow: 2px 0 8px rgba(0,0,0,0.08);
}
.logo {
  color: #fff;
  font-size: 17px;
  font-weight: 600;
  padding: 20px 16px;
  text-align: center;
  border-bottom: 1px solid rgba(255,255,255,0.08);
  letter-spacing: 1px;
}
.header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  background: #fff;
  border-bottom: 1px solid #ebeef5;
  padding: 0 24px;
  box-shadow: 0 1px 4px rgba(0,0,0,0.02);
}
.el-main { background: #f5f7fa; padding: 20px 24px; }
:deep(.el-menu) { border-right: none; background: transparent !important; }
:deep(.el-menu-item) { color: #b0bec5 !important; }
:deep(.el-menu-item:hover) { background: rgba(255,255,255,0.05) !important; color: #fff !important; }
:deep(.el-menu-item.is-active) {
  background: linear-gradient(90deg, rgba(64,158,255,0.2), transparent) !important;
  color: #409eff !important;
  border-left: 3px solid #409eff;
}
:deep(.el-menu-item .el-icon) { color: inherit; }
</style>
