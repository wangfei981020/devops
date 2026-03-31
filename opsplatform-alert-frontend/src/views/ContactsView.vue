<template>
  <div>
    <div class="card">
      <div class="card-header">
        <div class="card-title">通知人管理</div>
        <div style="display: flex; gap: 8px;">
          <button class="btn btn-outline" @click="showBatchModal = true; batchText = ''">
            <Upload :size="16" /> 批量添加
          </button>
          <button class="btn btn-primary" @click="showModal = true; resetForm()">
            <UserPlus :size="16" /> 添加通知人
          </button>
        </div>
      </div>

      <div v-if="loading" class="loading"><div class="spinner"></div></div>

      <div v-else-if="list.length === 0" class="empty-state">
        <Users :size="48" style="opacity: 0.4;" />
        <p class="mt-4">暂无通知人，请先添加</p>
      </div>

      <div v-else class="table-wrapper">
        <table>
          <thead>
            <tr>
              <th>姓名</th>
              <th>Lark ID</th>
              <th>备注</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in list" :key="item.id">
              <td style="font-weight: 500;">{{ item.name }}</td>
              <td class="text-sm"><span class="truncate" :title="item.lark_id">{{ item.lark_id }}</span></td>
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

      <div class="card" style="margin-top: 16px; padding: 12px; background: #f8fafc;">
        <div class="text-sm text-secondary">
          <strong>使用说明：</strong>在告警规则的"@用户"字段填写姓名数组即可，如：<code>["Bruce","Cesar"]</code>
          <br>系统会自动根据姓名查找对应的 Lark ID 进行 @通知。
        </div>
      </div>
    </div>

    <!-- Modal -->
    <div v-if="showModal" class="modal-overlay" @click.self="showModal = false">
      <div class="modal">
        <div class="modal-header">
          <div class="modal-title">{{ editId ? '编辑' : '添加' }}通知人</div>
          <button class="btn-icon" @click="showModal = false"><X :size="18" /></button>
        </div>
        <form @submit.prevent="handleSubmit">
          <div class="form-row">
            <div class="form-group">
              <label class="form-label">姓名 *</label>
              <input v-model="form.name" class="form-input" placeholder="如: Bruce" required />
            </div>
            <div class="form-group">
              <label class="form-label">Lark ID *</label>
              <input v-model="form.lark_id" class="form-input" placeholder="ou_xxxxx" required />
              <div class="form-hint">飞书管理后台获取的 open_id 或 user_id</div>
            </div>
          </div>
          <div class="form-group">
            <label class="form-label">备注</label>
            <input v-model="form.description" class="form-input" />
          </div>
          <div class="modal-footer">
            <button type="button" class="btn btn-outline" @click="showModal = false">取消</button>
            <button type="submit" class="btn btn-primary" :disabled="submitting">
              {{ submitting ? '保存中...' : '保存' }}
            </button>
          </div>
        </form>
      </div>
    </div>
    <!-- Batch Modal -->
    <div v-if="showBatchModal" class="modal-overlay" @click.self="showBatchModal = false">
      <div class="modal" style="max-width: 560px;">
        <div class="modal-header">
          <div class="modal-title">批量添加通知人</div>
          <button class="btn-icon" @click="showBatchModal = false"><X :size="18" /></button>
        </div>
        <div style="padding: 0 24px;">
          <div class="form-hint" style="margin-bottom: 8px;">每行一条，格式：<code>姓名,Lark ID</code>，已存在的姓名会更新 Lark ID</div>
          <textarea v-model="batchText" class="form-input" rows="10" placeholder="Bruce,ou_xxxxx&#10;Cesar,ou_yyyyy&#10;Alice,ou_zzzzz" style="font-family: monospace; font-size: 13px;"></textarea>
          <div v-if="batchPreview.length > 0" style="margin-top: 8px; font-size: 13px; color: var(--text-secondary);">
            解析到 <strong>{{ batchPreview.length }}</strong> 条记录
          </div>
        </div>
        <div class="modal-footer">
          <button type="button" class="btn btn-outline" @click="showBatchModal = false">取消</button>
          <button type="button" class="btn btn-primary" :disabled="batchSubmitting || batchPreview.length === 0" @click="handleBatchSubmit">
            {{ batchSubmitting ? '导入中...' : `导入 ${batchPreview.length} 条` }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import api from '../api'
import { useToast, useConfirm } from '../stores/ui'
import { UserPlus, Users, Pencil, Trash2, X, Upload } from 'lucide-vue-next'

const toast = useToast()
const dialog = useConfirm()
const list = ref([])
const loading = ref(false)
const showModal = ref(false)
const editId = ref(null)
const submitting = ref(false)

const form = ref({ name: '', lark_id: '', phone: '', email: '', description: '' })
const showBatchModal = ref(false)
const batchText = ref('')
const batchSubmitting = ref(false)

const batchPreview = computed(() => {
  if (!batchText.value.trim()) return []
  return batchText.value.trim().split('\n')
    .map(line => line.trim())
    .filter(line => line && line.includes(','))
    .map(line => {
      const [name, lark_id] = line.split(',').map(s => s.trim())
      return { name, lark_id }
    })
    .filter(item => item.name && item.lark_id)
})

async function handleBatchSubmit() {
  if (batchPreview.value.length === 0) return
  batchSubmitting.value = true
  try {
    const res = await api.post('/alert-contacts/batch', { items: batchPreview.value })
    if (res.code === 0) {
      showBatchModal.value = false
      toast.success(`导入完成：新增 ${res.data.created} 条，跳过/更新 ${res.data.skipped} 条`)
      loadList()
    } else {
      toast.error(res.message)
    }
  } catch (e) {
    toast.error(e.response?.data?.message || '导入失败')
  }
  batchSubmitting.value = false
}

function resetForm() {
  editId.value = null
  form.value = { name: '', lark_id: '', phone: '', email: '', description: '' }
}

async function loadList() {
  loading.value = true
  try {
    const res = await api.get('/alert-contacts')
    if (res.code === 0) list.value = res.data
  } catch (e) { /* ignore */ }
  loading.value = false
}

function editItem(item) {
  editId.value = item.id
  form.value = { name: item.name, lark_id: item.lark_id, phone: item.phone, email: item.email, description: item.description }
  showModal.value = true
}

async function handleSubmit() {
  submitting.value = true
  try {
    let res
    if (editId.value) {
      res = await api.put(`/alert-contacts/${editId.value}`, form.value)
    } else {
      res = await api.post('/alert-contacts', form.value)
    }
    if (res.code === 0) { showModal.value = false; toast.success('保存成功'); loadList() }
    else toast.error(res.message)
  } catch (e) { toast.error(e.response?.data?.message || '操作失败') }
  submitting.value = false
}

async function deleteItem(item) {
  const ok = await dialog.danger({ title: '删除通知人', message: `确认删除「${item.name}」？` })
  if (!ok) return
  try {
    await api.delete(`/alert-contacts/${item.id}`)
    toast.success('已删除')
    loadList()
  } catch (e) { toast.error('删除失败') }
}

onMounted(loadList)
</script>
