<script setup>
import { ref, onMounted } from 'vue'
import api from '@/api'
import { useAppStore } from '@/stores/app'

const appStore = useAppStore()
const services = ref([])
const loading = ref(false)

// Create/Edit dialog
const showDialog = ref(false)
const editMode = ref(false)
const form = ref({ code: '', name: '', description: '', home_url: '', icon: '' })

// API Key display
const showKeyDialog = ref(false)
const displayKey = ref('')
const displayServiceName = ref('')

async function loadServices() {
  loading.value = true
  try {
    const res = await api.get('/api/admin/services')
    services.value = res.data?.data || []
  } catch (e) {
    appStore.showToast('加载失败', 'error')
  } finally {
    loading.value = false
  }
}

function openCreate() {
  editMode.value = false
  form.value = { code: '', name: '', description: '', home_url: '', icon: '' }
  showDialog.value = true
}

function openEdit(svc) {
  editMode.value = true
  form.value = {
    id: svc.id, code: svc.code, name: svc.name,
    description: svc.description, home_url: svc.home_url,
    icon: svc.icon, status: svc.status
  }
  showDialog.value = true
}

async function saveService() {
  try {
    if (editMode.value) {
      await api.put(`/api/admin/services/${form.value.id}`, form.value)
      appStore.showToast('保存成功', 'success')
    } else {
      const res = await api.post('/api/admin/services', form.value)
      const data = res.data?.data
      if (data?.api_key) {
        displayKey.value = data.api_key
        displayServiceName.value = form.value.name
        showKeyDialog.value = true
      }
      appStore.showToast('创建成功，请保存 API Key', 'success')
    }
    showDialog.value = false
    loadServices()
  } catch (e) {
    appStore.showToast(e.response?.data?.message || '保存失败', 'error')
  }
}

async function deleteService(svc) {
  if (!confirm(`确定删除服务 ${svc.name}？`)) return
  try {
    await api.delete(`/api/admin/services/${svc.id}`)
    appStore.showToast('删除成功', 'success')
    loadServices()
  } catch (e) {
    appStore.showToast(e.response?.data?.message || '删除失败', 'error')
  }
}

async function regenKey(svc) {
  if (!confirm(`确定重新生成 ${svc.name} 的 API Key？旧 Key 将立即失效。`)) return
  try {
    const res = await api.post(`/api/admin/services/${svc.id}/regen-key`)
    const newKey = res.data?.data?.api_key
    if (newKey) {
      displayKey.value = newKey
      displayServiceName.value = svc.name
      showKeyDialog.value = true
    }
    appStore.showToast('API Key 已重新生成', 'success')
  } catch (e) {
    appStore.showToast(e.response?.data?.message || '操作失败', 'error')
  }
}

function copyKey() {
  navigator.clipboard.writeText(displayKey.value).then(() => {
    appStore.showToast('已复制到剪贴板', 'success')
  }).catch(() => {
    appStore.showToast('复制失败，请手动复制', 'error')
  })
}

onMounted(() => {
  loadServices()
})
</script>

<template>
  <div class="page">
    <div class="toolbar">
      <button class="btn btn-primary" @click="openCreate">+ 注册服务</button>
    </div>

    <div class="table-wrap">
      <table>
        <thead>
          <tr>
            <th>图标</th>
            <th>服务代码</th>
            <th>名称</th>
            <th>描述</th>
            <th>访问地址</th>
            <th>状态</th>
            <th>操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="svc in services" :key="svc.id">
            <td class="icon-cell">{{ svc.icon || '🔧' }}</td>
            <td><code>{{ svc.code }}</code></td>
            <td>{{ svc.name }}</td>
            <td>{{ svc.description || '-' }}</td>
            <td>
              <a v-if="svc.home_url" :href="svc.home_url" target="_blank" class="link">{{ svc.home_url }}</a>
              <span v-else>-</span>
            </td>
            <td><span class="status" :class="svc.status">{{ svc.status }}</span></td>
            <td>
              <button class="btn-sm" @click="regenKey(svc)">重置Key</button>
              <button class="btn-sm" @click="openEdit(svc)">编辑</button>
              <button class="btn-sm btn-danger" @click="deleteService(svc)">删除</button>
            </td>
          </tr>
          <tr v-if="services.length === 0">
            <td colspan="7" class="empty">{{ loading ? '加载中...' : '暂无数据' }}</td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- Create/Edit Dialog -->
    <div class="modal-overlay" v-if="showDialog" @click="showDialog = false">
      <div class="modal" @click.stop>
        <h3>{{ editMode ? '编辑服务' : '注册服务' }}</h3>
        <div class="form-group" v-if="!editMode">
          <label>服务代码</label>
          <input v-model="form.code" placeholder="如 opsplatform" />
        </div>
        <div class="form-group">
          <label>名称</label>
          <input v-model="form.name" placeholder="如 运维平台" />
        </div>
        <div class="form-group">
          <label>描述</label>
          <input v-model="form.description" placeholder="服务描述" />
        </div>
        <div class="form-group">
          <label>访问地址</label>
          <input v-model="form.home_url" placeholder="如 http://192.168.1.100:30800" />
        </div>
        <div class="form-group">
          <label>图标</label>
          <input v-model="form.icon" placeholder="如 🖥️" />
        </div>
        <div class="modal-actions">
          <button class="btn" @click="showDialog = false">取消</button>
          <button class="btn btn-primary" @click="saveService">保存</button>
        </div>
      </div>
    </div>

    <!-- API Key Display Dialog -->
    <div class="modal-overlay" v-if="showKeyDialog" @click="showKeyDialog = false">
      <div class="modal" @click.stop>
        <h3>API Key — {{ displayServiceName }}</h3>
        <p class="key-warning">请立即保存此 API Key，关闭后将无法再次查看。</p>
        <div class="key-box">
          <code>{{ displayKey }}</code>
          <button class="btn-sm" @click="copyKey">复制</button>
        </div>
        <div class="modal-actions">
          <button class="btn btn-primary" @click="showKeyDialog = false">我已保存</button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.page { max-width: 1200px; }
.toolbar { display: flex; gap: 8px; margin-bottom: 16px; }
.table-wrap { background: #1e293b; border-radius: 10px; border: 1px solid rgba(255,255,255,0.06); overflow-x: auto; }
table { width: 100%; border-collapse: collapse; }
th { padding: 12px 16px; text-align: left; font-size: 12px; color: #64748b; font-weight: 600; text-transform: uppercase; border-bottom: 1px solid rgba(255,255,255,0.06); }
td { padding: 12px 16px; font-size: 14px; color: #cbd5e1; border-bottom: 1px solid rgba(255,255,255,0.04); }
tr:hover td { background: rgba(255,255,255,0.02); }
.empty { text-align: center; color: #64748b; padding: 40px !important; }
.icon-cell { font-size: 20px; }
code { padding: 2px 6px; background: rgba(255,255,255,0.06); border-radius: 3px; font-size: 13px; }
.link { color: #60a5fa; text-decoration: none; font-size: 13px; }
.link:hover { text-decoration: underline; }
.status { padding: 2px 8px; border-radius: 4px; font-size: 12px; }
.status.active { background: rgba(16,185,129,0.15); color: #34d399; }

.btn { padding: 8px 16px; border: 1px solid rgba(255,255,255,0.1); background: #1e293b; color: #cbd5e1; border-radius: 6px; cursor: pointer; font-size: 13px; }
.btn:hover { background: #334155; }
.btn-primary { background: #3b82f6; border-color: #3b82f6; color: white; }
.btn-primary:hover { background: #2563eb; }
.btn-sm { padding: 4px 10px; font-size: 12px; background: transparent; border: 1px solid rgba(255,255,255,0.1); color: #94a3b8; border-radius: 4px; cursor: pointer; margin-right: 4px; }
.btn-sm:hover { background: rgba(255,255,255,0.06); }
.btn-danger { color: #f87171; border-color: rgba(239,68,68,0.3); }
.btn-danger:hover { background: rgba(239,68,68,0.1); }

.modal-overlay { position: fixed; inset: 0; background: rgba(0,0,0,0.6); display: flex; align-items: center; justify-content: center; z-index: 1000; }
.modal { background: #1e293b; border: 1px solid rgba(255,255,255,0.1); border-radius: 12px; padding: 24px; min-width: 450px; }
.modal h3 { margin: 0 0 16px; font-size: 16px; color: #f1f5f9; }
.modal-actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 20px; }

.form-group { margin-bottom: 12px; }
.form-group label { display: block; font-size: 13px; color: #94a3b8; margin-bottom: 4px; }
.form-group input { width: 100%; padding: 8px 12px; background: #0f172a; border: 1px solid rgba(255,255,255,0.1); border-radius: 6px; color: #e2e8f0; font-size: 14px; outline: none; box-sizing: border-box; }
.form-group input:focus { border-color: #3b82f6; }

.key-warning { color: #fbbf24; font-size: 13px; margin: 0 0 12px; }
.key-box { display: flex; align-items: center; gap: 8px; padding: 12px; background: #0f172a; border-radius: 6px; border: 1px solid rgba(255,255,255,0.1); }
.key-box code { flex: 1; word-break: break-all; font-size: 13px; color: #34d399; background: transparent; padding: 0; }
</style>
