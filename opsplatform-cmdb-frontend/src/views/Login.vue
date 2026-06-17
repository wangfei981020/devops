<template>
  <div class="login-wrap">
    <div class="login-box">
      <div class="logo"><el-icon :size="30" color="#3b82f6"><Files /></el-icon><span>CMDB 配置管理</span></div>
      <p class="sub">域名 · 证书 · 自动续期 · 到期提醒</p>
      <el-form @submit.prevent="doLogin">
        <el-form-item>
          <el-input v-model="username" placeholder="用户名" size="large" :prefix-icon="User" />
        </el-form-item>
        <el-form-item>
          <el-input v-model="password" type="password" placeholder="密码" size="large" :prefix-icon="Lock" show-password @keyup.enter="doLogin" />
        </el-form-item>
        <el-button type="primary" size="large" style="width:100%" :loading="loading" @click="doLogin">登录</el-button>
      </el-form>
    </div>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { User, Lock, Files } from '@element-plus/icons-vue'
import { useAuthStore } from '../stores/auth'

const router = useRouter()
const auth = useAuthStore()
const username = ref('admin')
const password = ref('')
const loading = ref(false)

async function doLogin() {
  if (!username.value || !password.value) { ElMessage.warning('请输入用户名和密码'); return }
  loading.value = true
  try {
    await auth.login(username.value, password.value)
    router.replace('/')
  } catch (e) {
    ElMessage.error(e.response?.data?.error || '登录失败')
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.login-wrap { height: 100vh; display: flex; align-items: center; justify-content: center; background: linear-gradient(135deg, #1f2430, #2d3748); }
.login-box { width: 360px; background: #fff; border-radius: 12px; padding: 36px 32px; box-shadow: 0 12px 40px rgba(0,0,0,.25); }
.logo { display: flex; align-items: center; gap: 10px; font-size: 20px; font-weight: 700; color: #1f2430; }
.sub { color: #909399; font-size: 12.5px; margin: 6px 0 24px; }
</style>
