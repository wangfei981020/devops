<template>
  <div>
    <div class="page-header">
      <h1>编辑权限</h1>
      <p>配置锁定页面后，哪些用户或用户组保留编辑权限</p>
    </div>

    <div class="card">
      <div class="card-header">
        <span class="card-title">权限规则</span>
        <button class="btn btn-primary btn-sm" @click="openModal()">新增</button>
      </div>

      <div class="table-wrapper">
        <table>
          <thead>
            <tr>
              <th>类型</th>
              <th>值</th>
              <th>说明</th>
              <th>启用</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="rule in rules" :key="rule.id">
              <td>
                <span :class="['badge', rule.rule_type === 'user' ? 'badge-info' : 'badge-success']">
                  {{ rule.rule_type === 'user' ? '用户' : '用户组' }}
                </span>
              </td>
              <td><code style="background: var(--bg-primary); padding: 2px 8px; border-radius: 4px;">{{ rule.rule_value }}</code></td>
              <td style="color: var(--text-secondary);">{{ rule.description || '-' }}</td>
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
          <p>暂无权限规则。锁定页面时，这些用户/组将保留编辑权限。</p>
        </div>
      </div>
    </div>

    <!-- 弹窗 -->
    <div v-if="showModal" class="modal-overlay" @click.self="showModal = false">
      <div class="modal">
        <div class="modal-header">
          <span class="modal-title">{{ editing ? '编辑权限规则' : '新增权限规则' }}</span>
          <button class="modal-close" @click="showModal = false">&times;</button>
        </div>
        <div class="form-group">
          <label class="form-label">类型 <span style="color: var(--danger);">*</span></label>
          <select v-model="form.rule_type" class="form-select">
            <option value="user">用户 (user)</option>
            <option value="group">用户组 (group)</option>
          </select>
        </div>
        <div class="form-group">
          <label class="form-label">{{ form.rule_type === 'user' ? 'Confluence 用户名' : 'Confluence 组名' }} <span style="color: var(--danger);">*</span></label>
          <input v-model="form.rule_value" class="form-input" :placeholder="form.rule_type === 'user' ? '如: zhangsan' : '如: ops-team'" />
        </div>
        <div class="form-group">
          <label class="form-label">说明</label>
          <input v-model="form.description" class="form-input" placeholder="如: 运维负责人" />
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
import { getPermissions, createPermission, updatePermission, deletePermission } from '@/api'

const toast = inject('toast')
const rules = ref([])
const showModal = ref(false)
const editing = ref(null)
const saving = ref(false)
const form = ref({ rule_type: 'user', rule_value: '', description: '', enabled: true })

async function loadRules() {
  try {
    const res = await getPermissions()
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
    form.value = { rule_type: 'user', rule_value: '', description: '', enabled: true }
  }
  showModal.value = true
}

async function handleSave() {
  if (!form.value.rule_value) {
    toast('用户名或组名不能为空', 'warning')
    return
  }
  saving.value = true
  try {
    if (editing.value) {
      await updatePermission(editing.value, form.value)
      toast('已更新', 'success')
    } else {
      await createPermission(form.value)
      toast('已创建', 'success')
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
  if (!confirm(`确定删除 "${rule.rule_value}" 吗？`)) return
  try {
    await deletePermission(rule.id)
    toast('已删除', 'success')
    loadRules()
  } catch (e) {
    toast(e.message, 'error')
  }
}

async function toggleEnabled(rule) {
  try {
    await updatePermission(rule.id, { ...rule, enabled: !rule.enabled })
    loadRules()
  } catch (e) {
    toast(e.message, 'error')
  }
}

onMounted(loadRules)
</script>
