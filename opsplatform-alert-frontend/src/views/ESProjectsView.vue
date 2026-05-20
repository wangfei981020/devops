<template>
  <div>
    <div class="card">
      <div class="card-header">
        <div class="card-title">ES 项目分类</div>
        <div class="flex gap-2">
          <button class="btn btn-outline" @click="openDiscover">
            <Search :size="16" /> 从 ES 扫描
          </button>
          <button class="btn btn-primary" @click="openCreate">
            <Plus :size="16" /> 新增项目环境
          </button>
        </div>
      </div>

      <div v-if="loading" class="loading"><div class="spinner"></div></div>

      <div v-else-if="list.length === 0" class="empty-state">
        <Layers :size="48" style="opacity: 0.4;" />
        <p class="mt-4">暂无项目分类，点击右上角"从 ES 扫描"快速初始化</p>
      </div>

      <div v-else class="table-wrapper">
        <table>
          <thead>
            <tr>
              <th style="width: 80px;">排序</th>
              <th>唯一标识</th>
              <th>显示名</th>
              <th>关键词 (AND)</th>
              <th style="width: 80px;">启用</th>
              <th style="width: 140px;">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in list" :key="item.id">
              <td>{{ item.sort_order }}</td>
              <td><code>{{ item.code }}</code></td>
              <td>{{ item.display_name }}</td>
              <td class="text-sm text-secondary">{{ item.match_keywords }}</td>
              <td>
                <label class="switch">
                  <input type="checkbox" :checked="item.enabled === 1" @change="toggleEnabled(item)">
                  <span class="slider"></span>
                </label>
              </td>
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

    <!-- 新增/编辑 Modal -->
    <div v-if="showModal" class="modal-overlay" @click.self="showModal = false">
      <div class="modal">
        <div class="modal-header">
          <div class="modal-title">{{ editId ? '编辑' : '新增' }} 项目环境</div>
          <button class="btn-icon" @click="showModal = false"><X :size="18" /></button>
        </div>
        <form @submit.prevent="submit">
          <div class="form-group">
            <label class="form-label">唯一标识 (code) *</label>
            <input v-model="form.code" class="form-input" placeholder="如: g32-prod" required />
            <div class="form-hint">建议格式：项目代号-环境，如 g32-prod / ls-uat</div>
          </div>
          <div class="form-group">
            <label class="form-label">显示名 *</label>
            <input v-model="form.display_name" class="form-input" placeholder="如: G32 平台-生产" required />
          </div>
          <div class="form-group">
            <label class="form-label">匹配关键词 (AND) *</label>
            <input v-model="form.match_keywords" class="form-input" placeholder="如: g32,prod" required />
            <div class="form-hint">逗号分隔，索引名必须<strong>同时</strong>包含所有关键词才算匹配</div>
          </div>
          <div class="form-row">
            <div class="form-group">
              <label class="form-label">排序</label>
              <input v-model.number="form.sort_order" type="number" class="form-input" />
            </div>
            <div class="form-group">
              <label class="form-label">
                <input type="checkbox" v-model="form.enabled" :true-value="1" :false-value="0" style="margin-right: 6px;" />
                启用
              </label>
            </div>
          </div>
          <div class="modal-footer">
            <button type="button" class="btn btn-outline" @click="showModal = false">取消</button>
            <button type="submit" class="btn btn-primary" :disabled="submitting">{{ submitting ? '保存中...' : '保存' }}</button>
          </div>
        </form>
      </div>
    </div>

    <!-- 扫描发现 Modal -->
    <div v-if="showDiscover" class="modal-overlay" @click.self="showDiscover = false">
      <div class="modal" style="width: 720px; max-width: 90vw;">
        <div class="modal-header">
          <div class="modal-title">从 ES 扫描发现项目</div>
          <button class="btn-icon" @click="showDiscover = false"><X :size="18" /></button>
        </div>
        <div class="form-group">
          <label class="form-label">ES 连接</label>
          <div class="flex gap-2">
            <select v-model="discoverConnID" class="form-select" style="flex: 1;">
              <option :value="0">请选择</option>
              <option v-for="c in esConnections" :key="c.id" :value="c.id">{{ c.name }} ({{ c.version }}.x)</option>
            </select>
            <button class="btn btn-primary" @click="runDiscover" :disabled="!discoverConnID || scanning">
              {{ scanning ? '扫描中...' : '开始扫描' }}
            </button>
          </div>
        </div>

        <div v-if="candidates.length > 0" class="table-wrapper" style="margin-top: 12px; max-height: 400px; overflow-y: auto;">
          <table>
            <thead>
              <tr>
                <th style="width: 40px;"></th>
                <th>code</th>
                <th>关键词</th>
                <th>示例索引</th>
                <th style="width: 80px;">状态</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="c in candidates" :key="c.code">
                <td>
                  <input type="checkbox" v-model="selectedCodes" :value="c.code" :disabled="c.existing" />
                </td>
                <td><code>{{ c.code }}</code></td>
                <td class="text-sm text-secondary">{{ c.match_keywords }}</td>
                <td class="text-sm text-secondary">
                  <div v-for="(s, idx) in c.sample_indices" :key="idx" class="truncate" style="max-width: 320px;">{{ s }}</div>
                </td>
                <td>
                  <span v-if="c.existing" class="badge badge-info">已存在</span>
                  <span v-else class="badge">新</span>
                </td>
              </tr>
            </tbody>
          </table>
        </div>

        <div class="modal-footer">
          <button type="button" class="btn btn-outline" @click="showDiscover = false">取消</button>
          <button type="button" class="btn btn-primary" @click="batchCreate" :disabled="selectedCodes.length === 0 || importing">
            {{ importing ? '导入中...' : `批量导入 (${selectedCodes.length})` }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import api from '../api'
import { useToast, useConfirm } from '../stores/ui'
import { Plus, Pencil, Trash2, X, Layers, Search } from 'lucide-vue-next'

const toast = useToast()
const dialog = useConfirm()

const list = ref([])
const loading = ref(false)
const showModal = ref(false)
const editId = ref(null)
const submitting = ref(false)

const form = ref({ code: '', display_name: '', match_keywords: '', enabled: 1, sort_order: 0 })

const showDiscover = ref(false)
const discoverConnID = ref(0)
const esConnections = ref([])
const candidates = ref([])
const selectedCodes = ref([])
const scanning = ref(false)
const importing = ref(false)

function resetForm() {
  editId.value = null
  form.value = { code: '', display_name: '', match_keywords: '', enabled: 1, sort_order: 0 }
}

function openCreate() {
  resetForm()
  showModal.value = true
}

function editItem(item) {
  editId.value = item.id
  form.value = { code: item.code, display_name: item.display_name, match_keywords: item.match_keywords, enabled: item.enabled, sort_order: item.sort_order }
  showModal.value = true
}

async function loadList() {
  loading.value = true
  try {
    const res = await api.get('/es-projects')
    if (res.code === 0) list.value = res.data || []
  } catch (e) { toast.error('加载失败') }
  loading.value = false
}

async function submit() {
  submitting.value = true
  try {
    const res = editId.value
      ? await api.put(`/es-projects/${editId.value}`, form.value)
      : await api.post('/es-projects', form.value)
    if (res.code === 0) {
      showModal.value = false
      loadList()
    } else {
      toast.error(res.message)
    }
  } catch (e) {
    toast.error(e.response?.data?.message || '保存失败')
  }
  submitting.value = false
}

async function deleteItem(item) {
  const ok = await dialog.danger({ title: '删除项目环境', message: `确认删除「${item.code}」？` })
  if (!ok) return
  try {
    const res = await api.delete(`/es-projects/${item.id}`)
    if (res.code === 0) loadList()
    else toast.error(res.message)
  } catch (e) {
    toast.error('删除失败')
  }
}

async function toggleEnabled(item) {
  const newEnabled = item.enabled === 1 ? 0 : 1
  try {
    const res = await api.put(`/es-projects/${item.id}`, { ...item, enabled: newEnabled })
    if (res.code === 0) item.enabled = newEnabled
    else toast.error(res.message)
  } catch (e) { toast.error('切换失败') }
}

async function openDiscover() {
  showDiscover.value = true
  candidates.value = []
  selectedCodes.value = []
  try {
    const res = await api.get('/es-connections')
    if (res.code === 0) esConnections.value = res.data || []
  } catch (e) { /* ignore */ }
}

async function runDiscover() {
  scanning.value = true
  candidates.value = []
  selectedCodes.value = []
  try {
    const res = await api.post('/es-projects/discover', { es_connection_id: discoverConnID.value })
    if (res.code === 0) {
      candidates.value = res.data?.candidates || []
      selectedCodes.value = candidates.value.filter(c => !c.existing).map(c => c.code)
    } else {
      toast.error(res.message)
    }
  } catch (e) { toast.error('扫描失败') }
  scanning.value = false
}

async function batchCreate() {
  importing.value = true
  let ok = 0, fail = 0
  for (const code of selectedCodes.value) {
    const c = candidates.value.find(x => x.code === code)
    if (!c) continue
    try {
      const res = await api.post('/es-projects', {
        code: c.code,
        display_name: c.display_name,
        match_keywords: c.match_keywords,
        enabled: 1,
        sort_order: 0,
      })
      if (res.code === 0) ok++; else fail++
    } catch (e) { fail++ }
  }
  importing.value = false
  toast.success(`导入完成: 成功 ${ok}, 失败 ${fail}`)
  showDiscover.value = false
  loadList()
}

onMounted(loadList)
</script>
