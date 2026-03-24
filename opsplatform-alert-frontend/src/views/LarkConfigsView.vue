<template>
  <div>
    <div class="card">
      <div class="card-header">
        <div class="card-title">Lark / 飞书 配置</div>
        <button class="btn btn-primary" @click="showModal = true; resetForm()">
          <Plus :size="16" /> 新增配置
        </button>
      </div>

      <div v-if="loading" class="loading"><div class="spinner"></div></div>

      <div v-else-if="list.length === 0" class="empty-state">
        <Send :size="48" style="opacity: 0.4;" />
        <p class="mt-4">暂无 Lark 配置，请先添加</p>
      </div>

      <div v-else class="table-wrapper">
        <table>
          <thead>
            <tr>
              <th>状态</th>
              <th>名称</th>
              <th>Webhook URL</th>
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
              <td><span class="truncate" :title="item.webhook_url">{{ item.webhook_url }}</span></td>
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
          <div class="modal-title">{{ editId ? '编辑' : '新增' }} Lark 配置</div>
          <button class="btn-icon" @click="showModal = false"><X :size="18" /></button>
        </div>
        <form @submit.prevent="handleSubmit">
          <div class="form-group">
            <label class="form-label">名称 *</label>
            <input v-model="form.name" class="form-input" placeholder="如: G32 告警群" required />
          </div>
          <div class="form-group">
            <label class="form-label">Webhook URL *</label>
            <input v-model="form.webhook_url" class="form-input" placeholder="https://open.feishu.cn/open-apis/bot/v2/hook/xxx" required />
          </div>
          <div class="form-group">
            <label class="form-label">签名密钥 (可选)</label>
            <input v-model="form.secret" class="form-input" placeholder="启用签名验证时填写" />
          </div>
          <div class="form-group">
            <label class="form-label">描述</label>
            <input v-model="form.description" class="form-input" />
          </div>
          <div class="modal-footer">
            <button type="button" class="btn btn-outline" @click="testWebhook" :disabled="testing">
              {{ testing ? '测试中...' : '发送测试消息' }}
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
import { Plus, Send, Pencil, Trash2, X } from 'lucide-vue-next'

const toast = useToast()
const dialog = useConfirm()

const list = ref([])
const loading = ref(false)
const showModal = ref(false)
const editId = ref(null)
const submitting = ref(false)
const testing = ref(false)

const form = ref({ name: '', webhook_url: '', secret: '', lark_type: 'larksuite', description: '' })

function resetForm() {
  editId.value = null
  form.value = { name: '', webhook_url: '', secret: '', lark_type: 'larksuite', description: '' }
}

async function loadList() {
  loading.value = true
  try {
    const res = await api.get('/lark-configs')
    if (res.code === 0) list.value = res.data
  } catch (e) { /* ignore */ }
  loading.value = false
}

function editItem(item) {
  editId.value = item.id
  form.value = { name: item.name, webhook_url: item.webhook_url, secret: '', lark_type: item.lark_type, description: item.description }
  showModal.value = true
}

async function handleSubmit() {
  submitting.value = true
  try {
    let res
    if (editId.value) {
      res = await api.put(`/lark-configs/${editId.value}`, form.value)
    } else {
      res = await api.post('/lark-configs', form.value)
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
  const ok = await dialog.danger({ title: '删除配置', message: `确认删除「${item.name}」？` })
  if (!ok) return
  try {
    const res = await api.delete(`/lark-configs/${item.id}`)
    if (res.code === 0) loadList()
    else toast.error(res.message)
  } catch (e) {
    toast.error(e.response?.data?.message || '删除失败')
  }
}

async function toggle(item) {
  await api.put(`/lark-configs/${item.id}/toggle`)
  item.status = item.status === 1 ? 0 : 1
}

async function testWebhook() {
  testing.value = true
  try {
    const res = await api.post('/lark-configs/test', form.value)
    if (res.code === 0) toast.success('测试消息发送成功!')
    else toast.error('发送失败: ' + res.message)
  } catch (e) {
    toast.error('发送失败: ' + (e.response?.data?.message || e.message))
  }
  testing.value = false
}

onMounted(loadList)
</script>
