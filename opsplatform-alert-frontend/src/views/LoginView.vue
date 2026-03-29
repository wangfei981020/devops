<template>
  <div class="login-page">
    <div class="login-card">
      <h1>Log Alert</h1>
      <p class="subtitle">告警管理平台</p>
      <form @submit.prevent="handleLogin">
        <div class="form-group">
          <label class="form-label">用户名</label>
          <input v-model="username" class="form-input" placeholder="请输入用户名" required />
        </div>
        <div class="form-group">
          <label class="form-label">密码</label>
          <input v-model="password" type="password" class="form-input" placeholder="请输入密码" required />
        </div>
        <p v-if="error" style="color: var(--danger); font-size: 14px; margin-bottom: 12px;">{{ error }}</p>
        <button type="submit" class="btn btn-primary" style="width: 100%; justify-content: center; padding: 10px;" :disabled="loading">
          {{ loading ? '登录中...' : '登录' }}
        </button>
      </form>
    </div>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '../stores/auth'

const router = useRouter()
const auth = useAuthStore()
const username = ref('')
const password = ref('')
const error = ref('')
const loading = ref(false)

async function handleLogin() {
  loading.value = true
  error.value = ''
  try {
    const res = await auth.login(username.value, password.value)
    if (res.code === 0) {
      router.push('/dashboard')
    } else {
      error.value = res.message || '登录失败'
    }
  } catch (e) {
    error.value = e.response?.data?.message || '网络错误'
  } finally {
    loading.value = false
  }
}
</script>
