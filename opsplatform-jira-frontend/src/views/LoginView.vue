<template>
  <div class="login-page">
    <div class="login-left">
      <div class="brand">
        <svg width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="#3B82F6" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M4 15s1-1 4-1 5 2 8 2 4-1 4-1V3s-1 1-4 1-5-2-8-2-4 1-4 1z"/><line x1="4" x2="4" y1="22" y2="15"/></svg>
        <h1>Jira Center</h1>
        <p>工单管理中心</p>
      </div>
    </div>

    <div class="login-right">
      <div class="login-form-wrapper">
        <h2>登录</h2>

        <div v-if="error" class="error-msg">{{ error }}</div>

        <form @submit.prevent="handleLogin" class="login-form">
          <div class="field">
            <label>用户名</label>
            <input class="input" v-model="username" placeholder="请输入用户名" autofocus />
          </div>
          <div class="field">
            <label>密码</label>
            <input class="input" type="password" v-model="password" placeholder="请输入密码" />
          </div>
          <button class="btn btn-primary login-btn" type="submit" :disabled="loading">
            {{ loading ? '登录中...' : '登录' }}
          </button>
        </form>

        <div v-if="oidcEnabled" class="sso-section">
          <div class="divider"><span>或</span></div>
          <button class="btn sso-btn" @click="loginWithOIDC">
            {{ oidcProviderName || 'SSO' }} 登录
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import api from '@/api'

const router = useRouter()
const route = useRoute()
const authStore = useAuthStore()

const username = ref('')
const password = ref('')
const loading = ref(false)
const error = ref('')
const oidcEnabled = ref(false)
const oidcProviderName = ref('')

onMounted(async () => {
  // 检查 URL 错误参数
  if (route.query.error) {
    const errorMap = {
      invalid_state: 'SSO 会话已过期，请重试',
      token_exchange_failed: 'SSO 认证失败',
      userinfo_failed: '获取用户信息失败',
      user_error: '用户处理失败',
      user_disabled: '账户已被禁用',
    }
    error.value = errorMap[route.query.error] || 'SSO 登录失败: ' + route.query.error
  }

  // 获取 OIDC 配置
  try {
    const res = await api.get('/api/oidc/config')
    oidcEnabled.value = res.data?.enabled || false
    oidcProviderName.value = res.data?.provider_name || ''
  } catch (e) { /* ignore */ }
})

async function handleLogin() {
  if (!username.value || !password.value) {
    error.value = '请输入用户名和密码'
    return
  }
  loading.value = true
  error.value = ''
  try {
    await authStore.login(username.value, password.value)
    const redirect = route.query.redirect || '/dashboard'
    router.push(redirect)
  } catch (e) {
    error.value = e.response?.data?.error || '登录失败'
  } finally {
    loading.value = false
  }
}

function loginWithOIDC() {
  window.location.href = '/api/oidc/login'
}
</script>

<style scoped>
.login-page {
  display: flex;
  height: 100vh;
  background: var(--bg-primary);
}

.login-left {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  background: linear-gradient(135deg, #1E40AF 0%, #3B82F6 50%, #0F172A 100%);
}

.brand {
  text-align: center;
  color: #fff;
}
.brand h1 {
  font-size: 32px;
  margin-top: 16px;
  font-weight: 700;
}
.brand p {
  margin-top: 8px;
  font-size: 16px;
  opacity: 0.8;
}

.login-right {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
}

.login-form-wrapper {
  width: 360px;
}
.login-form-wrapper h2 {
  font-size: 24px;
  margin-bottom: 24px;
  color: var(--text-primary);
}

.error-msg {
  padding: 10px 14px;
  background: rgba(239,68,68,0.1);
  border: 1px solid rgba(239,68,68,0.3);
  border-radius: var(--radius);
  color: var(--danger);
  font-size: 14px;
  margin-bottom: 16px;
}

.login-form { display: flex; flex-direction: column; gap: 16px; }

.field label {
  display: block;
  font-size: 13px;
  color: var(--text-secondary);
  margin-bottom: 6px;
  font-weight: 500;
}

.login-btn {
  width: 100%;
  padding: 10px;
  font-size: 15px;
  margin-top: 4px;
}

.sso-section { margin-top: 20px; }

.divider {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 16px;
  color: var(--text-muted);
  font-size: 13px;
}
.divider::before, .divider::after {
  content: '';
  flex: 1;
  height: 1px;
  background: var(--border);
}

.sso-btn {
  width: 100%;
  padding: 10px;
  justify-content: center;
  border-color: var(--primary-light);
  color: var(--primary-light);
}
.sso-btn:hover { background: rgba(59,130,246,0.1); }

@media (max-width: 768px) {
  .login-left { display: none; }
  .login-right { padding: 24px; }
  .login-form-wrapper { width: 100%; max-width: 360px; }
}
</style>
