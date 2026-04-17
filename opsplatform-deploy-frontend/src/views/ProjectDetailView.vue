<template>
  <div v-if="project" class="card">
    <div class="card-header">
      <div>
        <div class="card-title">{{ project.display_name || project.name }} <span class="text-gray text-sm">({{ project.name }})</span></div>
        <div class="text-sm text-gray" style="margin-top:4px">{{ project.description || '暂无描述' }}</div>
      </div>
      <button class="btn btn-primary btn-sm" @click="openCreateEnv"><Plus :size="14" /> 配置环境</button>
    </div>

    <div v-if="!projectEnvs.length" class="empty">
      还未配置任何环境, 请先点击 "配置环境"
    </div>

    <div v-for="pe in projectEnvs" :key="pe.id" class="env-section">
      <div class="env-header">
        <div>
          <span class="badge badge-info" style="font-size:13px">{{ pe.env_name }}</span>
          <span class="text-sm" style="margin-left:10px">命名空间: <code>{{ pe.namespace }}</code></span>
          <span class="text-sm text-gray" style="margin-left:10px">Git: {{ pe.git_repo }} ({{ pe.git_base_path }})</span>
        </div>
        <div class="actions">
          <button class="btn btn-sm" @click="goCreateModule(pe)"><Plus :size="13" /> 新建模块</button>
          <button class="btn btn-sm" @click="openEditEnv(pe)">编辑环境</button>
        </div>
      </div>

      <table class="table">
        <thead><tr><th>模块名</th><th>模板</th><th>镜像 / Tag</th><th>副本</th><th>状态</th><th>操作</th></tr></thead>
        <tbody>
          <tr v-for="m in modulesByPe[pe.id] || []" :key="m.id">
            <td><router-link :to="`/modules/${m.id}`" style="color:#2563eb">{{ m.name }}</router-link></td>
            <td>{{ m.template_name || m.template_id }}</td>
            <td class="text-sm">
              <div>{{ m.image_repo }}</div>
              <div class="text-gray">{{ m.current_tag || '-' }}</div>
            </td>
            <td>{{ m.replicas }}</td>
            <td><span class="badge" :class="statusBadge(m.status)">{{ m.status }}</span></td>
            <td class="actions">
              <button class="btn btn-sm" @click="$router.push(`/modules/${m.id}`)">详情</button>
              <button class="btn btn-sm" @click="$router.push(`/modules/${m.id}/edit`)">编辑</button>
            </td>
          </tr>
          <tr v-if="!(modulesByPe[pe.id] || []).length"><td colspan="6" class="empty" style="padding:24px">该环境暂无模块</td></tr>
        </tbody>
      </table>
    </div>
  </div>

  <div v-if="envDialogOpen" class="dialog-mask" @click.self="envDialogOpen=false">
    <div class="dialog">
      <div class="dialog-title">{{ envForm.id ? '编辑项目环境' : '新增项目环境' }}</div>
      <div class="dialog-content">
        <div class="form-group">
          <label class="form-label">环境 <span style="color:#ef4444">*</span></label>
          <select v-model.number="envForm.env_id" :disabled="!!envForm.id" class="form-select">
            <option :value="0">请选择</option>
            <option v-for="e in environments" :key="e.id" :value="e.id">{{ e.display_name || e.name }}</option>
          </select>
        </div>
        <div class="form-group">
          <label class="form-label">Git 仓库 <span style="color:#ef4444">*</span></label>
          <input v-model="envForm.git_repo" class="form-input" placeholder="http://gitlab.xx/ops/UAT-K8S-PLATFORM.git" />
        </div>
        <div class="grid">
          <div class="form-group">
            <label class="form-label">分支</label>
            <input v-model="envForm.git_branch" class="form-input" placeholder="main" />
          </div>
          <div class="form-group">
            <label class="form-label">基路径</label>
            <input v-model="envForm.git_base_path" class="form-input" placeholder="charts/g50-uat" />
          </div>
        </div>
        <div class="grid">
          <div class="form-group">
            <label class="form-label">K8s 命名空间</label>
            <input v-model="envForm.namespace" class="form-input" placeholder="g50-uat" />
          </div>
          <div class="form-group">
            <label class="form-label">ArgoCD project</label>
            <input v-model="envForm.argocd_project" class="form-input" placeholder="default" />
          </div>
        </div>
        <div class="form-group">
          <label class="form-label">ArgoCD 集群</label>
          <input v-model="envForm.argocd_cluster" class="form-input" placeholder="in-cluster" />
        </div>
      </div>
      <div class="dialog-actions">
        <button class="btn btn-outline" @click="envDialogOpen=false">取消</button>
        <button class="btn btn-primary" @click="saveEnv">保存</button>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted, computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Plus } from 'lucide-vue-next'
import { projectsApi, projectEnvsApi, environmentsApi, modulesApi } from '../api'
import { success, error } from '../stores/ui'

const route = useRoute()
const router = useRouter()
const projectId = Number(route.params.id)

const project = ref(null)
const projectEnvs = ref([])
const modules = ref([])
const environments = ref([])

const modulesByPe = computed(() => {
  const map = {}
  modules.value.forEach(m => {
    if (!map[m.project_env_id]) map[m.project_env_id] = []
    map[m.project_env_id].push(m)
  })
  return map
})

function statusBadge(s) {
  return { active: 'badge-success', scaled_zero: 'badge-warning', disabled: 'badge-gray', deleted: 'badge-danger' }[s] || 'badge-gray'
}

const envDialogOpen = ref(false)
const envForm = ref({})

async function load() {
  try {
    project.value = (await projectsApi.get(projectId)).data
    projectEnvs.value = (await projectEnvsApi.list({ project_id: projectId })).data || []
    environments.value = (await environmentsApi.list()).data || []
    // 加载所有模块 (按 project_env)
    const all = []
    for (const pe of projectEnvs.value) {
      const r = await modulesApi.list({ project_env_id: pe.id })
      ;(r.data || []).forEach(m => all.push(m))
    }
    modules.value = all
  } catch (e) { error('加载失败: ' + e.message) }
}

function openCreateEnv() {
  envForm.value = { id: null, env_id: 0, git_repo: '', git_branch: 'main', git_base_path: '',
    namespace: '', argocd_project: 'default', argocd_cluster: 'in-cluster' }
  envDialogOpen.value = true
}

function openEditEnv(pe) {
  envForm.value = { ...pe }
  envDialogOpen.value = true
}

async function saveEnv() {
  if (!envForm.value.env_id || !envForm.value.git_repo) return error('环境和 Git 仓库必填')
  try {
    const data = { ...envForm.value, project_id: projectId }
    if (envForm.value.id) await projectEnvsApi.update(envForm.value.id, data)
    else await projectEnvsApi.create(data)
    success('保存成功'); envDialogOpen.value = false; load()
  } catch (e) { error('保存失败: ' + (e.response?.data?.message || e.message)) }
}

function goCreateModule(pe) {
  router.push({ name: 'CreateModule', query: { project_env_id: pe.id } })
}

onMounted(load)
</script>

<style scoped>
.env-section { margin-top: 20px; border: 1px solid #e5e7eb; border-radius: 6px; overflow: hidden; }
.env-header { padding: 10px 14px; background: #f9fafb; border-bottom: 1px solid #e5e7eb; display: flex; justify-content: space-between; align-items: center; }
.dialog-mask { position: fixed; inset: 0; background: rgba(0,0,0,.4); z-index: 9000; display: flex; align-items: center; justify-content: center; }
.dialog { background: white; border-radius: 8px; min-width: 560px; box-shadow: 0 10px 40px rgba(0,0,0,.2); overflow: hidden; }
.dialog-title { padding: 16px 20px; font-weight: 600; font-size: 16px; border-bottom: 1px solid #e5e7eb; }
.dialog-content { padding: 20px; max-height: 70vh; overflow-y: auto; }
.dialog-actions { padding: 12px 20px; display: flex; gap: 8px; justify-content: flex-end; background: #f9fafb; border-top: 1px solid #e5e7eb; }
.grid { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
code { background: #f3f4f6; padding: 1px 5px; border-radius: 3px; font-size: 12px; }
</style>
