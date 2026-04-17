<template>
  <div class="mw-page">
    <div class="page-header">
      <h2>维护窗口管理</h2>
      <p class="page-desc">配置例行/临时/紧急维护窗口，告警统计和报告生成都会自动应用</p>
    </div>

    <div class="card table-card">
      <div class="card-top">
        <h3>维护窗口列表 <span class="row-count" v-if="maintenanceWindows.length">({{ maintenanceWindows.length }})</span></h3>
        <button class="btn btn-sm btn-primary" @click="newMW = defaultMW(); showAddMW = true">+ 添加维护窗口</button>
      </div>

      <!-- 添加/编辑表单 -->
      <div v-if="showAddMW" class="mw-form">
        <div class="mw-form-row">
          <div class="date-field">
            <label>项目名称</label>
            <input type="text" class="input" v-model="newMW.project" placeholder="如：G01" />
          </div>
          <div class="date-field" style="max-width:120px">
            <label>环境</label>
            <select class="input" v-model="newMW.environment">
              <option>PROD</option>
              <option>UAT</option>
            </select>
          </div>
          <div class="date-field" style="max-width:120px">
            <label>重复模式</label>
            <select class="input" v-model="newMW.repeat_mode">
              <option value="once">单次</option>
              <option value="weekly">每周</option>
            </select>
          </div>
          <div class="date-field" style="max-width:140px">
            <label>维护类型</label>
            <select class="input" v-model="newMW.maintenance_type">
              <option>例行维护</option>
              <option>临时维护</option>
              <option>紧急维护</option>
            </select>
          </div>
          <template v-if="newMW.repeat_mode === 'weekly'">
            <div class="date-field" style="max-width:100px">
              <label>星期</label>
              <select class="input" v-model="newMW.weekday">
                <option v-for="(d, i) in ['一','二','三','四','五','六','日']" :key="i" :value="i + 1">周{{ d }}</option>
              </select>
            </div>
            <div class="date-field" style="max-width:110px">
              <label>开始时间</label>
              <input type="time" class="input" v-model="newMW.time_start" />
            </div>
            <div class="date-field" style="max-width:110px">
              <label>结束时间</label>
              <input type="time" class="input" v-model="newMW.time_end" />
            </div>
          </template>
          <template v-else>
            <div class="date-field">
              <label>开始时间</label>
              <input type="datetime-local" class="input" v-model="newMW.start_time" />
            </div>
            <div class="date-field">
              <label>结束时间</label>
              <input type="datetime-local" class="input" v-model="newMW.end_time" />
            </div>
          </template>
          <div class="date-field" style="flex:1">
            <label>匹配规则名（可选，逗号分隔，不填匹配所有）</label>
            <input type="text" class="input" v-model="newMW.match_rules" placeholder="TcpPortDown, ApplicationInstanceDown" />
          </div>
        </div>
        <div class="date-field" style="margin-top:8px">
          <label>备注</label>
          <input type="text" class="input" v-model="newMW.note" placeholder="可选" />
        </div>
        <div class="mw-form-actions">
          <button class="btn btn-sm btn-primary" @click="saveMW">保存</button>
          <button class="btn btn-sm" @click="showAddMW = false">取消</button>
        </div>
      </div>

      <div class="table-wrapper" v-if="maintenanceWindows.length">
        <table>
          <thead>
            <tr>
              <th style="width:80px">项目</th>
              <th style="width:60px">环境</th>
              <th style="width:80px">重复</th>
              <th style="width:90px">维护类型</th>
              <th style="width:200px">时间</th>
              <th style="width:200px">匹配规则</th>
              <th>备注</th>
              <th style="width:80px"></th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="mw in maintenanceWindows" :key="mw.id">
              <td>{{ mw.project }}</td>
              <td>{{ mw.environment || 'PROD' }}</td>
              <td>{{ mw.repeat_mode === 'weekly' ? '每周' : '单次' }}</td>
              <td><span :class="['mw-tag', mw.maintenance_type === '紧急维护' ? 'urgent' : mw.maintenance_type === '临时维护' ? 'temp' : 'routine']">{{ mw.maintenance_type }}</span></td>
              <td>{{ formatMWTime(mw) }}</td>
              <td><span class="match-rules">{{ mw.match_rules || '全部' }}</span></td>
              <td>{{ mw.note || '-' }}</td>
              <td class="action-cell" style="white-space:nowrap">
                <button class="btn-icon" @click="editMW(mw)" title="编辑">&#9998;</button>
                <button class="btn-icon danger" @click="deleteMW(mw.id)" title="删除">&times;</button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <p v-else class="no-data">暂无维护窗口</p>
    </div>

    <!-- Toast -->
    <transition name="toast-fade">
      <div v-if="toast.show" :class="['toast', toast.type]" @click="toast.show = false">
        <span>{{ toast.message }}</span>
      </div>
    </transition>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import api from '@/api'

const maintenanceWindows = ref([])
const showAddMW = ref(false)

const defaultMW = () => ({ project: '', environment: 'PROD', repeat_mode: 'once', maintenance_type: '例行维护', weekday: 1, time_start: '02:00', time_end: '06:00', start_time: '', end_time: '', match_rules: '', operations: [], note: '' })
const newMW = ref(defaultMW())

const toast = ref({ show: false, message: '', type: 'error' })
let toastTimer = null
function showToast(message, type = 'error') {
  if (toastTimer) clearTimeout(toastTimer)
  toast.value = { show: true, message, type }
  toastTimer = setTimeout(() => { toast.value.show = false }, 4000)
}

async function loadMW() {
  try {
    const res = await api.get('/api/maintenance-windows')
    maintenanceWindows.value = res.data?.data || []
  } catch (e) { showToast('加载失败') }
}

async function saveMW() {
  if (!newMW.value.project) { showToast('请填写项目名称'); return }
  try {
    if (newMW.value.id) {
      await api.put(`/api/maintenance-windows/${newMW.value.id}`, newMW.value)
    } else {
      await api.post('/api/maintenance-windows', newMW.value)
    }
    newMW.value = defaultMW()
    showAddMW.value = false
    await loadMW()
    showToast('保存成功', 'success')
  } catch (e) {
    showToast('保存失败: ' + (e.response?.data?.error || e.message))
  }
}

function editMW(mw) {
  newMW.value = {
    id: mw.id,
    project: mw.project || '',
    environment: mw.environment || 'PROD',
    repeat_mode: mw.repeat_mode || 'once',
    maintenance_type: mw.maintenance_type || '例行维护',
    weekday: mw.weekday || 1,
    time_start: mw.time_start || '02:00',
    time_end: mw.time_end || '06:00',
    start_time: mw.start_time ? mw.start_time.replace(' ', 'T').slice(0, 16) : '',
    end_time: mw.end_time ? mw.end_time.replace(' ', 'T').slice(0, 16) : '',
    match_rules: mw.match_rules || '',
    operations: Array.isArray(mw.operations) ? [...mw.operations] : [],
    note: mw.note || '',
  }
  showAddMW.value = true
}

async function deleteMW(id) {
  if (!confirm('确认删除此维护窗口？')) return
  try {
    await api.delete(`/api/maintenance-windows/${id}`)
    await loadMW()
    showToast('已删除', 'success')
  } catch (e) {
    showToast('删除失败: ' + (e.response?.data?.error || e.message))
  }
}

const weekdayNames = ['', '周一', '周二', '周三', '周四', '周五', '周六', '周日']
function formatMWTime(mw) {
  if (mw.repeat_mode === 'weekly') {
    return `${weekdayNames[mw.weekday] || '周?'} ${mw.time_start || '??:??'} ~ ${mw.time_end || '??:??'}`
  }
  const fmtDT = (s) => {
    if (!s) return '?'
    const d = new Date(s)
    if (isNaN(d)) return s
    return `${String(d.getMonth()+1).padStart(2,'0')}/${String(d.getDate()).padStart(2,'0')} ${String(d.getHours()).padStart(2,'0')}:${String(d.getMinutes()).padStart(2,'0')}`
  }
  return `${fmtDT(mw.start_time)} ~ ${fmtDT(mw.end_time)}`
}

onMounted(() => loadMW())
</script>

<style scoped>
.mw-page { max-width: 1400px; margin: 0 auto; padding: 24px; }
.page-header { margin-bottom: 24px; }
.page-header h2 { font-size: 20px; font-weight: 700; color: var(--text-primary); margin: 0 0 4px; }
.page-desc { font-size: 13px; color: var(--text-muted); margin: 0; }

.card { background: var(--bg-secondary); border: 1px solid var(--border); border-radius: 12px; padding: 20px; margin-bottom: 16px; }
.card-top { display: flex; align-items: center; justify-content: space-between; margin-bottom: 16px; }
.card-top h3 { font-size: 15px; font-weight: 600; color: var(--text-primary); margin: 0; }
.row-count { font-size: 12px; color: var(--text-muted); font-weight: 400; margin-left: 4px; }

.input { padding: 8px 12px; background: var(--bg-input); border: 1px solid var(--border); border-radius: 8px; color: var(--text-primary); font-size: 13px; outline: none; transition: border-color 200ms; }
.input:focus { border-color: var(--primary-light); }

.btn { padding: 8px 16px; border: 1px solid var(--border); border-radius: 8px; background: var(--bg-secondary); color: var(--text-primary); cursor: pointer; font-size: 13px; }
.btn:hover { background: var(--bg-hover); }
.btn-sm { padding: 6px 12px; font-size: 12px; }
.btn-primary { background: var(--primary); color: white; border-color: var(--primary); }
.btn-primary:hover { opacity: 0.9; }

.mw-form { background: var(--bg-tertiary); border-radius: 8px; padding: 16px; margin-bottom: 16px; }
.mw-form-row { display: flex; gap: 12px; flex-wrap: wrap; }
.mw-form-actions { display: flex; gap: 8px; margin-top: 12px; }
.date-field { display: flex; flex-direction: column; gap: 4px; }
.date-field label { font-size: 12px; color: var(--text-muted); font-weight: 500; }

.table-wrapper { overflow-x: auto; }
table { width: 100%; border-collapse: collapse; font-size: 13px; }
th { text-align: left; padding: 10px 12px; background: var(--bg-tertiary); color: var(--text-secondary); font-weight: 600; font-size: 12px; text-transform: uppercase; letter-spacing: 0.03em; border-bottom: 1px solid var(--border); }
td { padding: 10px 12px; border-bottom: 1px solid var(--border-light, rgba(255,255,255,0.06)); }

.mw-tag { display: inline-block; padding: 2px 8px; border-radius: 4px; font-size: 11px; font-weight: 500; }
.mw-tag.routine { background: rgba(59,130,246,0.15); color: #3b82f6; }
.mw-tag.temp { background: rgba(168,85,247,0.15); color: #a855f7; }
.mw-tag.urgent { background: rgba(245,158,11,0.15); color: #f59e0b; }
.match-rules { font-size: 11px; font-family: var(--font-mono); color: var(--text-muted); }

.no-data { color: var(--text-muted); font-size: 13px; padding: 12px 0; text-align: center; }
.btn-icon { background: none; border: none; cursor: pointer; font-size: 16px; line-height: 1; padding: 4px 6px; border-radius: 4px; color: var(--text-muted); }
.btn-icon:hover { color: var(--primary-light); background: rgba(255,255,255,0.05); }
.btn-icon.danger:hover { color: #ef4444; background: rgba(239,68,68,0.1); }
.action-cell { text-align: center; }

.toast { position: fixed; top: 20px; right: 20px; z-index: 9999; padding: 12px 20px; border-radius: 8px; font-size: 13px; max-width: 420px; cursor: pointer; box-shadow: 0 4px 12px rgba(0,0,0,0.3); }
.toast.error { background: #991b1b; color: #fecaca; border: 1px solid #dc2626; }
.toast.success { background: #166534; color: #bbf7d0; border: 1px solid #22c55e; }
.toast-fade-enter-active, .toast-fade-leave-active { transition: all 0.3s ease; }
.toast-fade-enter-from, .toast-fade-leave-to { opacity: 0; transform: translateY(-10px); }
</style>
