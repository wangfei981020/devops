<script setup>
import { ref, onMounted, computed } from 'vue'
import api from '@/api'
import { useAppStore } from '@/stores/app'

const appStore = useAppStore()
const permissions = ref([])
const services = ref([])
const loading = ref(false)
const filterService = ref('')
const filterType = ref('')

// Create/Edit dialog
const showDialog = ref(false)
const editMode = ref(false)
const form = ref({ service_code: '', code: '', name: '', type: 'menu', parent_id: 0, icon: '', sort_order: 0, description: '' })

const permTypes = ['menu', 'button', 'data', 'api']

async function loadPermissions() {
  loading.value = true
  try {
    let url = '/api/admin/permissions'
    const params = []
    if (filterService.value) params.push(`service_code=${filterService.value}`)
    if (filterType.value) params.push(`type=${filterType.value}`)
    if (params.length) url += '?' + params.join('&')
    const res = await api.get(url)
    permissions.value = res.data?.data || []
  } catch (e) {
    appStore.showToast('加载失败', 'error')
  } finally {
    loading.value = false
  }
}

async function loadServices() {
  try {
    const res = await api.get('/api/admin/services')
    services.value = res.data?.data || []
  } catch (e) {}
}

// Build tree structure from flat list
const permTree = computed(() => {
  const map = {}
  const roots = []
  for (const p of permissions.value) {
    map[p.id] = { ...p, children: [] }
  }
  for (const p of permissions.value) {
    if (p.parent_id && map[p.parent_id]) {
      map[p.parent_id].children.push(map[p.id])
    } else {
      roots.push(map[p.id])
    }
  }
  // Sort by sort_order
  const sortFn = (a, b) => (a.sort_order || 0) - (b.sort_order || 0)
  roots.sort(sortFn)
  for (const r of Object.values(map)) {
    r.children.sort(sortFn)
  }
  return roots
})

// Group by service_code
const groupedTree = computed(() => {
  const groups = {}
  for (const p of permTree.value) {
    const svc = p.service_code || '未分组'
    if (!groups[svc]) groups[svc] = []
    groups[svc].push(p)
  }
  return groups
})

function openCreate(parentId = 0, serviceCode = '') {
  editMode.value = false
  form.value = { service_code: serviceCode, code: '', name: '', type: 'menu', parent_id: parentId, icon: '', sort_order: 0, description: '' }
  showDialog.value = true
}

function openEdit(perm) {
  editMode.value = true
  form.value = {
    id: perm.id, service_code: perm.service_code, code: perm.code,
    name: perm.name, type: perm.type, parent_id: perm.parent_id,
    icon: perm.icon, sort_order: perm.sort_order, description: perm.description
  }
  showDialog.value = true
}

async function savePerm() {
  try {
    if (editMode.value) {
      await api.put(`/api/admin/permissions/${form.value.id}`, form.value)
    } else {
      await api.post('/api/admin/permissions', form.value)
    }
    appStore.showToast('保存成功', 'success')
    showDialog.value = false
    loadPermissions()
  } catch (e) {
    appStore.showToast(e.response?.data?.message || '保存失败', 'error')
  }
}

async function deletePerm(perm) {
  if (!confirm(`确定删除权限 ${perm.name}？子权限也会被删除。`)) return
  try {
    await api.delete(`/api/admin/permissions/${perm.id}`)
    appStore.showToast('删除成功', 'success')
    loadPermissions()
  } catch (e) {
    appStore.showToast(e.response?.data?.message || '删除失败', 'error')
  }
}

function typeLabel(type) {
  const map = { menu: '菜单', button: '按钮', data: '数据', api: 'API' }
  return map[type] || type
}

function typeClass(type) {
  return 'type-' + type
}

// Expanded state for tree nodes
const expanded = ref({})
function toggleExpand(id) {
  expanded.value[id] = !expanded.value[id]
}

onMounted(() => {
  loadPermissions()
  loadServices()
})
</script>

<template>
  <div class="page">
    <div class="toolbar">
      <button class="btn btn-primary" @click="openCreate()">+ 添加权限</button>
      <select v-model="filterService" @change="loadPermissions" class="filter-select">
        <option value="">全部服务</option>
        <option v-for="s in services" :key="s.id" :value="s.code">{{ s.name }}</option>
      </select>
      <select v-model="filterType" @change="loadPermissions" class="filter-select">
        <option value="">全部类型</option>
        <option v-for="t in permTypes" :key="t" :value="t">{{ typeLabel(t) }}</option>
      </select>
    </div>

    <div class="table-wrap">
      <table>
        <thead>
          <tr>
            <th style="width:30%">名称</th>
            <th>代码</th>
            <th>类型</th>
            <th>服务</th>
            <th>图标</th>
            <th>排序</th>
            <th>操作</th>
          </tr>
        </thead>
        <tbody>
          <template v-for="(perms, svc) in groupedTree" :key="svc">
            <tr class="group-header">
              <td colspan="7">
                <strong>{{ svc }}</strong>
                <button class="btn-sm" style="margin-left:8px" @click="openCreate(0, svc)">+ 子权限</button>
              </td>
            </tr>
            <template v-for="perm in perms" :key="perm.id">
              <tr>
                <td>
                  <span v-if="perm.children.length" class="expand-btn" @click="toggleExpand(perm.id)">
                    {{ expanded[perm.id] ? '▼' : '▶' }}
                  </span>
                  <span v-else class="expand-placeholder"></span>
                  {{ perm.name }}
                </td>
                <td><code>{{ perm.code }}</code></td>
                <td><span class="type-tag" :class="typeClass(perm.type)">{{ typeLabel(perm.type) }}</span></td>
                <td>{{ perm.service_code }}</td>
                <td>{{ perm.icon || '-' }}</td>
                <td>{{ perm.sort_order }}</td>
                <td>
                  <button class="btn-sm" @click="openCreate(perm.id, perm.service_code)">+ 子级</button>
                  <button class="btn-sm" @click="openEdit(perm)">编辑</button>
                  <button class="btn-sm btn-danger" @click="deletePerm(perm)">删除</button>
                </td>
              </tr>
              <!-- Children (level 2) -->
              <template v-if="expanded[perm.id]">
                <tr v-for="child in perm.children" :key="child.id" class="child-row">
                  <td style="padding-left:40px">
                    <span class="expand-placeholder"></span>
                    {{ child.name }}
                  </td>
                  <td><code>{{ child.code }}</code></td>
                  <td><span class="type-tag" :class="typeClass(child.type)">{{ typeLabel(child.type) }}</span></td>
                  <td>{{ child.service_code }}</td>
                  <td>{{ child.icon || '-' }}</td>
                  <td>{{ child.sort_order }}</td>
                  <td>
                    <button class="btn-sm" @click="openEdit(child)">编辑</button>
                    <button class="btn-sm btn-danger" @click="deletePerm(child)">删除</button>
                  </td>
                </tr>
              </template>
            </template>
          </template>
          <tr v-if="permissions.length === 0">
            <td colspan="7" class="empty">{{ loading ? '加载中...' : '暂无数据' }}</td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- Create/Edit Dialog -->
    <div class="modal-overlay" v-if="showDialog" @click="showDialog = false">
      <div class="modal" @click.stop>
        <h3>{{ editMode ? '编辑权限' : '添加权限' }}</h3>
        <div class="form-group">
          <label>服务代码</label>
          <select v-model="form.service_code">
            <option value="">请选择</option>
            <option v-for="s in services" :key="s.id" :value="s.code">{{ s.name }} ({{ s.code }})</option>
          </select>
        </div>
        <div class="form-group" v-if="!editMode">
          <label>权限代码</label>
          <input v-model="form.code" placeholder="如 dashboard" />
        </div>
        <div class="form-group">
          <label>名称</label>
          <input v-model="form.name" placeholder="如 仪表盘" />
        </div>
        <div class="form-group">
          <label>类型</label>
          <select v-model="form.type">
            <option v-for="t in permTypes" :key="t" :value="t">{{ typeLabel(t) }}</option>
          </select>
        </div>
        <div class="form-group">
          <label>父级权限ID（0=顶级）</label>
          <input v-model.number="form.parent_id" type="number" />
        </div>
        <div class="form-group">
          <label>图标</label>
          <input v-model="form.icon" placeholder="如 📊" />
        </div>
        <div class="form-group">
          <label>排序</label>
          <input v-model.number="form.sort_order" type="number" />
        </div>
        <div class="form-group">
          <label>描述</label>
          <input v-model="form.description" placeholder="权限描述" />
        </div>
        <div class="modal-actions">
          <button class="btn" @click="showDialog = false">取消</button>
          <button class="btn btn-primary" @click="savePerm">保存</button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.page { max-width: 1200px; }
.toolbar { display: flex; gap: 8px; margin-bottom: 16px; align-items: center; }
.filter-select { padding: 8px 12px; background: #1e293b; border: 1px solid rgba(255,255,255,0.1); border-radius: 6px; color: #cbd5e1; font-size: 13px; outline: none; }
.table-wrap { background: #1e293b; border-radius: 10px; border: 1px solid rgba(255,255,255,0.06); overflow-x: auto; }
table { width: 100%; border-collapse: collapse; }
th { padding: 12px 16px; text-align: left; font-size: 12px; color: #64748b; font-weight: 600; text-transform: uppercase; border-bottom: 1px solid rgba(255,255,255,0.06); }
td { padding: 10px 16px; font-size: 14px; color: #cbd5e1; border-bottom: 1px solid rgba(255,255,255,0.04); }
tr:hover td { background: rgba(255,255,255,0.02); }
.empty { text-align: center; color: #64748b; padding: 40px !important; }
code { padding: 2px 6px; background: rgba(255,255,255,0.06); border-radius: 3px; font-size: 13px; }

.group-header td { background: rgba(59,130,246,0.06); color: #3b82f6; font-size: 13px; padding: 8px 16px; }
.child-row td { background: rgba(255,255,255,0.01); }

.expand-btn { cursor: pointer; margin-right: 6px; font-size: 10px; color: #64748b; user-select: none; }
.expand-placeholder { display: inline-block; width: 16px; }

.type-tag { padding: 2px 8px; border-radius: 4px; font-size: 12px; }
.type-menu { background: rgba(59,130,246,0.15); color: #60a5fa; }
.type-button { background: rgba(168,85,247,0.15); color: #c084fc; }
.type-data { background: rgba(245,158,11,0.15); color: #fbbf24; }
.type-api { background: rgba(16,185,129,0.15); color: #34d399; }

.btn { padding: 8px 16px; border: 1px solid rgba(255,255,255,0.1); background: #1e293b; color: #cbd5e1; border-radius: 6px; cursor: pointer; font-size: 13px; }
.btn:hover { background: #334155; }
.btn-primary { background: #3b82f6; border-color: #3b82f6; color: white; }
.btn-primary:hover { background: #2563eb; }
.btn-sm { padding: 4px 10px; font-size: 12px; background: transparent; border: 1px solid rgba(255,255,255,0.1); color: #94a3b8; border-radius: 4px; cursor: pointer; margin-right: 4px; }
.btn-sm:hover { background: rgba(255,255,255,0.06); }
.btn-danger { color: #f87171; border-color: rgba(239,68,68,0.3); }
.btn-danger:hover { background: rgba(239,68,68,0.1); }

.modal-overlay { position: fixed; inset: 0; background: rgba(0,0,0,0.6); display: flex; align-items: center; justify-content: center; z-index: 1000; }
.modal { background: #1e293b; border: 1px solid rgba(255,255,255,0.1); border-radius: 12px; padding: 24px; min-width: 450px; max-height: 80vh; overflow-y: auto; }
.modal h3 { margin: 0 0 16px; font-size: 16px; color: #f1f5f9; }
.modal-actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 20px; }

.form-group { margin-bottom: 12px; }
.form-group label { display: block; font-size: 13px; color: #94a3b8; margin-bottom: 4px; }
.form-group input, .form-group select { width: 100%; padding: 8px 12px; background: #0f172a; border: 1px solid rgba(255,255,255,0.1); border-radius: 6px; color: #e2e8f0; font-size: 14px; outline: none; box-sizing: border-box; }
.form-group input:focus, .form-group select:focus { border-color: #3b82f6; }
</style>
