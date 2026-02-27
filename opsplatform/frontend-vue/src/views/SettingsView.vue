<script setup>
import { ref, onMounted } from 'vue'
import { useAppStore, useAuthStore } from '@/stores'
import api from '@/api'

const appStore = useAppStore()
const authStore = useAuthStore()

const activeTab = ref('profile')

const profile = ref({
  display_name: '',
  email: '',
  phone: ''
})

const passwordForm = ref({
  old_password: '',
  new_password: '',
  confirm_password: ''
})

const mfaStatus = ref({
  enabled: false,
  secret: '',
  qrCode: ''
})

const loading = ref(false)
const savingProfile = ref(false)
const changingPassword = ref(false)

const isAdmin = ref(false)
const oidcConfig = ref({
  enabled: false,
  provider_name: '',
  client_id: '',
  client_secret: '',
  auth_url: '',
  token_url: '',
  userinfo_url: '',
  redirect_uri: '',
  scopes: 'openid profile email',
  auto_register: true,
  default_role: 'user'
})
const savingOidc = ref(false)

onMounted(async () => {
  loadProfile()
  loadMfaStatus()
  await checkAdminRole()
})

async function checkAdminRole() {
  let user = authStore.user
  if (!user) {
    const cached = localStorage.getItem('currentUser')
    if (cached) {
      try {
        user = JSON.parse(cached)
      } catch (e) {}
    }
  }
  if (!user) {
    user = await authStore.fetchUser()
  }
  isAdmin.value = user?.role === 'admin'
  if (isAdmin.value) {
    loadOidcConfig()
  }
}

async function loadOidcConfig() {
  try {
    const res = await api.get('/api/oidc/config')
    Object.assign(oidcConfig.value, res.data)
  } catch (e) {
    console.log('加载OIDC配置失败')
  }
}

async function saveOidcConfig() {
  savingOidc.value = true
  try {
    await api.post('/api/oidc/config', oidcConfig.value)
    appStore.showToast('OIDC配置已保存', 'success')
  } catch (e) {
    appStore.showToast('保存失败: ' + (e.response?.data?.error || e.message), 'error')
  } finally {
    savingOidc.value = false
  }
}

async function loadProfile() {
  try {
    const res = await api.get('/api/users/me')
    profile.value = {
      display_name: res.data.display_name || '',
      email: res.data.email || '',
      phone: res.data.phone || ''
    }
  } catch (e) {
    console.error('加载个人信息失败', e)
  }
}

async function loadMfaStatus() {
  try {
    const res = await api.get('/api/users/me/mfa')
    mfaStatus.value.enabled = res.data.mfa_enabled || false
  } catch (e) {
    console.error('加载MFA状态失败', e)
  }
}

async function saveProfile() {
  savingProfile.value = true
  try {
    await api.put('/api/users/me', profile.value)
    appStore.showToast('个人信息已更新', 'success')
  } catch (e) {
    appStore.showToast('更新失败: ' + (e.response?.data?.error || e.message), 'error')
  } finally {
    savingProfile.value = false
  }
}

async function changePassword() {
  if (!passwordForm.value.old_password) {
    appStore.showToast('请输入原密码', 'error')
    return
  }
  if (!passwordForm.value.new_password) {
    appStore.showToast('请输入新密码', 'error')
    return
  }
  if (passwordForm.value.new_password !== passwordForm.value.confirm_password) {
    appStore.showToast('两次输入的密码不一致', 'error')
    return
  }
  if (passwordForm.value.new_password.length < 6) {
    appStore.showToast('密码长度至少6位', 'error')
    return
  }

  changingPassword.value = true
  try {
    await api.put('/api/users/me/password', {
      old_password: passwordForm.value.old_password,
      new_password: passwordForm.value.new_password
    })
    appStore.showToast('密码修改成功', 'success')
    passwordForm.value = { old_password: '', new_password: '', confirm_password: '' }
  } catch (e) {
    appStore.showToast('密码修改失败: ' + (e.response?.data?.error || e.message), 'error')
  } finally {
    changingPassword.value = false
  }
}

async function enableMfa() {
  try {
    const res = await api.post('/api/users/me/mfa/setup')
    mfaStatus.value.secret = res.data.secret
    mfaStatus.value.qrCode = res.data.qr_code
  } catch (e) {
    appStore.showToast('设置MFA失败', 'error')
  }
}

async function disableMfa() {
  const confirmed = await appStore.showConfirm({
    type: 'warning',
    title: '关闭双因素认证',
    message: '确定要关闭双因素认证吗？\n关闭后账户安全性会降低。',
    okText: '确定关闭',
    cancelText: '取消'
  })
  if (!confirmed) return
  try {
    await api.delete('/api/users/me/mfa')
    mfaStatus.value.enabled = false
    mfaStatus.value.secret = ''
    mfaStatus.value.qrCode = ''
    appStore.showToast('MFA已关闭', 'success')
  } catch (e) {
    appStore.showToast('操作失败', 'error')
  }
}

function toggleTheme() {
  appStore.toggleTheme()
}
</script>

<template>
  <div class="settings-page">
    <div class="settings-sidebar">
      <button 
        class="sidebar-item" 
        :class="{ active: activeTab === 'profile' }"
        @click="activeTab = 'profile'"
      >
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <path d="M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2"></path>
          <circle cx="12" cy="7" r="4"></circle>
        </svg>
        个人信息
      </button>
      <button 
        class="sidebar-item" 
        :class="{ active: activeTab === 'password' }"
        @click="activeTab = 'password'"
      >
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <rect x="3" y="11" width="18" height="11" rx="2" ry="2"></rect>
          <path d="M7 11V7a5 5 0 0 1 10 0v4"></path>
        </svg>
        修改密码
      </button>
      <button 
        class="sidebar-item" 
        :class="{ active: activeTab === 'security' }"
        @click="activeTab = 'security'"
      >
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"></path>
        </svg>
        安全设置
      </button>
      <button 
        class="sidebar-item" 
        :class="{ active: activeTab === 'appearance' }"
        @click="activeTab = 'appearance'"
      >
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <circle cx="12" cy="12" r="5"></circle>
          <line x1="12" y1="1" x2="12" y2="3"></line>
          <line x1="12" y1="21" x2="12" y2="23"></line>
          <line x1="4.22" y1="4.22" x2="5.64" y2="5.64"></line>
          <line x1="18.36" y1="18.36" x2="19.78" y2="19.78"></line>
          <line x1="1" y1="12" x2="3" y2="12"></line>
          <line x1="21" y1="12" x2="23" y2="12"></line>
          <line x1="4.22" y1="19.78" x2="5.64" y2="18.36"></line>
          <line x1="18.36" y1="5.64" x2="19.78" y2="4.22"></line>
        </svg>
        外观设置
      </button>
      <button 
        v-if="isAdmin"
        class="sidebar-item" 
        :class="{ active: activeTab === 'oidc' }"
        @click="activeTab = 'oidc'"
      >
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <path d="M15 3h4a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2h-4"/>
          <polyline points="10 17 15 12 10 7"/>
          <line x1="15" y1="12" x2="3" y2="12"/>
        </svg>
        SSO 配置
      </button>
    </div>

    <div class="settings-content">
      <!-- 个人信息 -->
      <div class="settings-panel" v-if="activeTab === 'profile'">
        <h2>个人信息</h2>
        <p class="panel-desc">管理您的个人资料信息</p>
        
        <div class="form-section">
          <div class="form-group">
            <label>显示名称</label>
            <input type="text" v-model="profile.display_name" placeholder="您的显示名称">
          </div>
          <div class="form-group">
            <label>邮箱地址</label>
            <input type="email" v-model="profile.email" placeholder="用于接收通知">
          </div>
          <div class="form-group">
            <label>手机号码</label>
            <input type="text" v-model="profile.phone" placeholder="用于接收短信通知">
          </div>
          <button class="btn btn-primary" @click="saveProfile" :disabled="savingProfile">
            {{ savingProfile ? '保存中...' : '保存更改' }}
          </button>
        </div>
      </div>

      <!-- 修改密码 -->
      <div class="settings-panel" v-if="activeTab === 'password'">
        <h2>修改密码</h2>
        <p class="panel-desc">定期更换密码可以提高账户安全性</p>
        
        <div class="form-section">
          <div class="form-group">
            <label>原密码</label>
            <input type="password" v-model="passwordForm.old_password" placeholder="输入当前密码">
          </div>
          <div class="form-group">
            <label>新密码</label>
            <input type="password" v-model="passwordForm.new_password" placeholder="至少6位字符">
          </div>
          <div class="form-group">
            <label>确认新密码</label>
            <input type="password" v-model="passwordForm.confirm_password" placeholder="再次输入新密码">
          </div>
          <button class="btn btn-primary" @click="changePassword" :disabled="changingPassword">
            {{ changingPassword ? '修改中...' : '修改密码' }}
          </button>
        </div>
      </div>

      <!-- 安全设置 -->
      <div class="settings-panel" v-if="activeTab === 'security'">
        <h2>安全设置</h2>
        <p class="panel-desc">增强账户安全保护</p>
        
        <div class="security-item">
          <div class="security-info">
            <h3>双因素认证 (MFA)</h3>
            <p>使用验证器应用提供额外的安全保护</p>
          </div>
          <div class="security-action">
            <span class="mfa-status" :class="mfaStatus.enabled ? 'enabled' : 'disabled'">
              {{ mfaStatus.enabled ? '已开启' : '未开启' }}
            </span>
            <button 
              v-if="!mfaStatus.enabled" 
              class="btn btn-primary" 
              @click="enableMfa"
            >
              开启
            </button>
            <button 
              v-else 
              class="btn btn-secondary" 
              @click="disableMfa"
            >
              关闭
            </button>
          </div>
        </div>

        <div class="mfa-setup" v-if="mfaStatus.qrCode">
          <p>请使用验证器应用扫描二维码：</p>
          <img :src="mfaStatus.qrCode" alt="MFA QR Code" class="qr-code">
          <p class="secret-key">密钥：<code>{{ mfaStatus.secret }}</code></p>
        </div>
      </div>

      <!-- 外观设置 -->
      <div class="settings-panel" v-if="activeTab === 'appearance'">
        <h2>外观设置</h2>
        <p class="panel-desc">自定义界面外观</p>
        
        <div class="appearance-item">
          <div class="appearance-info">
            <h3>主题模式</h3>
            <p>选择您喜欢的界面主题</p>
          </div>
          <div class="theme-options">
            <button 
              class="theme-option" 
              :class="{ active: appStore.theme === 'dark' }"
              @click="appStore.setTheme('dark')"
            >
              <div class="theme-preview dark">
                <span>Aa</span>
              </div>
              深色
            </button>
            <button 
              class="theme-option" 
              :class="{ active: appStore.theme === 'light' }"
              @click="appStore.setTheme('light')"
            >
              <div class="theme-preview light">
                <span>Aa</span>
              </div>
              浅色
            </button>
          </div>
        </div>
      </div>

      <!-- OIDC/SSO 配置（仅管理员） -->
      <div class="settings-panel" v-if="activeTab === 'oidc' && isAdmin">
        <h2>SSO 单点登录配置</h2>
        <p class="panel-desc">配置 OIDC/OAuth2.0 身份提供商，实现单点登录</p>

        <div class="form-section">
          <div class="setting-row toggle-row">
            <div class="setting-info">
              <label>启用 SSO</label>
              <span class="setting-desc">开启后登录页将显示 SSO 登录按钮</span>
            </div>
            <label class="toggle-switch">
              <input type="checkbox" v-model="oidcConfig.enabled">
              <span class="toggle-slider"></span>
            </label>
          </div>

          <div class="form-group">
            <label>身份提供商名称</label>
            <input type="text" class="form-input" v-model="oidcConfig.provider_name" placeholder="如: 统一门户、企业微信">
            <span class="form-hint">显示在登录按钮上的名称</span>
          </div>

          <div class="form-row">
            <div class="form-group">
              <label>Client ID</label>
              <input type="text" class="form-input" v-model="oidcConfig.client_id" placeholder="OIDC Client ID">
            </div>
            <div class="form-group">
              <label>Client Secret</label>
              <input type="password" class="form-input" v-model="oidcConfig.client_secret" placeholder="OIDC Client Secret">
            </div>
          </div>

          <div class="form-group">
            <label>Authorization URL</label>
            <input type="text" class="form-input" v-model="oidcConfig.auth_url" placeholder="https://idp.example.com/oauth2/authorize">
          </div>

          <div class="form-group">
            <label>Token URL</label>
            <input type="text" class="form-input" v-model="oidcConfig.token_url" placeholder="https://idp.example.com/oauth2/token">
          </div>

          <div class="form-group">
            <label>UserInfo URL</label>
            <input type="text" class="form-input" v-model="oidcConfig.userinfo_url" placeholder="https://idp.example.com/oauth2/userinfo">
          </div>

          <div class="form-group">
            <label>Redirect URI</label>
            <input type="text" class="form-input" v-model="oidcConfig.redirect_uri" placeholder="https://your-domain.com/api/oidc/callback">
            <span class="form-hint">需要在身份提供商处配置此回调地址</span>
          </div>

          <div class="form-group">
            <label>Scopes</label>
            <input type="text" class="form-input" v-model="oidcConfig.scopes" placeholder="openid profile email">
          </div>

          <div class="setting-row toggle-row">
            <div class="setting-info">
              <label>自动注册用户</label>
              <span class="setting-desc">SSO 登录时自动创建本地用户账号</span>
            </div>
            <label class="toggle-switch">
              <input type="checkbox" v-model="oidcConfig.auto_register">
              <span class="toggle-slider"></span>
            </label>
          </div>

          <div class="form-group" v-if="oidcConfig.auto_register">
            <label>新用户默认角色</label>
            <select class="form-input" v-model="oidcConfig.default_role">
              <option value="user">普通用户</option>
              <option value="viewer">只读用户</option>
              <option value="admin">管理员</option>
            </select>
          </div>

          <div class="form-actions">
            <button class="btn btn-primary" @click="saveOidcConfig" :disabled="savingOidc">
              {{ savingOidc ? '保存中...' : '保存配置' }}
            </button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.settings-page {
  display: grid;
  grid-template-columns: 220px 1fr;
  gap: 24px;
  min-height: 100%;
}

.settings-sidebar {
  display: flex;
  flex-direction: column;
  gap: 4px;
  background: var(--bg-card);
  border: 1px solid var(--border-color);
  border-radius: 12px;
  padding: 12px;
  height: fit-content;
}

.sidebar-item {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px 16px;
  background: transparent;
  border: none;
  border-radius: 8px;
  color: var(--text-secondary);
  font-size: 14px;
  cursor: pointer;
  transition: all 0.2s;
  text-align: left;
}

.sidebar-item:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}

.sidebar-item.active {
  background: var(--primary-bg);
  color: var(--primary);
}

.sidebar-item svg {
  width: 18px;
  height: 18px;
}

.settings-content {
  flex: 1;
}

.settings-panel {
  background: var(--bg-card);
  border: 1px solid var(--border-color);
  border-radius: 12px;
  padding: 24px;
}

.settings-panel h2 {
  margin: 0 0 8px;
  font-size: 20px;
  color: var(--text-primary);
}

.panel-desc {
  margin: 0 0 24px;
  color: var(--text-muted);
  font-size: 14px;
}

.form-section {
  display: flex;
  flex-direction: column;
  gap: 16px;
  max-width: 400px;
}

.form-group {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.form-group label {
  font-size: 14px;
  font-weight: 500;
  color: var(--text-secondary);
}

.form-group input {
  padding: 10px 14px;
  background: var(--bg-input);
  border: 1px solid var(--border-color);
  border-radius: 8px;
  color: var(--text-primary);
  font-size: 14px;
}

.form-group input:focus {
  outline: none;
  border-color: var(--primary);
}

.btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  padding: 10px 20px;
  border: none;
  border-radius: 8px;
  cursor: pointer;
  font-size: 14px;
  font-weight: 500;
  transition: all 0.2s;
  width: fit-content;
}

.btn-primary {
  background: var(--primary);
  color: #fff;
}

.btn-primary:hover {
  background: var(--primary-dark);
}

.btn-primary:disabled {
  opacity: 0.6;
  cursor: not-allowed;
}

.btn-secondary {
  background: var(--bg-hover);
  color: var(--text-primary);
  border: 1px solid var(--border-color);
}

.security-item, .appearance-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 20px;
  background: var(--bg-hover);
  border-radius: 12px;
  margin-bottom: 16px;
}

.security-info h3, .appearance-info h3 {
  margin: 0 0 4px;
  font-size: 16px;
  color: var(--text-primary);
}

.security-info p, .appearance-info p {
  margin: 0;
  font-size: 13px;
  color: var(--text-muted);
}

.security-action {
  display: flex;
  align-items: center;
  gap: 12px;
}

.mfa-status {
  padding: 4px 12px;
  border-radius: 20px;
  font-size: 12px;
  font-weight: 500;
}

.mfa-status.enabled {
  background: rgba(16, 185, 129, 0.1);
  color: #10b981;
}

.mfa-status.disabled {
  background: rgba(107, 114, 128, 0.1);
  color: #6b7280;
}

.mfa-setup {
  margin-top: 20px;
  padding: 20px;
  background: var(--bg-hover);
  border-radius: 12px;
  text-align: center;
}

.qr-code {
  width: 200px;
  height: 200px;
  margin: 16px 0;
  border-radius: 8px;
}

.secret-key {
  font-size: 13px;
  color: var(--text-muted);
}

.secret-key code {
  padding: 4px 8px;
  background: var(--bg-input);
  border-radius: 4px;
  font-family: 'Fira Code', monospace;
}

.theme-options {
  display: flex;
  gap: 12px;
}

.theme-option {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
  padding: 12px;
  background: transparent;
  border: 2px solid var(--border-color);
  border-radius: 12px;
  cursor: pointer;
  font-size: 13px;
  color: var(--text-secondary);
  transition: all 0.2s;
}

.theme-option:hover {
  border-color: var(--primary);
}

.theme-option.active {
  border-color: var(--primary);
  color: var(--primary);
}

.theme-preview {
  width: 60px;
  height: 40px;
  border-radius: 6px;
  display: flex;
  align-items: center;
  justify-content: center;
  font-weight: 600;
}

.theme-preview.dark {
  background: #1a1a2e;
  color: #fff;
}

.theme-preview.light {
  background: #f8f9fa;
  color: #1a1a2e;
  border: 1px solid #e0e0e0;
}

@media (max-width: 768px) {
  .settings-page {
    grid-template-columns: 1fr;
  }
  
  .settings-sidebar {
    flex-direction: row;
    overflow-x: auto;
  }
  
  .sidebar-item {
    white-space: nowrap;
  }
}

/* OIDC 配置样式 */
.form-section {
  display: flex;
  flex-direction: column;
  gap: 20px;
  max-width: 600px;
}

.form-row {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 16px;
}

.form-hint {
  font-size: 12px;
  color: var(--text-muted);
  margin-top: 4px;
}

.form-input {
  padding: 10px 14px;
  background: var(--bg-input);
  border: 1px solid var(--border-color);
  border-radius: 8px;
  color: var(--text-primary);
  font-size: 14px;
  width: 100%;
}
.form-input:focus {
  outline: none;
  border-color: var(--primary);
}

.setting-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 16px 0;
  border-bottom: 1px solid var(--border-color);
}
.setting-row:last-child { border-bottom: none; }

.setting-info {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.setting-info label {
  font-size: 14px;
  font-weight: 500;
  color: var(--text-primary);
}
.setting-desc {
  font-size: 12px;
  color: var(--text-muted);
}

.toggle-switch {
  position: relative;
  width: 48px;
  height: 26px;
}
.toggle-switch input {
  opacity: 0;
  width: 0;
  height: 0;
}
.toggle-slider {
  position: absolute;
  cursor: pointer;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  background-color: var(--bg-hover);
  border: 1px solid var(--border-color);
  border-radius: 26px;
  transition: 0.3s;
}
.toggle-slider:before {
  position: absolute;
  content: "";
  height: 20px;
  width: 20px;
  left: 2px;
  bottom: 2px;
  background-color: white;
  border-radius: 50%;
  transition: 0.3s;
  box-shadow: 0 2px 4px rgba(0,0,0,0.2);
}
.toggle-switch input:checked + .toggle-slider {
  background-color: var(--primary);
  border-color: var(--primary);
}
.toggle-switch input:checked + .toggle-slider:before {
  transform: translateX(22px);
}

.form-actions {
  margin-top: 16px;
  padding-top: 16px;
  border-top: 1px solid var(--border-color);
}
</style>
