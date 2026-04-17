<template>
  <div v-if="module" class="card">
    <div class="card-header">
      <div>
        <div class="card-title">{{ module.name }}</div>
        <div class="text-sm text-gray" style="margin-top:4px">
          {{ module.project_name }} / {{ module.env_name }} ·
          模板 <strong>{{ module.template_name }}</strong> ·
          ArgoCD App <code>{{ module.argocd_app_name }}</code>
        </div>
      </div>
      <div class="actions">
        <button class="btn btn-sm" @click="$router.push(`/modules/${module.id}/edit`)">编辑配置</button>
        <button class="btn btn-sm btn-primary" @click="onUpdateImage">更新镜像</button>
        <button class="btn btn-sm" @click="onRestart">重启</button>
        <button class="btn btn-sm" @click="onScale">扩缩</button>
        <button class="btn btn-sm" @click="onSync">手动 Sync</button>
        <button class="btn btn-sm btn-danger" @click="onDelete">删除</button>
      </div>
    </div>

    <div class="tabs">
      <div v-for="t in tabs" :key="t.key" class="tab" :class="{active: activeTab===t.key}" @click="activeTab=t.key">{{ t.label }}</div>
    </div>

    <!-- 配置概览 -->
    <div v-show="activeTab==='overview'" class="tab-pane">
      <div class="kv"><div class="k">镜像</div><div class="v"><code>{{ module.image_repo }}:{{ module.current_tag }}</code></div></div>
      <div class="kv"><div class="k">副本数</div><div class="v">{{ module.replicas }}</div></div>
      <div class="kv"><div class="k">状态</div><div class="v"><span class="badge" :class="statusBadge(module.status)">{{ module.status }}</span></div></div>
      <div class="kv"><div class="k">滚动策略</div><div class="v"><pre>{{ module.rolling_update || '-' }}</pre></div></div>
      <div class="kv"><div class="k">resources</div><div class="v"><pre>{{ module.resources || '-' }}</pre></div></div>
      <div class="kv"><div class="k">env_vars</div><div class="v"><pre>{{ module.env_vars || '-' }}</pre></div></div>
      <div class="kv"><div class="k">extraEnvVars</div><div class="v"><pre>{{ module.extra_env_vars || '-' }}</pre></div></div>
    </div>

    <!-- 发布历史 -->
    <div v-show="activeTab==='history'" class="tab-pane">
      <table class="table">
        <thead><tr><th>时间</th><th>操作</th><th>From → To</th><th>状态</th><th>操作人</th></tr></thead>
        <tbody>
          <tr v-for="d in deployments" :key="d.id">
            <td class="text-sm">{{ d.created_at?.slice(0,19).replace('T',' ') }}</td>
            <td>{{ d.action }}</td>
            <td class="text-sm">{{ d.from_tag }} → <strong>{{ d.to_tag }}</strong></td>
            <td><span class="badge" :class="d.status==='success'?'badge-success':'badge-danger'">{{ d.status }}</span></td>
            <td>{{ d.operator || '-' }}</td>
          </tr>
          <tr v-if="!deployments.length"><td colspan="5" class="empty">暂无发布记录</td></tr>
        </tbody>
      </table>
    </div>

    <!-- 运行时状态 -->
    <div v-show="activeTab==='runtime'" class="tab-pane">
      <div class="text-gray" style="padding:20px">运行时状态查询待阶段3实现 (ArgoCD + Pod 状态)</div>
    </div>

    <!-- values.yaml -->
    <div v-show="activeTab==='values'" class="tab-pane">
      <pre style="background:#1e293b;color:#cbd5e1;padding:14px;border-radius:6px;overflow:auto">{{ valuesContent || '点击下方按钮查看渲染内容' }}</pre>
      <button class="btn btn-sm" style="margin-top:10px" @click="loadValues">查看 values.yaml</button>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { modulesApi, deploymentsApi } from '../api'
import { success, error, info, confirm } from '../stores/ui'

const route = useRoute()
const router = useRouter()
const moduleId = Number(route.params.id)

const module = ref(null)
const deployments = ref([])
const valuesContent = ref('')
const activeTab = ref('overview')
const tabs = [
  { key: 'overview', label: '配置概览' },
  { key: 'history', label: '发布历史' },
  { key: 'runtime', label: '运行时状态' },
  { key: 'values', label: 'values.yaml' }
]

function statusBadge(s) {
  return { active: 'badge-success', scaled_zero: 'badge-warning', disabled: 'badge-gray', deleted: 'badge-danger' }[s] || 'badge-gray'
}

async function load() {
  try {
    module.value = (await modulesApi.get(moduleId)).data
    const r = await deploymentsApi.list({ module_id: moduleId, page_size: 50 })
    deployments.value = r.data?.list || []
  } catch (e) { error('加载失败: ' + e.message) }
}

async function loadValues() {
  try {
    const r = await modulesApi.values(moduleId)
    valuesContent.value = r.data?.msg || JSON.stringify(r.data, null, 2)
  } catch (e) { error('加载失败: ' + e.message) }
}

async function onUpdateImage() {
  const tag = window.prompt('请输入新的镜像 tag:', module.value.current_tag || '')
  if (!tag) return
  try { const r = await modulesApi.updateImage(moduleId, { tag }); info(r.data?.msg || '已提交'); load() }
  catch (e) { error('更新失败: ' + e.message) }
}

async function onRestart() {
  if (!await confirm({ title: '重启服务', message: `确认重启 ${module.value.name}?` })) return
  try { const r = await modulesApi.restart(moduleId); info(r.data?.msg || '已触发') }
  catch (e) { error('重启失败: ' + e.message) }
}

async function onScale() {
  const v = window.prompt('请输入副本数 (0=软下线):', String(module.value.replicas))
  if (v === null) return
  const replicas = Number(v)
  if (Number.isNaN(replicas) || replicas < 0) return error('副本数必须是非负整数')
  try { const r = await modulesApi.scale(moduleId, { replicas }); info(r.data?.msg || '已提交'); load() }
  catch (e) { error('扩缩失败: ' + e.message) }
}

async function onSync() {
  try { const r = await modulesApi.sync(moduleId); info(r.data?.msg || '已触发') }
  catch (e) { error('Sync 失败: ' + e.message) }
}

async function onDelete() {
  if (!await confirm({ title: '删除模块', message: `永久删除 ${module.value.name}? 该操作会清理 ArgoCD App 和 Git 目录`, danger: true, confirmText: '删除' })) return
  try { await modulesApi.delete(moduleId); success('已删除'); router.back() }
  catch (e) { error('删除失败: ' + e.message) }
}

onMounted(load)
</script>

<style scoped>
.tabs { display: flex; gap: 4px; border-bottom: 1px solid #e5e7eb; margin: -8px 0 16px; }
.tab { padding: 10px 16px; cursor: pointer; font-size: 13px; color: #6b7280; border-bottom: 2px solid transparent; }
.tab:hover { color: #374151; }
.tab.active { color: #2563eb; border-bottom-color: #2563eb; font-weight: 500; }
.tab-pane { padding: 8px 0; }
.kv { display: grid; grid-template-columns: 140px 1fr; padding: 8px 0; border-bottom: 1px solid #f3f4f6; }
.kv .k { color: #6b7280; font-size: 13px; }
.kv .v { font-size: 13px; }
.kv pre { margin: 0; background: #f9fafb; padding: 6px 10px; border-radius: 4px; font-size: 12px; max-width: 800px; overflow-x: auto; }
code { background: #f3f4f6; padding: 1px 6px; border-radius: 3px; font-size: 12px; }
.actions { display: flex; gap: 6px; flex-wrap: wrap; }
</style>
