<script setup>
import { ref, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useAuthStore, useAppStore } from '@/stores'
import api from '@/api'

const router = useRouter()
const route = useRoute()
const authStore = useAuthStore()
const appStore = useAppStore()

const form = ref({
  username: '',
  password: '',
  rememberMe: false
})
const loading = ref(false)
const error = ref('')
const showPassword = ref(false)

const step = ref('login')
const mfaCode = ref('')
const mfaUserId = ref('')
const mfaUsername = ref('')
const mfaSecret = ref('')
const mfaQrCode = ref('')

const oidcConfig = ref({ enabled: false, provider_name: '' })
const oidcLoading = ref(false)

onMounted(async () => {
  const rememberUser = localStorage.getItem('rememberUser')
  if (rememberUser) {
    form.value.username = rememberUser
    form.value.rememberMe = true
  }

  const oidcError = route.query.error
  if (oidcError) {
    const errorMessages = {
      'oidc_access_denied': 'OIDC登录被拒绝',
      'invalid_state': '登录状态无效，请重试',
      'no_code': '未获取到授权码',
      'config_error': 'OIDC配置错误',
      'token_exchange_failed': '获取令牌失败',
      'userinfo_failed': '获取用户信息失败',
      'user_error': '用户处理失败',
      'user_disabled': '用户已被禁用',
      'token_error': '登录令牌生成失败'
    }
    error.value = errorMessages[oidcError] || 'OIDC登录失败: ' + oidcError
  }

  try {
    const resp = await api.get('/api/oidc/config')
    oidcConfig.value = resp.data
  } catch (e) {
    console.log('OIDC未启用')
  }
})

async function handleLogin() {
  if (!form.value.username || !form.value.password) {
    error.value = '请输入用户名和密码'
    return
  }
  
  loading.value = true
  error.value = ''
  
  try {
    const result = await authStore.login(form.value.username, form.value.password)
    
    if (result.requireMFA) {
      step.value = 'mfa-verify'
      mfaUserId.value = result.userId
      mfaUsername.value = form.value.username
      mfaCode.value = ''
    } else if (result.needBinding) {
      mfaUserId.value = result.userId
      mfaUsername.value = form.value.username
      await startMFASetup()
    } else if (result.token) {
      if (form.value.rememberMe) {
        localStorage.setItem('rememberUser', form.value.username)
      }
      router.push('/welcome')
    } else {
      error.value = '登录失败，请检查用户名和密码'
    }
  } catch (e) {
    error.value = e.response?.data || e.message || '登录失败，请稍后重试'
  } finally {
    loading.value = false
  }
}

async function startMFASetup() {
  try {
    const data = await authStore.setupMFA(mfaUserId.value)
    mfaSecret.value = data.secret
    mfaQrCode.value = data.qr_code
    step.value = 'mfa-bind'
    mfaCode.value = ''
  } catch (e) {
    error.value = 'MFA 设置失败: ' + (e.response?.data || e.message)
  }
}

async function verifyMFA() {
  if (!mfaCode.value || mfaCode.value.length !== 6) {
    error.value = '请输入6位验证码'
    return
  }
  loading.value = true
  error.value = ''
  try {
    await authStore.verifyMFA(mfaUserId.value, mfaCode.value)
    if (form.value.rememberMe) {
      localStorage.setItem('rememberUser', form.value.username)
    }
    router.push('/welcome')
  } catch (e) {
    error.value = e.response?.data || '验证码错误'
  } finally {
    loading.value = false
  }
}

async function bindMFA() {
  if (!mfaCode.value || mfaCode.value.length !== 6) {
    error.value = '请输入6位验证码'
    return
  }
  loading.value = true
  error.value = ''
  try {
    await authStore.bindMFA(mfaUserId.value, mfaCode.value, mfaSecret.value)
    if (form.value.rememberMe) {
      localStorage.setItem('rememberUser', form.value.username)
    }
    router.push('/welcome')
  } catch (e) {
    error.value = e.response?.data || '绑定失败'
  } finally {
    loading.value = false
  }
}

function cancelMFA() {
  step.value = 'login'
  mfaCode.value = ''
  mfaUserId.value = ''
  mfaSecret.value = ''
  mfaQrCode.value = ''
  error.value = ''
  authStore.cancelMFA()
}

function copySecret() {
  if (mfaSecret.value) {
    navigator.clipboard.writeText(mfaSecret.value)
    alert('密钥已复制到剪贴板')
  }
}

function togglePassword() {
  showPassword.value = !showPassword.value
}

function loginWithOIDC() {
  oidcLoading.value = true
  window.location.href = '/api/oidc/login'
}
</script>

<template>
  <div class="login-page">
    <!-- 左侧品牌区 -->
    <div class="brand-section">
      <div class="brand-content">
        <div class="logo-container">
          <div class="logo-text">
            <div class="logo-icon">⚡</div>
            <span class="logo-title">运维平台</span>
          </div>
        </div>

        <p class="brand-slogan">
          一站式智能运维解决方案<br>
          让运维更简单、更高效、更智能
        </p>

        <div class="features">
          <div class="feature-item">
            <div class="feature-icon">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <rect x="2" y="3" width="20" height="14" rx="2" ry="2"></rect>
                <line x1="8" y1="21" x2="16" y2="21"></line>
                <line x1="12" y1="17" x2="12" y2="21"></line>
              </svg>
            </div>
            <span>统一资源管理，全面掌控基础设施</span>
          </div>
          <div class="feature-item">
            <div class="feature-icon">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <path d="M21.21 15.89A10 10 0 1 1 8 2.83"></path>
                <path d="M22 12A10 10 0 0 0 12 2v10z"></path>
              </svg>
            </div>
            <span>实时监控告警，快速发现解决问题</span>
          </div>
          <div class="feature-item">
            <div class="feature-icon">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <path d="M12 2a2 2 0 0 1 2 2c0 .74-.4 1.39-1 1.73V7h1a7 7 0 0 1 7 7h1a1 1 0 0 1 1 1v3a1 1 0 0 1-1 1h-1v1a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-1H2a1 1 0 0 1-1-1v-3a1 1 0 0 1 1-1h1a7 7 0 0 1 7-7h1V5.73c-.6-.34-1-.99-1-1.73a2 2 0 0 1 2-2M7.5 13A2.5 2.5 0 0 0 5 15.5 2.5 2.5 0 0 0 7.5 18a2.5 2.5 0 0 0 2.5-2.5A2.5 2.5 0 0 0 7.5 13m9 0a2.5 2.5 0 0 0-2.5 2.5 2.5 2.5 0 0 0 2.5 2.5 2.5 2.5 0 0 0 2.5-2.5 2.5 2.5 0 0 0-2.5-2.5z"/>
              </svg>
            </div>
            <span>智能化运维，AI驱动故障预测</span>
          </div>
          <div class="feature-item">
            <div class="feature-icon">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <path d="M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.77-3.77a6 6 0 0 1-7.94 7.94l-6.91 6.91a2.12 2.12 0 0 1-3-3l6.91-6.91a6 6 0 0 1 7.94-7.94l-3.76 3.76z"></path>
              </svg>
            </div>
            <span>容器化管理，轻松驾驭K8S集群</span>
          </div>
        </div>
      </div>
    </div>

    <!-- 右侧登录区 -->
    <div class="login-section">
      <!-- 登录表单 -->
      <template v-if="step === 'login'">
        <div class="login-header">
          <h1 class="login-title">欢迎回来</h1>
          <p class="login-subtitle">请输入您的账号信息登录系统</p>
        </div>

        <form class="login-form" @submit.prevent="handleLogin">
          <div v-if="error" class="error-message">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <circle cx="12" cy="12" r="10"></circle>
              <line x1="12" y1="8" x2="12" y2="12"></line>
              <line x1="12" y1="16" x2="12.01" y2="16"></line>
            </svg>
            <span>{{ error }}</span>
          </div>

          <div class="form-group">
            <label class="form-label">用户名</label>
            <div class="form-input-wrapper">
              <span class="form-input-icon">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                  <path d="M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2"></path>
                  <circle cx="12" cy="7" r="4"></circle>
                </svg>
              </span>
              <input type="text" class="form-input" v-model="form.username" placeholder="请输入用户名" autocomplete="username" :disabled="loading">
            </div>
          </div>

          <div class="form-group">
            <label class="form-label">密码</label>
            <div class="form-input-wrapper">
              <span class="form-input-icon">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                  <rect x="3" y="11" width="18" height="11" rx="2" ry="2"></rect>
                  <path d="M7 11V7a5 5 0 0 1 10 0v4"></path>
                </svg>
              </span>
              <input :type="showPassword ? 'text' : 'password'" class="form-input" v-model="form.password" placeholder="请输入密码" autocomplete="current-password" :disabled="loading">
              <button type="button" class="password-toggle" @click="togglePassword">
                <svg v-if="!showPassword" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"></path><circle cx="12" cy="12" r="3"></circle></svg>
                <svg v-else viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M17.94 17.94A10.07 10.07 0 0 1 12 20c-7 0-11-8-11-8a18.45 18.45 0 0 1 5.06-5.94M9.9 4.24A9.12 9.12 0 0 1 12 4c7 0 11 8 11 8a18.5 18.5 0 0 1-2.16 3.19m-6.72-1.07a3 3 0 1 1-4.24-4.24"></path><line x1="1" y1="1" x2="23" y2="23"></line></svg>
              </button>
            </div>
          </div>

          <div class="form-options">
            <label class="remember-me"><input type="checkbox" v-model="form.rememberMe"><span>记住我</span></label>
            <a href="javascript:void(0)" class="forgot-password">忘记密码？</a>
          </div>

          <button type="submit" class="login-btn" :class="{ loading }" :disabled="loading">
            <div class="spinner" v-if="loading"></div>
            <span class="btn-text" v-else>登 录</span>
          </button>

          <!-- OIDC 单点登录 -->
          <template v-if="oidcConfig.enabled">
            <div class="divider">或</div>
            <button type="button" class="oidc-btn" @click="loginWithOIDC" :disabled="oidcLoading">
              <svg v-if="oidcLoading" class="spinner-icon" viewBox="0 0 24 24"><circle cx="12" cy="12" r="10" stroke="currentColor" stroke-width="3" fill="none" stroke-dasharray="30 70"/></svg>
              <svg v-else viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M15 3h4a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2h-4"/><polyline points="10 17 15 12 10 7"/><line x1="15" y1="12" x2="3" y2="12"/></svg>
              <span>{{ oidcConfig.provider_name || 'SSO' }} 登录</span>
            </button>
          </template>

          <div class="divider" v-if="!oidcConfig.enabled">其他登录方式</div>

          <div class="other-login" v-if="!oidcConfig.enabled">
            <button type="button" class="social-btn" title="企业微信"><svg viewBox="0 0 24 24" fill="currentColor"><path d="M19.5 3H4.5C3.12 3 2 4.12 2 5.5v13C2 19.88 3.12 21 4.5 21h15c1.38 0 2.5-1.12 2.5-2.5v-13C22 4.12 20.88 3 19.5 3zM8 14c-.83 0-1.5-.67-1.5-1.5S7.17 11 8 11s1.5.67 1.5 1.5S8.83 14 8 14zm4 0c-.83 0-1.5-.67-1.5-1.5S11.17 11 12 11s1.5.67 1.5 1.5S12.83 14 12 14zm4 0c-.83 0-1.5-.67-1.5-1.5S15.17 11 16 11s1.5.67 1.5 1.5S16.83 14 16 14z"/></svg></button>
            <button type="button" class="social-btn" title="钉钉"><svg viewBox="0 0 24 24" fill="currentColor"><path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm0 3c1.66 0 3 1.34 3 3s-1.34 3-3 3-3-1.34-3-3 1.34-3 3-3zm0 14.2c-2.5 0-4.71-1.28-6-3.22.03-1.99 4-3.08 6-3.08 1.99 0 5.97 1.09 6 3.08-1.29 1.94-3.5 3.22-6 3.22z"/></svg></button>
            <button type="button" class="social-btn" title="LDAP"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"></path></svg></button>
          </div>
        </form>
      </template>

      <!-- MFA 验证界面 -->
      <template v-else-if="step === 'mfa-verify'">
        <div class="login-header">
          <h1 class="login-title">多因素认证</h1>
          <p class="login-subtitle">请输入您认证器上的 6 位验证码</p>
        </div>

        <div class="mfa-section">
          <div class="mfa-user-info">
            <div class="mfa-avatar">{{ mfaUsername.charAt(0).toUpperCase() }}</div>
            <span>{{ mfaUsername }}</span>
          </div>

          <div v-if="error" class="error-message">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"></circle><line x1="12" y1="8" x2="12" y2="12"></line><line x1="12" y1="16" x2="12.01" y2="16"></line></svg>
            <span>{{ error }}</span>
          </div>

          <div class="mfa-code-input-wrap">
            <input type="text" v-model="mfaCode" class="mfa-code-input" maxlength="6" placeholder="000000" inputmode="numeric" @keyup.enter="verifyMFA">
          </div>

          <div class="mfa-actions">
            <button type="button" class="login-btn" :class="{ loading }" :disabled="loading || mfaCode.length !== 6" @click="verifyMFA">
              <div class="spinner" v-if="loading"></div>
              <span class="btn-text" v-else>验证并登录</span>
            </button>
            <button type="button" class="mfa-back-btn" @click="cancelMFA">← 返回登录</button>
          </div>
        </div>
      </template>

      <!-- MFA 绑定界面 -->
      <template v-else-if="step === 'mfa-bind'">
        <div class="bind-section">
          <div class="bind-header">
            <div class="bind-icon">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="3" y="11" width="18" height="11" rx="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/><circle cx="12" cy="16" r="1"/></svg>
            </div>
            <h2 class="bind-title">设置双因素认证</h2>
            <p class="bind-subtitle">使用认证器 App 扫描二维码，为账户添加安全保护</p>
          </div>

          <div class="steps-indicator">
            <div class="step done"><span class="step-dot"><svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3"><polyline points="20 6 9 17 4 12"/></svg></span><span class="step-label">密码验证</span></div>
            <div class="step-line done"></div>
            <div class="step active"><span class="step-dot">2</span><span class="step-label">扫码绑定</span></div>
            <div class="step-line"></div>
            <div class="step pending"><span class="step-dot">3</span><span class="step-label">完成</span></div>
          </div>

          <div class="bind-qr-container">
            <div class="bind-qr-wrapper">
              <img v-if="mfaQrCode" :src="mfaQrCode" alt="MFA QR Code" class="bind-qr-img">
              <div v-else class="bind-qr-placeholder">加载中...</div>
            </div>
            <div class="bind-secret-section">
              <div class="bind-secret-label">或手动输入密钥</div>
              <div class="bind-secret-key">
                <code>{{ mfaSecret }}</code>
                <button type="button" class="bind-copy-btn" @click="copySecret" title="复制密钥">
                  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>
                </button>
              </div>
            </div>
          </div>

          <div v-if="error" class="error-message">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"></circle><line x1="12" y1="8" x2="12" y2="12"></line><line x1="12" y1="16" x2="12.01" y2="16"></line></svg>
            <span>{{ error }}</span>
          </div>

          <div class="bind-code-section">
            <div class="bind-code-label">输入认证器上的 6 位验证码</div>
            <input type="text" v-model="mfaCode" class="mfa-code-input" maxlength="6" placeholder="000000" inputmode="numeric" @keyup.enter="bindMFA">
          </div>

          <button type="button" class="bind-submit-btn" :class="{ loading }" :disabled="loading || mfaCode.length !== 6" @click="bindMFA">
            <div class="spinner" v-if="loading"></div>
            <template v-else>
              <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M9 12l2 2 4-4"/><circle cx="12" cy="12" r="10"/></svg>
              <span>验证并完成绑定</span>
            </template>
          </button>

          <a class="bind-back-link" @click="cancelMFA">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M19 12H5M12 19l-7-7 7-7"/></svg>
            <span>返回登录</span>
          </a>
        </div>
      </template>

      <div class="login-footer">
        <p>© 2026 运维平台 · <a href="#">使用条款</a> · <a href="#">隐私政策</a></p>
      </div>
    </div>
  </div>
</template>

<style scoped>
.login-page {
  min-height: 100vh;
  display: flex;
}

/* 左侧品牌区 */
.brand-section {
  flex: 1;
  background: linear-gradient(135deg, #001529 0%, #002140 100%);
  display: flex;
  flex-direction: column;
  justify-content: center;
  align-items: center;
  padding: 60px;
  position: relative;
  overflow: hidden;
}

.brand-section::before {
  content: '';
  position: absolute;
  width: 600px;
  height: 600px;
  background: radial-gradient(circle, rgba(24,144,255,0.15) 0%, transparent 70%);
  top: -200px;
  right: -200px;
}

.brand-section::after {
  content: '';
  position: absolute;
  width: 400px;
  height: 400px;
  background: radial-gradient(circle, rgba(24,144,255,0.1) 0%, transparent 70%);
  bottom: -100px;
  left: -100px;
}

.brand-content {
  position: relative;
  z-index: 1;
  text-align: center;
  max-width: 480px;
}

.logo-container {
  margin-bottom: 40px;
}

.logo-text {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 16px;
}

.logo-icon {
  width: 56px;
  height: 56px;
  background: linear-gradient(135deg, #1890ff, #40a9ff);
  border-radius: 14px;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 28px;
  box-shadow: 0 8px 24px rgba(24,144,255,0.4);
}

.logo-title {
  font-size: 32px;
  font-weight: 700;
  color: #fff;
  letter-spacing: -0.5px;
}

.brand-slogan {
  font-size: 18px;
  color: rgba(255,255,255,0.7);
  margin-bottom: 48px;
  line-height: 1.6;
}

.features {
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.feature-item {
  display: flex;
  align-items: center;
  gap: 16px;
  color: rgba(255,255,255,0.85);
  font-size: 15px;
  text-align: left;
}

.feature-icon {
  width: 40px;
  height: 40px;
  background: rgba(24,144,255,0.2);
  border-radius: 10px;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}

.feature-icon svg {
  width: 20px;
  height: 20px;
  color: #40a9ff;
}

/* 右侧登录区 */
.login-section {
  width: 520px;
  display: flex;
  flex-direction: column;
  justify-content: center;
  padding: 60px;
  background: #fff;
}

.login-header {
  margin-bottom: 40px;
}

.login-title {
  font-size: 28px;
  font-weight: 600;
  color: #1f2937;
  margin: 0 0 8px 0;
}

.login-subtitle {
  color: #6b7280;
  font-size: 15px;
  margin: 0;
}

.login-form {
  display: flex;
  flex-direction: column;
  gap: 24px;
}

.form-group {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.form-label {
  font-size: 14px;
  font-weight: 500;
  color: #1f2937;
}

.form-input-wrapper {
  position: relative;
  display: flex;
  align-items: center;
}

.form-input-icon {
  position: absolute;
  left: 14px;
  color: #6b7280;
  z-index: 1;
  display: flex;
}

.form-input-icon svg {
  width: 18px;
  height: 18px;
}

.form-input {
  width: 100%;
  height: 48px;
  padding: 0 16px 0 44px;
  border: 1px solid #e5e7eb;
  border-radius: 8px;
  font-size: 15px;
  color: #1f2937;
  background: #fff;
  transition: all 0.2s;
}

.form-input:focus {
  outline: none;
  border-color: #1890ff;
  box-shadow: 0 0 0 3px rgba(24,144,255,0.1);
}

.form-input::placeholder {
  color: #9ca3af;
}

.password-toggle {
  position: absolute;
  right: 14px;
  background: none;
  border: none;
  color: #6b7280;
  cursor: pointer;
  padding: 4px;
  display: flex;
}

.password-toggle svg {
  width: 18px;
  height: 18px;
}

.password-toggle:hover {
  color: #1890ff;
}

.form-options {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.remember-me {
  display: flex;
  align-items: center;
  gap: 8px;
  cursor: pointer;
}

.remember-me input {
  width: 18px;
  height: 18px;
  accent-color: #1890ff;
  cursor: pointer;
}

.remember-me span {
  font-size: 14px;
  color: #6b7280;
}

.forgot-password {
  font-size: 14px;
  color: #1890ff;
  text-decoration: none;
}

.forgot-password:hover {
  text-decoration: underline;
}

.login-btn {
  height: 48px;
  background: linear-gradient(135deg, #1890ff, #096dd9);
  color: #fff;
  border: none;
  border-radius: 8px;
  font-size: 16px;
  font-weight: 600;
  cursor: pointer;
  transition: all 0.3s;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
}

.login-btn:hover:not(:disabled) {
  transform: translateY(-2px);
  box-shadow: 0 8px 20px rgba(24,144,255,0.4);
}

.login-btn:active {
  transform: translateY(0);
}

.login-btn:disabled {
  opacity: 0.8;
  cursor: not-allowed;
}

.spinner {
  width: 20px;
  height: 20px;
  border: 2px solid rgba(255,255,255,0.3);
  border-top-color: #fff;
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
}

@keyframes spin {
  to { transform: rotate(360deg); }
}

.divider {
  display: flex;
  align-items: center;
  gap: 16px;
  color: #6b7280;
  font-size: 13px;
}

.divider::before,
.divider::after {
  content: '';
  flex: 1;
  height: 1px;
  background: #e5e7eb;
}

.other-login {
  display: flex;
  justify-content: center;
  gap: 16px;
}

.social-btn {
  width: 48px;
  height: 48px;
  border: 1px solid #e5e7eb;
  border-radius: 10px;
  background: #fff;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #6b7280;
  transition: all 0.2s;
}

.social-btn svg {
  width: 24px;
  height: 24px;
}

.social-btn:hover {
  border-color: #1890ff;
  background: rgba(24,144,255,0.05);
  color: #1890ff;
}

.oidc-btn {
  width: 100%;
  padding: 14px 20px;
  border: 2px solid #1890ff;
  border-radius: 10px;
  background: linear-gradient(135deg, rgba(24,144,255,0.08), rgba(24,144,255,0.02));
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 10px;
  color: #1890ff;
  font-size: 15px;
  font-weight: 600;
  transition: all 0.2s;
}
.oidc-btn svg { width: 20px; height: 20px; }
.oidc-btn:hover {
  background: #1890ff;
  color: #fff;
  box-shadow: 0 4px 16px rgba(24,144,255,0.35);
  transform: translateY(-1px);
}
.oidc-btn:disabled {
  opacity: 0.6;
  cursor: not-allowed;
  transform: none;
}
.spinner-icon {
  animation: spin 1s linear infinite;
}
@keyframes spin {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}

.login-footer {
  margin-top: 40px;
  text-align: center;
  color: #6b7280;
  font-size: 13px;
}

.login-footer a {
  color: #1890ff;
  text-decoration: none;
}

.login-footer a:hover {
  text-decoration: underline;
}

.error-message {
  background: #fef2f2;
  border: 1px solid #fecaca;
  color: #dc2626;
  padding: 12px 16px;
  border-radius: 8px;
  font-size: 14px;
  display: flex;
  align-items: center;
  gap: 8px;
}

.error-message svg {
  width: 18px;
  height: 18px;
  flex-shrink: 0;
}

/* MFA 验证样式 */
.mfa-section {
  display: flex;
  flex-direction: column;
  gap: 24px;
}

.mfa-user-info {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 16px;
  background: #f8fafc;
  border-radius: 12px;
}

.mfa-avatar {
  width: 48px;
  height: 48px;
  background: linear-gradient(135deg, #1890ff, #096dd9);
  border-radius: 12px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #fff;
  font-size: 20px;
  font-weight: 600;
}

.mfa-code-input-wrap {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.mfa-code-input {
  width: 100%;
  height: 48px;
  padding: 0 16px;
  border: 2px solid #e5e7eb;
  border-radius: 10px;
  font-size: 22px;
  font-weight: 600;
  text-align: center;
  letter-spacing: 6px;
  color: #1f2937;
  transition: all 0.2s;
}

.mfa-code-input:focus {
  outline: none;
  border-color: #1890ff;
  box-shadow: 0 0 0 3px rgba(24,144,255,0.1);
}

.mfa-code-input::placeholder {
  color: #d1d5db;
  letter-spacing: 6px;
}

.mfa-actions {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.mfa-back-btn {
  background: none;
  border: none;
  color: #6b7280;
  font-size: 14px;
  cursor: pointer;
  padding: 8px;
}

.mfa-back-btn:hover {
  color: #1890ff;
}

/* MFA 绑定界面样式 */
.bind-section {
  text-align: center;
}

.bind-header {
  margin-bottom: 16px;
}

.bind-icon {
  width: 48px;
  height: 48px;
  margin: 0 auto 10px;
  background: linear-gradient(135deg, #10b981, #059669);
  border-radius: 12px;
  display: flex;
  align-items: center;
  justify-content: center;
  box-shadow: 0 4px 16px rgba(16,185,129,0.25);
}

.bind-icon svg {
  width: 24px;
  height: 24px;
  stroke: white;
}

.bind-title {
  font-size: 1.125rem;
  font-weight: 700;
  color: #1f2937;
  margin-bottom: 4px;
}

.bind-subtitle {
  font-size: 0.8rem;
  color: #6b7280;
  margin: 0;
}

.steps-indicator {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 0;
  margin: 16px 0;
}

.step {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 4px;
}

.step-dot {
  width: 24px;
  height: 24px;
  border-radius: 50%;
  background: #e5e7eb;
  color: #9ca3af;
  font-size: 12px;
  font-weight: 600;
  display: flex;
  align-items: center;
  justify-content: center;
}

.step.done .step-dot {
  background: #10b981;
  color: white;
}

.step.active .step-dot {
  background: #3b82f6;
  color: white;
}

.step-label {
  font-size: 11px;
  color: #6b7280;
}

.step.done .step-label,
.step.active .step-label {
  color: #1f2937;
  font-weight: 500;
}

.step-line {
  width: 40px;
  height: 2px;
  background: #e5e7eb;
  margin: 0 8px 20px;
}

.step-line.done {
  background: #10b981;
}

.bind-qr-container {
  background: #f8fafc;
  border-radius: 12px;
  padding: 16px;
  margin-bottom: 16px;
}

.bind-qr-wrapper {
  background: white;
  padding: 12px;
  border-radius: 10px;
  display: inline-block;
  box-shadow: 0 2px 8px rgba(0,0,0,0.06);
  margin-bottom: 12px;
}

.bind-qr-img {
  width: 120px;
  height: 120px;
  display: block;
}

.bind-qr-placeholder {
  width: 120px;
  height: 120px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #9ca3af;
  font-size: 12px;
}

.bind-secret-section {
  text-align: center;
}

.bind-secret-label {
  font-size: 12px;
  color: #6b7280;
  margin-bottom: 6px;
}

.bind-secret-key {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 6px 12px;
  background: white;
  border: 1px solid #e5e7eb;
  border-radius: 6px;
}

.bind-secret-key code {
  font-family: monospace;
  font-size: 12px;
  color: #1f2937;
  letter-spacing: 0.5px;
}

.bind-copy-btn {
  background: none;
  border: none;
  color: #6b7280;
  cursor: pointer;
  padding: 2px;
  display: flex;
}

.bind-copy-btn:hover {
  color: #3b82f6;
}

.bind-code-section {
  margin-bottom: 16px;
}

.bind-code-label {
  font-size: 13px;
  color: #374151;
  font-weight: 500;
  margin-bottom: 8px;
}

.bind-submit-btn {
  width: 100%;
  height: 44px;
  background: linear-gradient(135deg, #10b981, #059669);
  color: white;
  border: none;
  border-radius: 10px;
  font-size: 14px;
  font-weight: 600;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  transition: all 0.2s;
  margin-bottom: 12px;
}

.bind-submit-btn:hover:not(:disabled) {
  transform: translateY(-1px);
  box-shadow: 0 4px 16px rgba(16,185,129,0.3);
}

.bind-submit-btn:disabled {
  opacity: 0.6;
  cursor: not-allowed;
}

.bind-submit-btn.loading {
  pointer-events: none;
}

.bind-back-link {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  color: #6b7280;
  font-size: 13px;
  cursor: pointer;
  transition: color 0.2s;
}

.bind-back-link:hover {
  color: #3b82f6;
}

/* 响应式 */
@media (max-width: 1024px) {
  .brand-section {
    display: none;
  }
  
  .login-section {
    width: 100%;
    max-width: 480px;
    margin: 0 auto;
  }
}

@media (max-width: 480px) {
  .login-section {
    padding: 40px 24px;
  }
}
</style>
