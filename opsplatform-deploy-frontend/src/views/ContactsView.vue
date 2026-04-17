<template>
  <div>
    <div class="kpi-bar">
      <div class="kpi kpi-blue">
        <div class="kpi-icon"><UserPlus :size="18" /></div>
        <div class="kpi-body">
          <div class="kpi-label">通知人总数</div>
          <div class="kpi-value">{{ list.length }}</div>
          <div class="kpi-foot">{{ enabledCount }} 启用 · {{ list.length - enabledCount }} 禁用</div>
        </div>
      </div>
      <div class="kpi kpi-green">
        <div class="kpi-icon"><Check :size="18" /></div>
        <div class="kpi-body">
          <div class="kpi-label">启用</div>
          <div class="kpi-value">{{ enabledCount }}</div>
          <div class="kpi-foot">可接收 Lark @ 通知</div>
        </div>
      </div>
      <div class="kpi kpi-gray" style="background: white; border: 1px solid #e2e8f0">
        <div class="kpi-icon" style="background: #f1f5f9; color: #64748b"><Info :size="18" /></div>
        <div class="kpi-body">
          <div class="kpi-label">用途</div>
          <div class="kpi-value" style="font-size: 14px; font-weight: 500">Lark 发布通知</div>
          <div class="kpi-foot">模块发布成功/失败时 @</div>
        </div>
      </div>
    </div>

    <div class="toolbar">
      <div class="toolbar-left">
        <span class="toolbar-title">通知人</span>
        <div class="search-input">
          <Search :size="13" />
          <input v-model="keyword" placeholder="搜索姓名..." />
        </div>
      </div>
      <div class="toolbar-right">
        <button class="btn btn-primary" @click="openCreate"><Plus :size="13" /> 新增通知人</button>
      </div>
    </div>

    <div class="card" style="padding: 0; overflow: hidden">
      <GhostEmpty v-if="!filteredList.length"
        :headers="[{label:'ID',width:'60px'},{label:'姓名',width:'180px'},{label:'Lark Open ID'},{label:'备注'},{label:'状态',width:'100px'},{label:'创建时间',width:'120px'},{label:'操作',width:'180px'}]"
        :icon="UserPlus"
        :title="keyword ? '没有匹配的通知人' : '暂无通知人'"
        description="添加通知人后可在模块发布时 @ 该用户"
        cta-label="新增通知人"
        @cta="openCreate" />
      <table v-else class="table">
        <thead>
          <tr>
            <th style="width: 60px">ID</th>
            <th style="width: 180px">姓名</th>
            <th>Lark Open ID</th>
            <th>备注</th>
            <th style="width: 100px">状态</th>
            <th style="width: 120px">创建时间</th>
            <th style="width: 180px; text-align: right">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="c in filteredList" :key="c.id">
            <td class="mono text-muted text-xs">#{{ c.id }}</td>
            <td>
              <div class="contact-cell">
                <div class="contact-avatar" :style="{ background: avatarColor(c.name) }">{{ (c.name || 'U').charAt(0) }}</div>
                <strong>{{ c.name }}</strong>
              </div>
            </td>
            <td><code class="mono text-xs" style="color: #475569">{{ c.lark_id }}</code></td>
            <td class="text-sm">{{ c.remark || '—' }}</td>
            <td>
              <span class="chip" :class="c.status ? 'chip-green' : 'chip-gray'">
                <span class="dot" :class="c.status ? 'dot-success' : 'dot-gray'"></span>
                {{ c.status ? '启用' : '禁用' }}
              </span>
            </td>
            <td class="text-xs text-gray mono">{{ formatTime(c.created_at) }}</td>
            <td style="text-align: right">
              <div class="actions" style="justify-content: flex-end">
                <button class="btn btn-sm btn-outline" @click="openEdit(c)">编辑</button>
                <button class="btn btn-sm btn-danger-light" @click="onDelete(c)">删除</button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <div v-if="dialogOpen" class="dialog-mask" @click.self="dialogOpen=false">
      <div class="dialog">
        <div class="dialog-title">{{ form.id ? '编辑通知人' : '新增通知人' }}</div>
        <div class="dialog-content">
          <div class="form-group">
            <label class="form-label">姓名 <span class="text-danger">*</span></label>
            <input v-model="form.name" class="form-input" placeholder="张三" />
          </div>
          <div class="form-group">
            <label class="form-label">Lark Open ID <span class="text-danger">*</span></label>
            <input v-model="form.lark_id" class="form-input" placeholder="ou_xxxxxxxxxxxxxxxx" />
            <div class="form-help">用于消息 @ 该用户</div>
          </div>
          <div class="form-group">
            <label class="form-label">备注</label>
            <textarea v-model="form.remark" class="form-textarea" rows="2"></textarea>
          </div>
          <div v-if="form.id" class="form-group">
            <label class="form-label flex items-center gap-2">
              <input type="checkbox" v-model="form.status" :true-value="1" :false-value="0" />
              <span>启用</span>
            </label>
          </div>
        </div>
        <div class="dialog-actions">
          <button class="btn btn-outline" @click="dialogOpen=false">取消</button>
          <button class="btn btn-primary" @click="onSave">保存</button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted, computed } from 'vue'
import { Plus, UserPlus, Check, Info, Search } from 'lucide-vue-next'
import { contactsApi } from '../api'
import { success, error, confirm } from '../stores/ui'
import GhostEmpty from '../components/GhostEmpty.vue'

const list = ref([])
const dialogOpen = ref(false)
const keyword = ref('')
const form = ref({ id: null, name: '', lark_id: '', remark: '', status: 1 })

const enabledCount = computed(() => list.value.filter(c => c.status).length)
const filteredList = computed(() => {
  if (!keyword.value) return list.value
  const k = keyword.value.toLowerCase()
  return list.value.filter(c => c.name.toLowerCase().includes(k))
})

function avatarColor(name) {
  const colors = ['#1e40af', '#6d28d9', '#db2777', '#b45309', '#15803d', '#0e7490', '#4338ca', '#b91c1c']
  let hash = 0
  for (let i = 0; i < (name || 'U').length; i++) hash = name.charCodeAt(i) + ((hash << 5) - hash)
  return colors[Math.abs(hash) % colors.length]
}
function formatTime(t) { return t ? t.slice(0, 16).replace('T', ' ') : '-' }

async function load() {
  try { list.value = (await contactsApi.list()).data || [] }
  catch (e) { error('加载失败: ' + e.message) }
}

function openCreate() { form.value = { id: null, name: '', lark_id: '', remark: '', status: 1 }; dialogOpen.value = true }
function openEdit(c) { form.value = { ...c }; dialogOpen.value = true }

async function onSave() {
  if (!form.value.name || !form.value.lark_id) return error('姓名和 Lark ID 必填')
  try {
    if (form.value.id) await contactsApi.update(form.value.id, form.value)
    else await contactsApi.create(form.value)
    success('保存成功'); dialogOpen.value = false; load()
  } catch (e) { error('保存失败: ' + (e.response?.data?.message || e.message)) }
}

async function onDelete(c) {
  if (!await confirm({ title: '删除通知人', message: `删除 "${c.name}"?`, danger: true })) return
  try { await contactsApi.delete(c.id); success('已删除'); load() }
  catch (e) { error(e.message) }
}

onMounted(load)
</script>

<style scoped>
.contact-cell { display: flex; align-items: center; gap: 8px; }
.contact-avatar {
  width: 24px; height: 24px; border-radius: 50%;
  display: flex; align-items: center; justify-content: center;
  color: white; font-weight: 700; font-size: 11px;
  flex-shrink: 0;
}
</style>
