<template>
  <div class="login-page">
    <div class="login-left">
      <div class="brand">
        <svg class="brand-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
          <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/>
          <path d="M9 12l2 2 4-4" stroke-linecap="round" stroke-linejoin="round"/>
        </svg>
        <h1>SSO Gateway</h1>
        <p>统一认证平台</p>
        <div class="features">
          <div class="feature-item"><span class="check">&#10003;</span> 单点登录</div>
          <div class="feature-item"><span class="check">&#10003;</span> 集中权限管理</div>
          <div class="feature-item"><span class="check">&#10003;</span> 多服务接入</div>
          <div class="feature-item"><span class="check">&#10003;</span> OIDC 标准协议</div>
        </div>
      </div>
      <div v-if="envLabel" :class="['env-corner', envClass]">{{ envLabel }}</div>
    </div>
    <div class="login-right">
      <div class="login-form-wrapper">
        <h2>登录</h2>
        <p class="subtitle">请输入您的账户信息</p>

        <div v-if="errorMsg" class="error-msg">{{ errorMsg }}</div>

        <form @submit.prevent="handleLogin">
          <div class="form-group">
            <label>用户名</label>
            <input v-model="username" type="text" placeholder="请输入用户名" required autofocus>
          </div>
          <div class="form-group">
            <label>密码</label>
            <input v-model="password" type="password" placeholder="请输入密码" required>
          </div>
          <button type="submit" class="login-btn" :disabled="submitting">
            {{ submitting ? '登录中...' : '登录' }}
          </button>
        </form>

        <div class="login-footer">SSO Gateway v2.0</div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useAuthStore } from '../stores/auth'
import api from '../api'

const router = useRouter()
const route = useRoute()
const authStore = useAuthStore()

const username = ref('')
const password = ref('')
const errorMsg = ref('')
const submitting = ref(false)
const envLabel = ref('')

const envClass = computed(() => {
  const label = envLabel.value
  if (label.includes('开发') || label.includes('dev')) return 'env-dev'
  if (label.includes('测试') || label.includes('test')) return 'env-test'
  if (label.includes('生产') || label.includes('prod')) return 'env-prod'
  return 'env-default'
})

async function loadEnv() {
  try {
    const res = await api.get('/api/public/env')
    envLabel.value = res.data.data?.env_label || ''
  } catch (e) {}
}

async function handleLogin() {
  if (submitting.value) return
  errorMsg.value = ''
  submitting.value = true

  try {
    await authStore.login(username.value, password.value)

    const clientId = route.query.client_id
    const redirectUri = route.query.redirect_uri
    const state = route.query.state || ''
    const scope = route.query.scope || 'openid profile email'

    if (clientId && redirectUri) {
      const params = new URLSearchParams({
        client_id: clientId,
        redirect_uri: redirectUri,
        response_type: 'code',
        state: state,
        scope: scope
      })
      window.location.href = '/authorize?' + params.toString()
    } else {
      const redirect = route.query.redirect || '/portal'
      router.push(redirect)
    }
  } catch (err) {
    errorMsg.value = err.response?.data?.message || '登录失败'
  } finally {
    submitting.value = false
  }
}

onMounted(() => { loadEnv() })
</script>

<style scoped>
.login-page {
  display: flex;
  min-height: 100vh;
}

.login-left {
  flex: 1;
  background: linear-gradient(135deg, #667eea 0%, #764ba2 50%, #667eea 100%);
  background-size: 200% 200%;
  animation: gradientShift 8s ease infinite;
  display: flex;
  align-items: center;
  justify-content: center;
  color: white;
  padding: 40px;
  position: relative;
  overflow: hidden;
}

.login-left::before {
  content: '';
  position: absolute;
  inset: 0;
  background: radial-gradient(circle at 30% 40%, rgba(255,255,255,0.08) 0%, transparent 50%),
              radial-gradient(circle at 70% 80%, rgba(255,255,255,0.05) 0%, transparent 40%);
}

@keyframes gradientShift {
  0% { background-position: 0% 50%; }
  50% { background-position: 100% 50%; }
  100% { background-position: 0% 50%; }
}

.brand {
  text-align: center;
  position: relative;
  z-index: 1;
}

.brand-icon {
  width: 72px;
  height: 72px;
  margin-bottom: 20px;
  filter: drop-shadow(0 4px 12px rgba(0,0,0,0.2));
}

.brand h1 {
  font-size: 34px;
  margin-bottom: 8px;
  font-weight: 700;
  letter-spacing: -0.5px;
}

.brand p {
  font-size: 16px;
  opacity: 0.8;
  margin-bottom: 40px;
}

.features { text-align: left; display: inline-block; }
.feature-item { padding: 8px 0; font-size: 15px; opacity: 0.9; display: flex; align-items: center; gap: 10px; }
.check { width: 20px; height: 20px; background: rgba(255,255,255,0.2); border-radius: 50%; display: inline-flex; align-items: center; justify-content: center; font-size: 11px; }

.env-corner {
  position: absolute;
  top: 20px;
  right: 20px;
  padding: 4px 14px;
  border-radius: 4px;
  font-size: 12px;
  font-weight: 600;
  z-index: 2;
}
.env-dev { background: rgba(255,255,255,0.2); color: #e0e7ff; }
.env-test { background: rgba(245, 158, 11, 0.3); color: #fef3c7; }
.env-prod { background: rgba(239, 68, 68, 0.3); color: #fecaca; }
.env-default { background: rgba(255,255,255,0.15); color: #e5e7eb; }

.login-right {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  background: #0f0f23;
  padding: 40px;
}

.login-form-wrapper { width: 100%; max-width: 380px; }

.login-form-wrapper h2 { color: #fff; font-size: 26px; margin-bottom: 6px; font-weight: 700; }
.subtitle { color: #666; margin-bottom: 28px; font-size: 14px; }

.error-msg {
  background: rgba(220, 38, 38, 0.1);
  color: #f87171;
  padding: 12px 16px;
  border-radius: 8px;
  border: 1px solid rgba(220, 38, 38, 0.2);
  margin-bottom: 20px;
  font-size: 13px;
}

.form-group { margin-bottom: 20px; }
.form-group label { display: block; color: #888; margin-bottom: 6px; font-size: 13px; font-weight: 500; }

.form-group input {
  width: 100%;
  padding: 12px 16px;
  background: rgba(22, 33, 62, 0.6);
  border: 1px solid rgba(255, 255, 255, 0.08);
  border-radius: 8px;
  color: #fff;
  font-size: 15px;
  outline: none;
  transition: border-color 0.2s, box-shadow 0.2s;
  box-sizing: border-box;
}

.form-group input:focus {
  border-color: #667eea;
  box-shadow: 0 0 0 3px rgba(102, 126, 234, 0.12);
}

.form-group input::placeholder { color: #444; }

.login-btn {
  width: 100%;
  padding: 13px;
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
  color: white;
  border: none;
  border-radius: 8px;
  font-size: 15px;
  font-weight: 600;
  cursor: pointer;
  transition: transform 0.2s, box-shadow 0.2s;
}

.login-btn:hover:not(:disabled) {
  transform: translateY(-2px);
  box-shadow: 0 8px 24px rgba(102, 126, 234, 0.35);
}

.login-btn:disabled { opacity: 0.6; cursor: not-allowed; }

.login-footer { text-align: center; margin-top: 40px; color: #333; font-size: 12px; }

@media (max-width: 768px) {
  .login-page { flex-direction: column; }
  .login-left { display: none; }
}
</style>
