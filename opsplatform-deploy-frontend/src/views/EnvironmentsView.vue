<template>
  <div>
    <!-- KPI Row -->
    <div class="kpi-bar">
      <div class="kpi kpi-blue">
        <div class="kpi-icon"><Layers :size="18" /></div>
        <div class="kpi-body">
          <div class="kpi-label">环境总数</div>
          <div class="kpi-value">{{ list.length }}</div>
          <div class="kpi-foot">dev / test / uat / prod</div>
        </div>
      </div>
      <div class="kpi kpi-green">
        <div class="kpi-icon"><Cloud :size="18" /></div>
        <div class="kpi-body">
          <div class="kpi-label">ArgoCD 已配置</div>
          <div class="kpi-value">{{ configuredArgo }}</div>
          <div class="kpi-foot">{{ list.length - configuredArgo }} 个环境未配置</div>
        </div>
      </div>
      <div class="kpi kpi-cyan">
        <div class="kpi-icon"><Zap :size="18" /></div>
        <div class="kpi-body">
          <div class="kpi-label">自动同步</div>
          <div class="kpi-value">{{ autoSyncCount }}</div>
          <div class="kpi-foot">{{ list.length - autoSyncCount }} 手动同步</div>
        </div>
      </div>
      <div class="kpi kpi-purple">
        <div class="kpi-icon"><Server :size="18" /></div>
        <div class="kpi-body">
          <div class="kpi-label">项目-环境实例</div>
          <div class="kpi-value">{{ projectEnvCount }}</div>
          <div class="kpi-foot">跨所有环境</div>
        </div>
      </div>
    </div>

    <!-- Toolbar -->
    <div class="toolbar">
      <div class="toolbar-left">
        <span class="toolbar-title">环境字典</span>
        <span class="text-gray text-sm">每环境独立 ArgoCD 实例</span>
      </div>
      <div class="toolbar-right">
        <button class="btn btn-primary" @click="openCreate"><Plus :size="13" /> 新增环境</button>
      </div>
    </div>

    <!-- Table -->
    <div class="card" style="padding: 0; overflow: hidden;">
      <GhostEmpty v-if="!list.length"
        :headers="[{label:'环境',width:'200px'},{label:'代号',width:'110px'},{label:'同步策略',width:'110px'},{label:'ArgoCD',width:'110px'},{label:'URL'},{label:'更新时间',width:'120px'},{label:'操作',width:'180px'}]"
        :icon="Layers"
        title="暂无环境"
        description="添加 dev / test / uat / prod 等环境并配置 ArgoCD"
        cta-label="新增环境"
        @cta="openCreate" />
      <table v-else class="table">
        <thead>
          <tr>
            <th style="width: 200px">环境</th>
            <th style="width: 110px">代号</th>
            <th style="width: 110px">同步策略</th>
            <th style="width: 110px">ArgoCD</th>
            <th>URL</th>
            <th style="width: 120px">更新时间</th>
            <th style="width: 180px; text-align: right;">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="e in list" :key="e.id">
            <td>
              <div class="env-cell-inline">
                <span class="dot" :style="{ background: envColor(e.name) }"></span>
                <strong>{{ e.display_name || e.name }}</strong>
              </div>
              <div v-if="e.description" class="text-xs text-muted" style="margin-left: 14px; margin-top: 2px">{{ e.description }}</div>
            </td>
            <td><code class="mono text-xs">{{ e.name }}</code></td>
            <td>
              <span class="badge" :class="e.auto_sync ? 'badge-success' : 'badge-gray'">
                {{ e.auto_sync ? 'AUTO' : 'MANUAL' }}
              </span>
            </td>
            <td>
              <span class="chip" :class="e.argocd_url ? 'chip-green' : 'chip-gray'">
                <span class="dot" :class="e.argocd_url ? 'dot-success' : 'dot-gray'"></span>
                {{ e.argocd_url ? '已连接' : '未配置' }}
              </span>
            </td>
            <td>
              <code v-if="e.argocd_url" class="mono text-xs">{{ e.argocd_url }}</code>
              <span v-else class="text-muted text-xs">—</span>
            </td>
            <td class="text-xs text-gray mono">{{ formatTime(e.updated_at) }}</td>
            <td style="text-align: right;">
              <div class="actions" style="justify-content: flex-end">
                <button class="btn btn-sm btn-outline" :disabled="!e.argocd_url" @click="onTest(e)">测试</button>
                <button class="btn btn-sm btn-outline" @click="openEdit(e)">编辑</button>
                <button class="btn btn-sm btn-danger-light" @click="onDelete(e)">删除</button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- Dialog -->
    <div v-if="dialogOpen" class="dialog-mask" @click.self="dialogOpen=false">
      <div class="dialog">
        <div class="dialog-title">{{ form.id ? '编辑环境' : '新增环境' }}</div>
        <div class="dialog-content">
          <div class="grid-2">
            <div class="form-group">
              <label class="form-label">环境代号 <span class="text-danger">*</span></label>
              <input v-model="form.name" class="form-input" :disabled="!!form.id" placeholder="如 uat / prod" />
            </div>
            <div class="form-group">
              <label class="form-label">显示名</label>
              <input v-model="form.display_name" class="form-input" placeholder="如 预发布" />
            </div>
          </div>

          <div class="section-label">ArgoCD 配置</div>
          <div class="form-group">
            <label class="form-label">ArgoCD URL</label>
            <input v-model="form.argocd_url" class="form-input" placeholder="https://argocd-uat.xx.com" />
          </div>
          <div class="form-group">
            <label class="form-label">ArgoCD Token</label>
            <input v-model="form.argocd_token" class="form-input" type="password" placeholder="留空不修改 (编辑)" />
            <div class="form-help">AES 加密存储</div>
          </div>
          <div class="form-group">
            <label class="form-label flex items-center gap-2">
              <input type="checkbox" v-model="form.auto_sync" :true-value="1" :false-value="0" />
              <span>发布后自动调用 ArgoCD sync</span>
            </label>
            <div class="form-help">建议: prod 手动, 其他自动</div>
          </div>

          <div class="section-label">其他</div>
          <div class="grid-2">
            <div class="form-group">
              <label class="form-label">描述</label>
              <input v-model="form.description" class="form-input" />
            </div>
            <div class="form-group">
              <label class="form-label">排序</label>
              <input v-model.number="form.sort_order" type="number" class="form-input" />
            </div>
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
import { ref, computed, onMounted } from 'vue'
import { Plus, Layers, Cloud, Zap, Server } from 'lucide-vue-next'
import { environmentsApi, projectEnvsApi } from '../api'
import { success, error, info, confirm } from '../stores/ui'
import GhostEmpty from '../components/GhostEmpty.vue'

const list = ref([])
const projectEnvCount = ref(0)
const dialogOpen = ref(false)
const form = ref({
  id: null, name: '', display_name: '', auto_sync: 0,
  argocd_url: '', argocd_token: '', description: '', sort_order: 0
})

const configuredArgo = computed(() => list.value.filter(e => e.argocd_url).length)
const autoSyncCount = computed(() => list.value.filter(e => e.auto_sync).length)

function envColor(name) {
  const map = { dev: '#10b981', test: '#06b6d4', uat: '#f59e0b', prod: '#ef4444' }
  return map[name] || '#64748b'
}

function formatTime(t) {
  if (!t) return '-'
  return t.slice(0, 16).replace('T', ' ')
}

async function load() {
  try {
    const [e, pe] = await Promise.all([environmentsApi.list(), projectEnvsApi.list()])
    list.value = e.data || []
    projectEnvCount.value = (pe.data || []).length
  } catch (e) { error('加载失败: ' + e.message) }
}

function openCreate() {
  form.value = { id: null, name: '', display_name: '', auto_sync: 0, argocd_url: '', argocd_token: '', description: '', sort_order: 0 }
  dialogOpen.value = true
}
function openEdit(e) { form.value = { ...e }; dialogOpen.value = true }

async function onSave() {
  if (!form.value.name) return error('环境代号不能为空')
  try {
    if (form.value.id) await environmentsApi.update(form.value.id, form.value)
    else await environmentsApi.create(form.value)
    success('保存成功'); dialogOpen.value = false; load()
  } catch (e) { error('保存失败: ' + (e.response?.data?.message || e.message)) }
}

async function onDelete(e) {
  if (!await confirm({ title: '删除环境', message: `确认删除 "${e.display_name || e.name}"?`, danger: true })) return
  try { await environmentsApi.delete(e.id); success('已删除'); load() }
  catch (err) { error('删除失败: ' + (err.response?.data?.message || err.message)) }
}

async function onTest(e) {
  try {
    const r = await environmentsApi.testArgocd(e.id)
    info(r.data?.msg || '测试完成')
  } catch (err) { error('测试失败: ' + err.message) }
}

onMounted(load)
</script>

<style scoped>
.env-cell-inline { display: flex; align-items: center; gap: 8px; }
.grid-2 { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
.section-label {
  font-size: 11.5px; font-weight: 600; color: #1e40af;
  margin: 14px 0 8px; padding-bottom: 5px;
  border-bottom: 1px solid #dbeafe;
  text-transform: uppercase; letter-spacing: .3px;
}
</style>
