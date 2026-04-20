<template>
  <div class="pc">
    <div class="list">
      <div class="list-head">
        <span class="title">项目环境 · <b class="mono" style="color:var(--primary);">{{ envs.length }}</b></span>
        <el-button type="primary" :icon="Plus" size="small" @click="onAdd">新增</el-button>
      </div>
      <el-input v-model="q" placeholder="搜索..." size="small" style="margin: 8px 14px; width: calc(100% - 28px);" />
      <div class="items">
        <div v-for="e in filtered" :key="e.id" :class="['item', { sel: selected?.id === e.id }]" @click="onPick(e)">
          <div class="item-top">
            <span class="mono" style="font-weight:600;">{{ e.name }}</span>
            <span :class="'env-chip ' + e.env_type">{{ e.env_type.toUpperCase() }}</span>
          </div>
          <div class="item-meta">
            <span>分支 <b class="mono">{{ e.git_branch }}</b></span>
            <span>auto-sync <b class="mono">{{ e.auto_sync ? 'ON' : 'OFF' }}</b></span>
          </div>
        </div>
        <div v-if="!filtered.length" class="item-empty">暂无</div>
      </div>
    </div>

    <div class="detail" v-if="selected">
      <div class="detail-head">
        <div>
          <div style="font-size:10px;text-transform:uppercase;color:var(--text-3);letter-spacing:1px;font-weight:600;">PROJECT ENV</div>
          <div style="font-weight:700;font-size:16px;font-family:var(--mono);">{{ form.name || '(新增)' }}</div>
        </div>
        <span v-if="form.env_type" :class="'env-chip ' + form.env_type">{{ form.env_type.toUpperCase() }}</span>
      </div>
      <div class="form">
        <el-form :model="form" label-position="top" size="default">
          <el-form-item label="唯一名称">
            <el-input v-model="form.name" :disabled="!!selected.id" class="mono" />
          </el-form-item>
          <el-form-item label="显示名">
            <el-input v-model="form.display_name" />
          </el-form-item>
          <el-form-item label="环境类型">
            <el-radio-group v-model="form.env_type" @change="onEnvTypeChange">
              <el-radio-button value="uat">UAT</el-radio-button>
              <el-radio-button value="prod">PROD</el-radio-button>
            </el-radio-group>
          </el-form-item>
          <el-form-item label="Git Repo URL">
            <el-input v-model="form.git_repo" class="mono" />
          </el-form-item>
          <el-form-item label="分支">
            <el-input v-model="form.git_branch" class="mono" style="width:240px" />
          </el-form-item>
          <el-form-item label="Chart 基础路径">
            <el-input v-model="form.chart_base_path" class="mono" />
          </el-form-item>
          <el-form-item label="K8s Namespace">
            <el-input v-model="form.namespace" class="mono" style="width:280px" />
          </el-form-item>
          <el-form-item label="ArgoCD URL">
            <el-input v-model="form.argocd_url" class="mono" />
          </el-form-item>
          <el-form-item label="ArgoCD Token">
            <el-input v-model="form.argocd_token" type="password" show-password placeholder="新建时必填；修改时留空则不变" />
          </el-form-item>
          <el-form-item label="Lark Webhook（可空）">
            <el-input v-model="form.lark_webhook" class="mono" />
          </el-form-item>
          <el-form-item label="Auto Sync">
            <el-switch v-model="form.auto_sync" :active-value="1" :inactive-value="0" :disabled="form.env_type === 'prod'" />
            <span style="color: var(--text-3); margin-left: 10px; font-size: 11px;">PROD 强制关闭</span>
          </el-form-item>
        </el-form>
      </div>
      <div class="detail-foot">
        <div class="left">
          <el-button @click="onTestGit" :loading="testing.git">测试 Git 连通</el-button>
          <el-button @click="onTestArgo" :loading="testing.argo">测试 ArgoCD 连通</el-button>
          <el-button v-if="selected.id" type="danger" plain @click="onDelete">删除</el-button>
        </div>
        <el-button type="primary" @click="onSave" :loading="saving">保存</el-button>
      </div>
    </div>
    <div v-else class="empty">选一个 project_env 查看 / 编辑，或点「新增」创建</div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, reactive } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus } from '@element-plus/icons-vue'
import {
  listProjectEnvs, createProjectEnv, updateProjectEnv, deleteProjectEnv,
  testProjectEnvGit, testProjectEnvArgocd
} from '../api'

const envs = ref([])
const q = ref('')
const selected = ref(null)
const blank = () => ({
  name: '', display_name: '', env_type: 'uat',
  git_repo: '', git_branch: 'main', chart_base_path: '',
  namespace: '', argocd_url: '', argocd_token: '', lark_webhook: '',
  auto_sync: 1
})
const form = reactive(blank())
const saving = ref(false)
const testing = reactive({ git: false, argo: false })

const filtered = computed(() => {
  const qq = q.value.trim().toLowerCase()
  return envs.value.filter(e =>
    !qq || e.name.toLowerCase().includes(qq) || (e.display_name || '').toLowerCase().includes(qq)
  )
})

async function load() { envs.value = (await listProjectEnvs()) || [] }
function onPick(e) {
  selected.value = e
  Object.assign(form, e, { argocd_token: '' })
}
function onAdd() {
  selected.value = { id: null }
  Object.assign(form, blank())
}
function onEnvTypeChange() {
  if (form.env_type === 'prod') form.auto_sync = 0
}
async function onSave() {
  saving.value = true
  try {
    if (selected.value?.id) {
      await updateProjectEnv(selected.value.id, form)
    } else {
      await createProjectEnv(form)
    }
    ElMessage.success('保存成功')
    await load()
  } finally { saving.value = false }
}
async function onDelete() {
  try { await ElMessageBox.confirm(`删除 ${selected.value.name}？`, '确认') } catch (_) { return }
  await deleteProjectEnv(selected.value.id)
  ElMessage.success('已删除')
  selected.value = null
  await load()
}
async function onTestGit() {
  if (!selected.value?.id) { ElMessage.warning('先保存再测试'); return }
  testing.git = true
  try { await testProjectEnvGit(selected.value.id); ElMessage.success('Git 连通 OK') }
  finally { testing.git = false }
}
async function onTestArgo() {
  if (!selected.value?.id) { ElMessage.warning('先保存再测试'); return }
  testing.argo = true
  try {
    const r = await testProjectEnvArgocd(selected.value.id)
    ElMessage.success(`ArgoCD OK (${r.version})`)
  } finally { testing.argo = false }
}

onMounted(load)
</script>

<style scoped>
.pc { display: grid; grid-template-columns: 340px 1fr; gap: 14px; height: calc(100vh - 120px); }
.list, .detail { background: #fff; border: 1px solid var(--border); border-radius: 8px; display: flex; flex-direction: column; overflow: hidden; }
.list-head { padding: 12px 14px; border-bottom: 1px solid var(--border-soft); display: flex; justify-content: space-between; align-items: center; }
.title { font-weight: 600; font-size: 13px; }
.items { flex: 1; overflow-y: auto; }
.item { padding: 12px 14px; border-bottom: 1px solid var(--border-soft); cursor: pointer; }
.item:hover { background: #fafbfc; }
.item.sel { background: #eff6ff; border-left: 3px solid var(--primary); padding-left: 11px; }
.item-top { display: flex; align-items: center; gap: 6px; }
.item-meta { font-size: 11px; color: var(--text-3); margin-top: 4px; display: flex; gap: 12px; }
.item-meta b { color: var(--text); }
.item-empty { padding: 40px; text-align: center; color: var(--text-3); font-size: 12px; }
.detail-head { padding: 14px 18px; border-bottom: 1px solid var(--border-soft); display: flex; justify-content: space-between; align-items: center; }
.form { flex: 1; overflow-y: auto; padding: 16px 22px; }
.detail-foot { padding: 14px 22px; border-top: 1px solid var(--border-soft); background: #fafbfc; display: flex; justify-content: space-between; }
.left { display: flex; gap: 8px; }
.empty { padding: 80px; text-align: center; color: var(--text-3); background: #fff; border: 1px solid var(--border); border-radius: 8px; }
</style>
