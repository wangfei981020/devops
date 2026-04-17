<template>
  <div class="card">
    <div class="card-header">
      <div class="card-title">{{ isEdit ? '编辑模块' : '新建模块' }}</div>
    </div>

    <h3 class="section-title">基本信息</h3>
    <div class="grid">
      <div class="form-group">
        <label class="form-label">所属项目-环境 <span style="color:#ef4444">*</span></label>
        <select v-model.number="form.project_env_id" :disabled="isEdit" class="form-select">
          <option :value="0">请选择</option>
          <option v-for="pe in projectEnvs" :key="pe.id" :value="pe.id">{{ pe.project_name }} / {{ pe.env_name }}</option>
        </select>
      </div>
      <div class="form-group">
        <label class="form-label">模块名 <span style="color:#ef4444">*</span></label>
        <input v-model="form.name" class="form-input" :disabled="isEdit" placeholder="如 g50-baccarat-resource-backend" />
      </div>
      <div class="form-group">
        <label class="form-label">Chart 模板 <span style="color:#ef4444">*</span></label>
        <select v-model.number="form.template_id" class="form-select">
          <option :value="0">请选择</option>
          <option v-for="t in templates" :key="t.id" :value="t.id">{{ t.name }} ({{ t.type }})</option>
        </select>
      </div>
    </div>

    <h3 class="section-title">镜像</h3>
    <div class="grid">
      <div class="form-group">
        <label class="form-label">镜像仓库 <span style="color:#ef4444">*</span></label>
        <input v-model="form.image_repo" class="form-input" placeholder="harbor.xx/g50/g50-baccarat-resource-backend" />
      </div>
      <div class="form-group">
        <label class="form-label">镜像 Tag</label>
        <input v-model="form.current_tag" class="form-input" placeholder="20260415092722-8" />
      </div>
    </div>

    <h3 class="section-title">副本与资源</h3>
    <div class="grid">
      <div class="form-group">
        <label class="form-label">副本数</label>
        <input v-model.number="form.replicas" type="number" min="0" class="form-input" />
        <div class="form-help">设为 0 软下线</div>
      </div>
      <div class="form-group">
        <label class="form-label">滚动更新策略 (JSON)</label>
        <input v-model="form.rolling_update" class="form-input" placeholder='{"maxSurge":1,"maxUnavailable":0}' />
      </div>
    </div>
    <div class="form-group">
      <label class="form-label">resources (requests/limits JSON)</label>
      <textarea v-model="form.resources" class="form-textarea" rows="4" placeholder='{"requests":{"cpu":"100m","memory":"256Mi"},"limits":{"cpu":"1","memory":"1Gi"}}'></textarea>
    </div>
    <div class="form-group">
      <label class="form-label">autoscaling (HPA JSON)</label>
      <input v-model="form.autoscaling" class="form-input" placeholder='{"enabled":true,"minReplicas":1,"maxReplicas":3}' />
    </div>

    <h3 class="section-title">环境变量与 Secret</h3>
    <div class="form-group">
      <label class="form-label">普通环境变量 (JSON 数组)</label>
      <textarea v-model="form.env_vars" class="form-textarea" rows="4" placeholder='[{"key":"LOG_LEVEL","value":"info"}]'></textarea>
    </div>
    <div class="form-group">
      <label class="form-label">extraEnvVars (Secret 名称数组)</label>
      <textarea v-model="form.extra_env_vars" class="form-textarea" rows="3" placeholder='["g50-nacos-secret","g50-redis-secret"]'></textarea>
    </div>
    <div class="form-group">
      <label class="form-label">tidbSecrets (JSON)</label>
      <textarea v-model="form.tidb_secrets" class="form-textarea" rows="3" placeholder='[{"name":"g50-baccarat-tidb","database":"baccarat"}]'></textarea>
    </div>

    <h3 class="section-title">探针 (覆盖模板默认)</h3>
    <div class="form-group">
      <label class="form-label">探针覆盖 (JSON, 留空使用模板默认)</label>
      <textarea v-model="form.probe_override" class="form-textarea" rows="4" placeholder='{"liveness":{"path":"/health","port":8080}}'></textarea>
    </div>

    <h3 v-if="isFrontendTemplate" class="section-title">前端 ConfigMap (config.js)</h3>
    <div v-if="isFrontendTemplate" class="form-group">
      <label class="form-label">configmap_data (JSON)</label>
      <textarea v-model="form.configmap_data" class="form-textarea" rows="6" placeholder='{"apiGateway":"...","theme":"light"}'></textarea>
    </div>

    <div style="border-top:1px solid #e5e7eb;padding-top:16px;display:flex;gap:8px;justify-content:flex-end;margin-top:20px">
      <button class="btn btn-outline" @click="$router.back()">取消</button>
      <button class="btn btn-primary" @click="onSave">{{ isEdit ? '保存修改' : '创建模块' }}</button>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted, computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { modulesApi, projectEnvsApi, chartTemplatesApi } from '../api'
import { success, error } from '../stores/ui'

const route = useRoute()
const router = useRouter()
const isEdit = computed(() => !!route.params.id)

const projectEnvs = ref([])
const templates = ref([])
const form = ref({
  project_env_id: Number(route.query.project_env_id) || 0,
  name: '',
  template_id: 0,
  image_repo: '',
  current_tag: '',
  replicas: 1,
  autoscaling: '',
  resources: '',
  rolling_update: '{"maxSurge":1,"maxUnavailable":0}',
  env_vars: '[]',
  extra_env_vars: '[]',
  tidb_secrets: '[]',
  probe_override: '',
  configmap_data: ''
})

const isFrontendTemplate = computed(() => {
  const t = templates.value.find(x => x.id === form.value.template_id)
  return t && t.type === 'frontend'
})

async function loadMeta() {
  try {
    projectEnvs.value = (await projectEnvsApi.list()).data || []
    templates.value = (await chartTemplatesApi.list()).data || []
    if (isEdit.value) {
      const m = (await modulesApi.get(route.params.id)).data
      Object.assign(form.value, m)
    }
  } catch (e) { error('加载失败: ' + e.message) }
}

async function onSave() {
  if (!form.value.project_env_id || !form.value.name || !form.value.template_id) {
    return error('项目环境/模块名/模板必填')
  }
  try {
    if (isEdit.value) {
      await modulesApi.update(route.params.id, form.value)
      success('保存成功')
    } else {
      await modulesApi.create(form.value)
      success('创建成功 (后端核心实现待阶段3)')
    }
    router.back()
  } catch (e) { error('保存失败: ' + (e.response?.data?.message || e.message)) }
}

onMounted(loadMeta)
</script>

<style scoped>
.section-title { font-size: 14px; font-weight: 600; margin: 16px 0 8px; color: #374151; padding-bottom: 6px; border-bottom: 1px solid #f3f4f6; }
.grid { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
</style>
