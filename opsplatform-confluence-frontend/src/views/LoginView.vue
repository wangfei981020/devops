<template>
  <div class="login-page">
    <div class="login-left">
      <div class="brand">
        <svg width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="#4C9AFF" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M2 3h6a4 4 0 0 1 4 4v14a3 3 0 0 0-3-3H2z"/><path d="M22 3h-6a4 4 0 0 0-4 4v14a3 3 0 0 1 3-3h7z"/></svg>
        <h1>Confluence Center</h1>
        <p>知识库管理中心</p>
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
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useAuthStore } from '@/stores/auth'

const router = useRouter()
const route = useRoute()
const authStore = useAuthStore()

const username = ref('')
const password = ref('')
const loading = ref(false)
const error = ref('')

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
  background: linear-gradient(135deg, #0747A6 0%, #4C9AFF 50%, #0F172A 100%);
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

@media (max-width: 768px) {
  .login-left { display: none; }
  .login-right { padding: 24px; }
  .login-form-wrapper { width: 100%; max-width: 360px; }
}
</style>
