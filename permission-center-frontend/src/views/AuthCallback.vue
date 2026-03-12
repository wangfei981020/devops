<script setup>
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import axios from 'axios'

const router = useRouter()
const authStore = useAuthStore()
const error = ref('')

onMounted(async () => {
  const params = new URLSearchParams(window.location.search)
  const code = params.get('code')

  if (!code) {
    error.value = '缺少授权码'
    return
  }

  try {
    // Exchange code for token with SSO
    const ssoUrl = 'http://localhost:30900'
    const callbackUrl = window.location.origin + '/auth/callback'
    const tokenRes = await axios.post(`${ssoUrl}/token`, new URLSearchParams({
      grant_type: 'authorization_code',
      code: code,
      redirect_uri: callbackUrl,
      client_id: 'permission-center',
      client_secret: 'permission-center-secret'
    }), { headers: { 'Content-Type': 'application/x-www-form-urlencoded' } })

    const { access_token, id_token } = tokenRes.data

    // Get user info from SSO
    const userRes = await axios.get(`${ssoUrl}/userinfo`, {
      headers: { Authorization: `Bearer ${access_token}` }
    })

    authStore.setAuth(access_token, {
      id: userRes.data.sub,
      username: userRes.data.preferred_username || userRes.data.name,
      display_name: userRes.data.name,
      email: userRes.data.email
    })

    router.replace('/dashboard')
  } catch (e) {
    console.error('Auth callback failed:', e)
    error.value = '认证失败: ' + (e.response?.data?.error_description || e.message)
  }
})
</script>

<template>
  <div class="callback-page">
    <div class="loading-box" v-if="!error">
      <div class="spinner"></div>
      <p>正在完成登录...</p>
    </div>
    <div class="error-box" v-else>
      <p>{{ error }}</p>
      <button @click="$router.push('/login')">重新登录</button>
    </div>
  </div>
</template>

<style scoped>
.callback-page {
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  background: #0f172a;
  color: #e2e8f0;
}
.loading-box, .error-box { text-align: center; }
.spinner {
  width: 40px; height: 40px; margin: 0 auto 16px;
  border: 3px solid rgba(255,255,255,0.1);
  border-top-color: #3b82f6;
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
}
@keyframes spin { to { transform: rotate(360deg); } }
.error-box button {
  margin-top: 16px; padding: 8px 24px;
  background: #3b82f6; color: white; border: none;
  border-radius: 6px; cursor: pointer;
}
</style>
