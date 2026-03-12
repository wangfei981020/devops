<script setup>
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import axios from 'axios'

const router = useRouter()
const authStore = useAuthStore()
const username = ref('')
const password = ref('')
const error = ref('')
const loading = ref(false)
const localLoginEnabled = ref(false)
const settingsLoaded = ref(false)

onMounted(async () => {
  try {
    const res = await axios.get('/api/auth/login-settings')
    localLoginEnabled.value = res.data?.data?.local_login_enabled || false
  } catch (e) {
    // If settings endpoint fails, just show SSO login
  }
  settingsLoaded.value = true
})

async function handleLocalLogin() {
  if (!username.value || !password.value) {
    error.value = '请输入用户名和密码'
    return
  }
  loading.value = true
  error.value = ''
  try {
    const res = await axios.post('/api/auth/login', {
      username: username.value,
      password: password.value
    })
    const { token, user } = res.data.data
    authStore.setAuth(token, user)
    router.replace('/dashboard')
  } catch (e) {
    error.value = e.response?.data?.message || '登录失败'
  } finally {
    loading.value = false
  }
}

function handleSSOLogin() {
  const ssoUrl = 'http://localhost:30900'
  const callbackUrl = window.location.origin + '/auth/callback'
  window.location.href = `${ssoUrl}/authorize?client_id=permission-center&redirect_uri=${encodeURIComponent(callbackUrl)}&response_type=code&scope=openid profile`
}
</script>

<template>
  <div class="login-page">
    <div class="login-card" v-if="settingsLoaded">
      <div class="login-header">
        <div class="logo">🛡️</div>
        <h1>权限管理中心</h1>
        <p class="subtitle">Permission Center</p>
      </div>

      <!-- Local Login Form -->
      <div v-if="localLoginEnabled" class="login-form">
        <div class="form-group">
          <input v-model="username" type="text" placeholder="用户名" @keyup.enter="handleLocalLogin" autofocus />
        </div>
        <div class="form-group">
          <input v-model="password" type="password" placeholder="密码" @keyup.enter="handleLocalLogin" />
        </div>
        <div class="error-msg" v-if="error">{{ error }}</div>
        <button class="btn-login" @click="handleLocalLogin" :disabled="loading">
          {{ loading ? '登录中...' : '登 录' }}
        </button>
        <div class="divider"><span>或</span></div>
      </div>

      <!-- SSO Login Button -->
      <button class="btn-sso" @click="handleSSOLogin">
        🔑 通过 SSO 统一认证登录
      </button>
    </div>

    <!-- Loading -->
    <div class="loading-box" v-else>
      <div class="spinner"></div>
      <p>加载中...</p>
    </div>
  </div>
</template>

<style scoped>
.login-page {
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  background: #0f172a;
  color: #e2e8f0;
}
.login-card {
  background: #1e293b;
  border: 1px solid rgba(255,255,255,0.08);
  border-radius: 16px;
  padding: 40px;
  width: 400px;
}
.login-header { text-align: center; margin-bottom: 32px; }
.logo { font-size: 48px; margin-bottom: 12px; }
.login-header h1 { font-size: 22px; font-weight: 600; margin: 0; color: #f1f5f9; }
.subtitle { font-size: 13px; color: #64748b; margin-top: 4px; }

.login-form { margin-bottom: 0; }
.form-group { margin-bottom: 16px; }
.form-group input {
  width: 100%;
  padding: 12px 16px;
  background: #0f172a;
  border: 1px solid rgba(255,255,255,0.1);
  border-radius: 8px;
  color: #e2e8f0;
  font-size: 14px;
  outline: none;
  box-sizing: border-box;
  transition: border-color 0.2s;
}
.form-group input:focus { border-color: #3b82f6; }
.form-group input::placeholder { color: #475569; }

.error-msg {
  color: #f87171;
  font-size: 13px;
  margin-bottom: 12px;
  text-align: center;
}

.btn-login {
  width: 100%;
  padding: 12px;
  background: #3b82f6;
  color: white;
  border: none;
  border-radius: 8px;
  font-size: 15px;
  font-weight: 500;
  cursor: pointer;
  transition: background 0.2s;
}
.btn-login:hover { background: #2563eb; }
.btn-login:disabled { opacity: 0.6; cursor: not-allowed; }

.divider {
  display: flex;
  align-items: center;
  margin: 24px 0;
  color: #475569;
  font-size: 13px;
}
.divider::before, .divider::after {
  content: '';
  flex: 1;
  height: 1px;
  background: rgba(255,255,255,0.08);
}
.divider span { padding: 0 12px; }

.btn-sso {
  width: 100%;
  padding: 12px;
  background: transparent;
  color: #94a3b8;
  border: 1px solid rgba(255,255,255,0.1);
  border-radius: 8px;
  font-size: 14px;
  cursor: pointer;
  transition: all 0.2s;
}
.btn-sso:hover {
  background: rgba(255,255,255,0.04);
  border-color: rgba(255,255,255,0.2);
  color: #e2e8f0;
}

.loading-box { text-align: center; }
.spinner {
  width: 40px; height: 40px; margin: 0 auto 16px;
  border: 3px solid rgba(255,255,255,0.1);
  border-top-color: #3b82f6;
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
}
@keyframes spin { to { transform: rotate(360deg); } }
</style>
