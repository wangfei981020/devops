<script setup>
import { ref, onMounted } from 'vue'
import api from '@/api'
import { useAppStore } from '@/stores/app'

const appStore = useAppStore()
const settings = ref([])
const loading = ref(false)

const settingLabels = {
  local_login_enabled: { label: '本地用户登录', description: '启用后，source 为 local 的用户可使用用户名密码直接登录，无需 SSO' }
}

async function loadSettings() {
  loading.value = true
  try {
    const res = await api.get('/api/admin/settings')
    settings.value = res.data?.data || []
  } catch (e) {
    appStore.showToast('加载设置失败', 'error')
  } finally {
    loading.value = false
  }
}

async function toggleSetting(setting) {
  const newValue = setting.value === 'true' ? 'false' : 'true'
  try {
    await api.put('/api/admin/settings', { key: setting.key, value: newValue })
    setting.value = newValue
    appStore.showToast('设置已更新', 'success')
  } catch (e) {
    appStore.showToast(e.response?.data?.message || '更新失败', 'error')
  }
}

onMounted(loadSettings)
</script>

<template>
  <div class="page">
    <div class="settings-list">
      <div v-for="setting in settings" :key="setting.key" class="setting-item">
        <div class="setting-info">
          <div class="setting-label">
            {{ settingLabels[setting.key]?.label || setting.key }}
          </div>
          <div class="setting-desc">
            {{ settingLabels[setting.key]?.description || setting.description }}
          </div>
        </div>
        <div class="setting-control">
          <label class="toggle">
            <input type="checkbox" :checked="setting.value === 'true'" @change="toggleSetting(setting)" />
            <span class="toggle-slider"></span>
          </label>
        </div>
      </div>
      <div v-if="settings.length === 0" class="empty">
        {{ loading ? '加载中...' : '暂无设置项' }}
      </div>
    </div>
  </div>
</template>

<style scoped>
.page { max-width: 800px; }
.settings-list { display: flex; flex-direction: column; gap: 1px; background: rgba(255,255,255,0.04); border-radius: 10px; overflow: hidden; border: 1px solid rgba(255,255,255,0.06); }
.setting-item {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 20px 24px;
  background: #1e293b;
}
.setting-label { font-size: 15px; color: #f1f5f9; font-weight: 500; margin-bottom: 4px; }
.setting-desc { font-size: 13px; color: #64748b; }
.empty { text-align: center; color: #64748b; padding: 40px; background: #1e293b; }

/* Toggle switch */
.toggle { position: relative; display: inline-block; width: 48px; height: 26px; cursor: pointer; }
.toggle input { opacity: 0; width: 0; height: 0; }
.toggle-slider {
  position: absolute;
  inset: 0;
  background: #334155;
  border-radius: 26px;
  transition: 0.3s;
}
.toggle-slider::before {
  content: '';
  position: absolute;
  width: 20px;
  height: 20px;
  left: 3px;
  bottom: 3px;
  background: white;
  border-radius: 50%;
  transition: 0.3s;
}
.toggle input:checked + .toggle-slider { background: #3b82f6; }
.toggle input:checked + .toggle-slider::before { transform: translateX(22px); }
</style>
