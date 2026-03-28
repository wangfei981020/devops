<template>
  <div>
    <div class="card">
      <div class="card-header">
        <div class="card-title">ES 连接管理</div>
        <button class="btn btn-primary" @click="showModal = true; resetForm()">
          <Plus :size="16" /> 新增连接
        </button>
      </div>

      <div v-if="loading" class="loading"><div class="spinner"></div></div>

      <div v-else-if="list.length === 0" class="empty-state">
        <Database :size="48" style="opacity: 0.4;" />
        <p class="mt-4">暂无 ES 连接，请先添加</p>
      </div>

      <div v-else class="table-wrapper">
        <table>
          <thead>
            <tr>
              <th>状态</th>
              <th>名称</th>
              <th>地址</th>
              <th>版本</th>
              <th>用户名</th>
              <th>描述</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in list" :key="item.id">
              <td>
                <label class="switch">
                  <input type="checkbox" :checked="item.status === 1" @change="toggle(item)">
                  <span class="slider"></span>
                </label>
              </td>
              <td style="font-weight: 500;">{{ item.name }}</td>
              <td><span class="truncate" :title="item.url">{{ item.url }}</span></td>
              <td><span class="badge badge-info">{{ item.version }}.x</span></td>
              <td>{{ item.username || '-' }}</td>
              <td class="text-sm text-secondary">{{ item.description || '-' }}</td>
              <td>
                <div class="actions">
                  <button class="btn btn-sm btn-outline" @click="editItem(item)"><Pencil :size="14" /></button>
                  <button class="btn btn-sm btn-outline" style="color: var(--danger);" @click="deleteItem(item)"><Trash2 :size="14" /></button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <!-- Modal -->
    <div v-if="showModal" class="modal-overlay" @click.self="showModal = false">
      <div class="modal">
        <div class="modal-header">
          <div class="modal-title">{{ editId ? '编辑' : '新增' }} ES 连接</div>
          <button class="btn-icon" @click="showModal = false"><X :size="18" /></button>
        </div>
        <form @submit.prevent="handleSubmit">
          <div class="form-group">
            <label class="form-label">名称 *</label>
            <input v-model="form.name" class="form-input" placeholder="如: 生产环境 ES" required />
          </div>
          <div class="form-group">
            <label class="form-label">地址 *</label>
            <input v-model="form.url" class="form-input" placeholder="http://es-host:9200" required />
            <div class="form-hint">多个地址用逗号分隔</div>
          </div>
          <div class="form-row">
            <div class="form-group">
              <label class="form-label">版本</label>
              <select v-model="form.version" class="form-select">
                <option value="7">Elasticsearch 7.x</option>
                <option value="8">Elasticsearch 8.x</option>
              </select>
            </div>
            <div class="form-group">
              <label class="form-label">用户名</label>
              <input v-model="form.username" class="form-input" placeholder="elastic" />
            </div>
          </div>
          <div class="form-row">
            <div class="form-group">
              <label class="form-label">密码</label>
              <input v-model="form.password" type="password" class="form-input" />
            </div>
            <div class="form-group">
              <label class="form-label">API Key (ES 8.x)</label>
              <input v-model="form.api_key" class="form-input" placeholder="仅 ES 8.x 支持" />
            </div>
          </div>
          <div class="form-group">
            <label class="form-label">
              <input type="checkbox" v-model="form.skip_tls_verify" style="margin-right: 6px;" />
              跳过 TLS 证书验证
            </label>
            <div class="form-hint">ES 使用自签证书时勾选此项</div>
          </div>
          <div class="form-group">
            <label class="form-label">描述</label>
            <input v-model="form.description" class="form-input" />
          </div>
          <div class="modal-footer">
            <button type="button" class="btn btn-outline" @click="testConnection" :disabled="testing">
              {{ testing ? '测试中...' : '测试连接' }}
            </button>
            <button type="button" class="btn btn-outline" @click="showModal = false">取消</button>
            <button type="submit" class="btn btn-primary" :disabled="submitting">
              {{ submitting ? '保存中...' : '保存' }}
            </button>
          </div>
        </form>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import api from '../api'
import { useToast, useConfirm } from '../stores/ui'
import { Plus, Database, Pencil, Trash2, X } from 'lucide-vue-next'

const toast = useToast()
const dialog = useConfirm()

const list = ref([])
const loading = ref(false)
const showModal = ref(false)
const editId = ref(null)
const submitting = ref(false)
const testing = ref(false)

const form = ref({ name: '', url: '', version: '7', username: '', password: '', api_key: '', skip_tls_verify: false, description: '' })

function resetForm() {
  editId.value = null
  form.value = { name: '', url: '', version: '7', username: '', password: '', api_key: '', skip_tls_verify: false, description: '' }
}

async function loadList() {
  loading.value = true
  try {
    const res = await api.get('/es-connections')
    if (res.code === 0) list.value = res.data
  } catch (e) { /* ignore */ }
  loading.value = false
}

function editItem(item) {
  editId.value = item.id
  form.value = { name: item.name, url: item.url, version: item.version, username: item.username, password: '', api_key: '', skip_tls_verify: !!item.skip_tls_verify, description: item.description }
  showModal.value = true
}

async function handleSubmit() {
  submitting.value = true
  try {
    let res
    if (editId.value) {
      res = await api.put(`/es-connections/${editId.value}`, form.value)
    } else {
      res = await api.post('/es-connections', form.value)
    }
    if (res.code === 0) {
      showModal.value = false
      loadList()
    } else {
      toast.error(res.message)
    }
  } catch (e) {
    toast.error('保存失败: ' + (e.response?.data?.message || e.message))
  }
  submitting.value = false
}

async function deleteItem(item) {
  const ok = await dialog.danger({ title: '删除连接', message: `确认删除「${item.name}」？` })
  if (!ok) return
  try {
    const res = await api.delete(`/es-connections/${item.id}`)
    if (res.code === 0) loadList()
    else toast.error(res.message)
  } catch (e) {
    toast.error(e.response?.data?.message || '删除失败')
  }
}

async function toggle(item) {
  await api.put(`/es-connections/${item.id}/toggle`)
  item.status = item.status === 1 ? 0 : 1
}

async function testConnection() {
  testing.value = true
  try {
    const res = await api.post('/es-connections/test', form.value)
    if (res.code === 0) toast.success('连接成功!')
    else toast.error('连接失败: ' + res.message)
  } catch (e) {
    toast.error('连接失败: ' + (e.response?.data?.message || e.message))
  }
  testing.value = false
}

onMounted(loadList)
</script>
