<template>
  <div>
    <div class="page-header">
      <h1>触发规则</h1>
      <p>配置 Jira 工作流状态变更时触发锁定或解锁 Confluence 页面</p>
    </div>

    <div class="card">
      <div class="card-header">
        <span class="card-title">规则列表</span>
        <button class="btn btn-primary btn-sm" @click="openModal()">新增规则</button>
      </div>

      <div class="table-wrapper">
        <table>
          <thead>
            <tr>
              <th>规则名称</th>
              <th>动作</th>
              <th>Jira 项目</th>
              <th>工作流状态名</th>
              <th>启用</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="rule in rules" :key="rule.id">
              <td>{{ rule.rule_name }}</td>
              <td><span :class="['badge', 'badge-' + rule.action]">{{ rule.action === 'lock' ? '锁定' : '解锁' }}</span></td>
              <td>{{ rule.project_key || '所有项目' }}</td>
              <td><code style="background: var(--bg-primary); padding: 2px 8px; border-radius: 4px; font-size: 13px;">{{ rule.transition_name }}</code></td>
              <td>
                <label class="toggle">
                  <input type="checkbox" :checked="rule.enabled" @change="toggleEnabled(rule)">
                  <span class="toggle-slider"></span>
                </label>
              </td>
              <td>
                <button class="btn btn-secondary btn-sm" @click="openModal(rule)">编辑</button>
                <button class="btn btn-danger btn-sm" style="margin-left: 6px;" @click="handleDelete(rule)">删除</button>
              </td>
            </tr>
          </tbody>
        </table>
        <div v-if="rules.length === 0" class="empty-state">
          <p>暂无触发规则，点击"新增规则"开始配置</p>
        </div>
      </div>
    </div>

    <!-- 新增/编辑弹窗 -->
    <div v-if="showModal" class="modal-overlay" @click.self="showModal = false">
      <div class="modal">
        <div class="modal-header">
          <span class="modal-title">{{ editing ? '编辑规则' : '新增规则' }}</span>
          <button class="modal-close" @click="showModal = false">&times;</button>
        </div>
        <div class="form-group">
          <label class="form-label">规则名称</label>
          <input v-model="form.rule_name" class="form-input" placeholder="如: 研发HOD审批锁定" />
        </div>
        <div class="form-group">
          <label class="form-label">动作</label>
          <select v-model="form.action" class="form-select">
            <option value="lock">锁定 (lock)</option>
            <option value="unlock">解锁 (unlock)</option>
          </select>
        </div>
        <div class="form-group">
          <label class="form-label">Jira 项目 Key <span style="color: var(--text-muted);">(留空匹配所有项目)</span></label>
          <input v-model="form.project_key" class="form-input" placeholder="如: YX" />
        </div>
        <div class="form-group">
          <label class="form-label">工作流状态名 <span style="color: var(--danger);">*</span></label>
          <input v-model="form.transition_name" class="form-input" placeholder="Jira 工作流变更后的状态名，如: 研发HOD审批通过" />
        </div>
        <div class="form-group">
          <label class="toggle" style="display: inline-flex; align-items: center; gap: 8px;">
            <input type="checkbox" v-model="form.enabled">
            <span class="toggle-slider"></span>
            <span style="font-size: 13px; color: var(--text-secondary);">启用</span>
          </label>
        </div>
        <div class="modal-actions">
          <button class="btn btn-secondary" @click="showModal = false">取消</button>
          <button class="btn btn-primary" @click="handleSave" :disabled="saving">{{ saving ? '保存中...' : '保存' }}</button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted, inject } from 'vue'
import { getTriggers, createTrigger, updateTrigger, deleteTrigger } from '@/api'

const toast = inject('toast')
const rules = ref([])
const showModal = ref(false)
const editing = ref(null)
const saving = ref(false)
const form = ref({ rule_name: '', action: 'lock', project_key: '', transition_name: '', enabled: true })

async function loadRules() {
  try {
    const res = await getTriggers()
    rules.value = res.data
  } catch (e) {
    toast(e.message, 'error')
  }
}

function openModal(rule = null) {
  if (rule) {
    editing.value = rule.id
    form.value = { ...rule }
  } else {
    editing.value = null
    form.value = { rule_name: '', action: 'lock', project_key: '', transition_name: '', enabled: true }
  }
  showModal.value = true
}

async function handleSave() {
  if (!form.value.transition_name) {
    toast('工作流状态名不能为空', 'warning')
    return
  }
  saving.value = true
  try {
    if (editing.value) {
      await updateTrigger(editing.value, form.value)
      toast('规则已更新', 'success')
    } else {
      await createTrigger(form.value)
      toast('规则已创建', 'success')
    }
    showModal.value = false
    loadRules()
  } catch (e) {
    toast(e.message, 'error')
  } finally {
    saving.value = false
  }
}

async function handleDelete(rule) {
  if (!confirm(`确定删除规则 "${rule.rule_name}" 吗？`)) return
  try {
    await deleteTrigger(rule.id)
    toast('已删除', 'success')
    loadRules()
  } catch (e) {
    toast(e.message, 'error')
  }
}

async function toggleEnabled(rule) {
  try {
    await updateTrigger(rule.id, { ...rule, enabled: !rule.enabled })
    loadRules()
  } catch (e) {
    toast(e.message, 'error')
  }
}

onMounted(loadRules)
</script>
