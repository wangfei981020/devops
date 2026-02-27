<script setup>
import { ref, onMounted } from 'vue'
import { useAppStore } from '@/stores'
import api from '@/api'

const appStore = useAppStore()

const loading = ref(false)
const saving = ref(false)
const roles = ref([])

const oidcConfig = ref({
  enabled: false,
  provider_name: '',
  client_id: '',
  client_secret: '',
  issuer_url: '',
  redirect_uri: '',
  scopes: 'openid profile email',
  auto_register: true,
  default_role: ''
})

onMounted(() => {
  loadConfig()
  loadRoles()
})

async function loadRoles() {
  try {
    const res = await api.get('/api/roles')
    roles.value = res.data || []
  } catch (e) {
    console.log('加载角色列表失败:', e)
  }
}

async function loadConfig() {
  loading.value = true
  try {
    const res = await api.get('/api/oidc/admin-config')
    Object.assign(oidcConfig.value, res.data)
  } catch (e) {
    console.log('加载OIDC配置失败:', e)
  } finally {
    loading.value = false
  }
}

async function saveConfig() {
  saving.value = true
  try {
    await api.post('/api/oidc/admin-config', oidcConfig.value)
    appStore.showToast('SSO配置已保存', 'success')
  } catch (e) {
    appStore.showToast('保存失败: ' + (e.response?.data?.error || e.message), 'error')
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <div class="sso-config-page">
    <div class="page-header">
      <h1>SSO 单点登录配置</h1>
      <p class="page-desc">配置 OIDC/OAuth2.0 身份提供商，实现从外部平台单点登录</p>
    </div>

    <div class="config-card" v-if="!loading">
      <div class="config-section">
        <h2>基础配置</h2>
        
        <div class="setting-row">
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
          <label>身份提供商名称 <span class="required">*</span></label>
          <input type="text" class="form-input" v-model="oidcConfig.provider_name" placeholder="如: 统一门户、企业微信、Keycloak">
          <span class="form-hint">显示在登录按钮上的名称，如 "统一门户 登录"</span>
        </div>
      </div>

      <div class="config-section">
        <h2>OIDC 配置</h2>
        
        <div class="form-row">
          <div class="form-group">
            <label>Client ID <span class="required">*</span></label>
            <input type="text" class="form-input" v-model="oidcConfig.client_id" placeholder="从身份提供商获取">
          </div>
          <div class="form-group">
            <label>Client Secret <span class="required">*</span></label>
            <input type="password" class="form-input" v-model="oidcConfig.client_secret" placeholder="从身份提供商获取">
          </div>
        </div>

        <div class="form-group">
          <label>Issuer URL <span class="required">*</span></label>
          <input type="text" class="form-input" v-model="oidcConfig.issuer_url" placeholder="https://sso.example.com">
          <span class="form-hint">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><line x1="12" y1="16" x2="12" y2="12"/><line x1="12" y1="8" x2="12.01" y2="8"/></svg>
            身份提供商的基础地址，系统会自动从 /.well-known/openid-configuration 发现端点
          </span>
        </div>

        <div class="form-group">
          <label>Redirect URI (回调地址) <span class="required">*</span></label>
          <input type="text" class="form-input" v-model="oidcConfig.redirect_uri" placeholder="https://your-ops-domain.com/api/oidc/callback">
          <span class="form-hint">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><line x1="12" y1="16" x2="12" y2="12"/><line x1="12" y1="8" x2="12.01" y2="8"/></svg>
            填写本运维平台的回调地址（如 https://ops.yourdomain.com/api/oidc/callback），需在 SSO 提供商处配置
          </span>
        </div>

        <div class="form-group">
          <label>Scopes</label>
          <input type="text" class="form-input" v-model="oidcConfig.scopes" placeholder="openid profile email">
          <span class="form-hint">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><line x1="12" y1="16" x2="12" y2="12"/><line x1="12" y1="8" x2="12.01" y2="8"/></svg>
            OIDC 请求的权限范围，多个用空格分隔（默认: openid profile email）
          </span>
        </div>
      </div>

      <div class="config-section">
        <h2>用户设置</h2>

        <div class="setting-row">
          <div class="setting-info">
            <label>自动注册用户</label>
            <span class="setting-desc">SSO 登录时自动创建本地用户账号（如关闭则用户需先在本系统存在）</span>
          </div>
          <label class="toggle-switch">
            <input type="checkbox" v-model="oidcConfig.auto_register">
            <span class="toggle-slider"></span>
          </label>
        </div>

        <div class="form-group" v-if="oidcConfig.auto_register">
          <label>新用户默认角色</label>
          <select class="form-input" v-model="oidcConfig.default_role">
            <option value="">请选择角色</option>
            <option v-for="role in roles" :key="role.id" :value="role.code">{{ role.name }}</option>
          </select>
          <span class="form-hint">通过SSO首次登录创建的用户默认角色（从角色管理中选择）</span>
        </div>
      </div>

      <div class="form-actions">
        <button class="btn btn-primary" @click="saveConfig" :disabled="saving">
          <svg v-if="saving" class="spinner" viewBox="0 0 24 24"><circle cx="12" cy="12" r="10" stroke="currentColor" stroke-width="3" fill="none" stroke-dasharray="30 70"/></svg>
          {{ saving ? '保存中...' : '保存配置' }}
        </button>
      </div>
    </div>

    <div class="loading-state" v-else>
      <div class="spinner-lg"></div>
      <p>加载配置中...</p>
    </div>
  </div>
</template>

<style scoped>
.sso-config-page {
  max-width: 800px;
}

.page-header {
  margin-bottom: 24px;
}
.page-header h1 {
  font-size: 22px;
  font-weight: 600;
  margin: 0 0 8px 0;
}
.page-desc {
  color: var(--text-muted);
  font-size: 14px;
  margin: 0;
}

.config-card {
  background: var(--bg-card);
  border: 1px solid var(--border-color);
  border-radius: 12px;
  padding: 24px;
}

.config-section {
  padding-bottom: 24px;
  margin-bottom: 24px;
  border-bottom: 1px solid var(--border-color);
}
.config-section:last-of-type {
  border-bottom: none;
  margin-bottom: 0;
  padding-bottom: 0;
}
.config-section h2 {
  font-size: 16px;
  font-weight: 600;
  margin: 0 0 20px 0;
  color: var(--text-primary);
}

.form-group {
  margin-bottom: 20px;
}
.form-group label {
  display: block;
  font-size: 14px;
  font-weight: 500;
  color: var(--text-secondary);
  margin-bottom: 8px;
}
.form-group .required {
  color: #ef4444;
}

.form-input {
  width: 100%;
  padding: 10px 14px;
  background: var(--bg-input);
  border: 1px solid var(--border-color);
  border-radius: 8px;
  color: var(--text-primary);
  font-size: 14px;
  transition: border-color 0.2s;
}
.form-input:focus {
  outline: none;
  border-color: var(--primary);
}
.form-input::placeholder {
  color: var(--text-muted);
}

.form-hint {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  color: var(--text-muted);
  margin-top: 6px;
}
.form-hint svg {
  width: 14px;
  height: 14px;
  color: var(--primary);
}

.form-row {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 16px;
}

.setting-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 16px 0;
  border-bottom: 1px solid var(--border-color);
}
.setting-row:last-child {
  border-bottom: none;
}
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
  flex-shrink: 0;
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
  margin-top: 24px;
  padding-top: 24px;
  border-top: 1px solid var(--border-color);
}

.btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 12px 24px;
  border: none;
  border-radius: 8px;
  cursor: pointer;
  font-size: 14px;
  font-weight: 500;
  transition: all 0.2s;
}
.btn-primary {
  background: var(--primary);
  color: #fff;
}
.btn-primary:hover {
  background: #2563eb;
  box-shadow: 0 4px 12px rgba(59,130,246,0.3);
}
.btn-primary:disabled {
  opacity: 0.6;
  cursor: not-allowed;
}

.spinner {
  width: 18px;
  height: 18px;
  animation: spin 1s linear infinite;
}
@keyframes spin {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}

.loading-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 60px 20px;
  color: var(--text-muted);
}
.spinner-lg {
  width: 40px;
  height: 40px;
  border: 3px solid var(--border-color);
  border-top-color: var(--primary);
  border-radius: 50%;
  animation: spin 1s linear infinite;
  margin-bottom: 16px;
}

@media (max-width: 768px) {
  .form-row {
    grid-template-columns: 1fr;
  }
}
</style>
