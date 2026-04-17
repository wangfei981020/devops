<template>
  <div class="login-bg">
    <div class="login-card">
      <div class="brand">
        <div class="logo-icon">
          <el-icon :size="28"><Position /></el-icon>
        </div>
        <h2>网络探测平台</h2>
        <div class="subtitle">opsplatform-probe · 连通性监测</div>
      </div>
      <el-form :model="form" label-width="80px" @submit.prevent="onLogin">
        <el-form-item label="用户名">
          <el-input v-model="form.username" />
        </el-form-item>
        <el-form-item label="密码">
          <el-input v-model="form.password" type="password" show-password />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" :loading="loading" @click="onLogin" style="width:100%">登录</el-button>
        </el-form-item>
      </el-form>
      <div style="text-align:center;color:#aaa;font-size:12px">
        支持运维平台 SSO 统一登录
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import api from '../api/client'
import { ElMessage } from 'element-plus'

const router = useRouter()
const route = useRoute()
const form = ref({ username: '', password: '' })
const loading = ref(false)

async function onLogin() {
  loading.value = true
  try {
    const r = await api.post('/login', form.value)
    // The real JWT is in an httpOnly cookie set by the backend.
    // We only set a non-sensitive marker so the SPA router knows the user is logged in.
    if (r.data?.user) {
      localStorage.setItem('probe_logged_in', '1')
      router.push('/dashboard')
    }
  } catch (e) {} finally { loading.value = false }
}

onMounted(async () => {
  // Portal SSO: ?token=xxx from opsplatform redirect
  const ssoToken = route.query.token
  if (ssoToken) {
    try {
      const r = await api.post('/portal-auth', { token: ssoToken })
      if (r.data?.user) {
        localStorage.setItem('probe_logged_in', '1')
        router.push('/dashboard')
      }
    } catch (e) { ElMessage.error('SSO 登录失败') }
  }
})
</script>

<style scoped>
.login-bg {
  height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  background:
    radial-gradient(circle at 20% 30%, rgba(64,158,255,0.15), transparent 50%),
    radial-gradient(circle at 80% 70%, rgba(103,194,58,0.12), transparent 50%),
    linear-gradient(135deg, #0f1e37 0%, #1a2942 100%);
}
.login-card {
  width: 400px;
  background: #fff;
  border-radius: 8px;
  padding: 40px 36px 32px;
  box-shadow: 0 20px 60px rgba(0,0,0,0.35);
}
.brand { text-align: center; margin-bottom: 28px; }
.logo-icon {
  width: 56px; height: 56px;
  border-radius: 14px;
  background: linear-gradient(135deg, #409eff, #337ecc);
  color: #fff;
  display: inline-flex;
  align-items: center; justify-content: center;
  margin-bottom: 12px;
  box-shadow: 0 8px 20px rgba(64,158,255,0.35);
}
.brand h2 { margin: 4px 0 4px; color: #1f2d3d; font-size: 22px; font-weight: 600; }
.subtitle { color: #909399; font-size: 13px; }
</style>
