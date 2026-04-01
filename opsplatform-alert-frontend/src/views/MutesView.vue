<template>
  <div>
    <div class="card">
      <div class="card-header">
        <div class="card-title">屏蔽管理</div>
        <div class="flex gap-2 items-center">
          <select v-model="filterRuleId" class="form-select" style="width: 220px;" @change="loadMutes">
            <option value="">全部规则</option>
            <option v-for="r in ruleOptions" :key="r.id" :value="r.id">{{ r.name }}</option>
          </select>
        </div>
      </div>

      <!-- Add Mute -->
      <div class="flex gap-2 items-center" style="padding: 0 16px 12px;">
        <div style="position: relative; flex: 0 0 220px;">
          <input v-model="ruleSearch" class="form-input" placeholder="搜索并选择规则..." @focus="showRuleDropdown = true" @input="showRuleDropdown = true" />
          <div v-if="showRuleDropdown && filteredRuleOptions.length > 0" class="search-dropdown">
            <div v-for="r in filteredRuleOptions" :key="r.id" class="dropdown-item" @mousedown.prevent="selectRule(r)">
              {{ r.name }}
            </div>
          </div>
        </div>
        <input v-model="newGroupKey" class="form-input" style="width: 200px;" placeholder="容器名" />
        <select v-model="newDuration" class="form-select" style="width: 120px;">
          <option value="1h">1 小时</option>
          <option value="3h">3 小时</option>
          <option value="6h">6 小时</option>
          <option value="12h">12 小时</option>
          <option value="24h">24 小时</option>
          <option value="7d">7 天</option>
          <option value="30d">30 天</option>
          <option value="forever">1 年</option>
        </select>
        <button class="btn btn-primary btn-sm" @click="addMute" :disabled="!newRuleId || !newGroupKey">添加屏蔽</button>
      </div>

      <div v-if="loading" class="loading"><div class="spinner"></div></div>

      <div v-else-if="mutes.length === 0" class="empty-state" style="padding: 40px;">
        <ShieldOff :size="48" style="opacity: 0.4;" />
        <p>暂无屏蔽记录</p>
      </div>

      <div v-else class="table-wrapper">
        <table>
          <thead>
            <tr>
              <th>容器名</th>
              <th>所属规则</th>
              <th>屏蔽截止</th>
              <th>剩余时间</th>
              <th>操作人</th>
              <th>原因</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="m in mutes" :key="m.id">
              <td style="font-weight: 500;">{{ m.group_key }}</td>
              <td class="text-sm">{{ m.rule_name }}</td>
              <td class="text-sm">{{ formatTime(m.mute_until) }}</td>
              <td class="text-sm"><span class="badge badge-warning">{{ remainTime(m.mute_until) }}</span></td>
              <td class="text-sm">{{ m.created_by || '-' }}</td>
              <td class="text-sm text-secondary">{{ m.reason || '-' }}</td>
              <td>
                <div class="actions">
                  <button class="btn btn-sm btn-outline" @click="openEditMute(m)">修改时长</button>
                  <button class="btn btn-sm btn-outline" style="color: var(--danger);" @click="removeMute(m)">取消屏蔽</button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <!-- Edit Mute Modal -->
    <div v-if="editMute" class="modal-overlay" @click.self="editMute = null">
      <div class="modal" style="min-width: 380px;">
        <div class="modal-header">
          <div class="modal-title">修改屏蔽时长</div>
          <button class="btn-icon" @click="editMute = null">&times;</button>
        </div>
        <div style="padding: 0 24px;">
          <div class="text-sm" style="margin-bottom: 12px;">
            容器: <strong>{{ editMute.group_key }}</strong><br>
            规则: {{ editMute.rule_name }}
          </div>
          <div class="form-group">
            <label class="form-label">新的屏蔽时长</label>
            <select v-model="editDuration" class="form-select">
              <option value="1h">1 小时</option>
              <option value="3h">3 小时</option>
              <option value="6h">6 小时</option>
              <option value="12h">12 小时</option>
              <option value="24h">24 小时</option>
              <option value="7d">7 天</option>
              <option value="30d">30 天</option>
              <option value="forever">1 年</option>
            </select>
          </div>
        </div>
        <div class="modal-footer">
          <button class="btn btn-outline" @click="editMute = null">取消</button>
          <button class="btn btn-primary" @click="saveEditMute">确认修改</button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import api from '../api'
import { useToast, useConfirm } from '../stores/ui'
import { ShieldOff } from 'lucide-vue-next'

const toast = useToast()
const dialog = useConfirm()

const mutes = ref([])
const loading = ref(false)
const filterRuleId = ref('')

// Rule options for dropdown
const ruleOptions = ref([])
const ruleSearch = ref('')
const showRuleDropdown = ref(false)
const newRuleId = ref(0)
const newGroupKey = ref('')
const newDuration = ref('1h')

const filteredRuleOptions = computed(() => {
  if (!ruleSearch.value) return ruleOptions.value
  const s = ruleSearch.value.toLowerCase()
  return ruleOptions.value.filter(r => r.name.toLowerCase().includes(s))
})

function selectRule(r) {
  newRuleId.value = r.id
  ruleSearch.value = r.name
  showRuleDropdown.value = false
}

async function loadMutes() {
  loading.value = true
  try {
    const params = {}
    if (filterRuleId.value) params.rule_id = filterRuleId.value
    const res = await api.get('/alert-mutes', { params })
    if (res.code === 0) mutes.value = res.data
  } catch (e) { /* ignore */ }
  loading.value = false
}

async function loadRules() {
  try {
    const res = await api.get('/alert-rules', { params: { limit: 500 } })
    if (res.code === 0) ruleOptions.value = res.data.map(r => ({ id: r.id, name: r.name }))
  } catch (e) { /* ignore */ }
}

async function addMute() {
  if (!newRuleId.value || !newGroupKey.value) return
  try {
    const res = await api.post('/alert-mutes', {
      rule_id: newRuleId.value,
      group_key: newGroupKey.value,
      duration: newDuration.value,
      reason: '屏蔽管理页添加'
    })
    if (res.code === 0) {
      toast.success(`${newGroupKey.value} 已屏蔽`)
      newGroupKey.value = ''
      loadMutes()
    } else {
      toast.error(res.message)
    }
  } catch (e) {
    toast.error('屏蔽失败')
  }
}

const editMute = ref(null)
const editDuration = ref('1h')

function openEditMute(m) {
  editMute.value = m
  editDuration.value = '1h'
}

async function saveEditMute() {
  if (!editMute.value) return
  try {
    const res = await api.post('/alert-mutes', {
      rule_id: editMute.value.rule_id,
      group_key: editMute.value.group_key,
      duration: editDuration.value,
      reason: '屏蔽管理页修改时长'
    })
    if (res.code === 0) {
      toast.success(`${editMute.value.group_key} 屏蔽时长已修改`)
      editMute.value = null
      loadMutes()
    } else {
      toast.error(res.message)
    }
  } catch (e) {
    toast.error('修改失败')
  }
}

async function removeMute(m) {
  const ok = await dialog.confirm({ title: '取消屏蔽', message: `确认取消屏蔽「${m.group_key}」？` })
  if (!ok) return
  try {
    await api.delete(`/alert-mutes/${m.id}`)
    toast.success('已取消屏蔽')
    loadMutes()
  } catch (e) {
    toast.error('操作失败')
  }
}

function formatTime(t) {
  if (!t) return '-'
  return new Date(t).toLocaleString('zh-CN')
}

function remainTime(t) {
  if (!t) return '-'
  const diff = new Date(t) - new Date()
  if (diff <= 0) return '已过期'
  const days = Math.floor(diff / 86400000)
  const hours = Math.floor((diff % 86400000) / 3600000)
  if (days > 30) return `${Math.floor(days / 30)}个月`
  if (days > 0) return `${days}天${hours}小时`
  const mins = Math.floor((diff % 3600000) / 60000)
  if (hours > 0) return `${hours}小时${mins}分`
  return `${mins}分钟`
}

onMounted(() => {
  loadRules()
  loadMutes()
})
</script>

<style scoped>
.search-dropdown {
  position: absolute;
  top: 100%;
  left: 0;
  right: 0;
  background: var(--bg-card, #fff);
  border: 1px solid var(--border, #e2e8f0);
  border-radius: 6px;
  box-shadow: 0 4px 12px rgba(0,0,0,0.1);
  max-height: 200px;
  overflow-y: auto;
  z-index: 100;
}
.dropdown-item {
  padding: 8px 12px;
  cursor: pointer;
  font-size: 13px;
}
.dropdown-item:hover {
  background: var(--bg-hover, #f1f5f9);
}
</style>
