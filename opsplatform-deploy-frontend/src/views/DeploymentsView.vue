<template>
  <div>
    <!-- KPI -->
    <div class="kpi-bar">
      <div class="kpi kpi-blue">
        <div class="kpi-icon"><Rocket :size="18" /></div>
        <div class="kpi-body">
          <div class="kpi-label">总发布数</div>
          <div class="kpi-value">{{ total }}</div>
          <div class="kpi-foot">全部历史</div>
        </div>
      </div>
      <div class="kpi kpi-green">
        <div class="kpi-icon"><Check :size="18" /></div>
        <div class="kpi-body">
          <div class="kpi-label">成功</div>
          <div class="kpi-value">{{ stats.success }}</div>
          <div class="kpi-foot">成功率 {{ stats.successRate }}%</div>
        </div>
      </div>
      <div class="kpi kpi-red">
        <div class="kpi-icon"><X :size="18" /></div>
        <div class="kpi-body">
          <div class="kpi-label">失败</div>
          <div class="kpi-value">{{ stats.failed }}</div>
          <div class="kpi-foot">需要关注</div>
        </div>
      </div>
      <div class="kpi kpi-amber">
        <div class="kpi-icon"><Clock :size="18" /></div>
        <div class="kpi-body">
          <div class="kpi-label">今日发布</div>
          <div class="kpi-value">{{ stats.today }}</div>
          <div class="kpi-foot">{{ new Date().toISOString().slice(0,10) }}</div>
        </div>
      </div>
      <div class="kpi kpi-cyan">
        <div class="kpi-icon"><Users :size="18" /></div>
        <div class="kpi-body">
          <div class="kpi-label">操作人</div>
          <div class="kpi-value">{{ uniqueOperators }}</div>
          <div class="kpi-foot">参与发布人数</div>
        </div>
      </div>
    </div>

    <!-- Toolbar -->
    <div class="toolbar">
      <div class="toolbar-left">
        <span class="toolbar-title">发布历史</span>
        <div class="filter-tabs">
          <button class="filter-tab" :class="{ active: filters.status==='' }" @click="filters.status=''; reload()">全部</button>
          <button class="filter-tab" :class="{ active: filters.status==='success' }" @click="filters.status='success'; reload()">成功</button>
          <button class="filter-tab" :class="{ active: filters.status==='failed' }" @click="filters.status='failed'; reload()">失败</button>
          <button class="filter-tab" :class="{ active: filters.status==='pending' }" @click="filters.status='pending'; reload()">进行中</button>
        </div>
      </div>
      <div class="toolbar-right">
        <select v-model="filters.action" class="form-select" style="width:140px" @change="reload">
          <option value="">全部操作</option>
          <option v-for="a in actions" :key="a.value" :value="a.value">{{ a.label }}</option>
        </select>
        <button class="btn btn-outline" @click="load"><RotateCw :size="13" /> 刷新</button>
      </div>
    </div>

    <!-- Table -->
    <div class="card" style="padding: 0; overflow: hidden">
      <GhostEmpty v-if="!list.length"
        :headers="[{label:'时间',width:'130px'},{label:'操作',width:'110px'},{label:'模块',width:'200px'},{label:'版本变化',width:'180px'},{label:'Git Commit',width:'120px'},{label:'ArgoCD',width:'110px'},{label:'状态',width:'100px'},{label:'操作人'}]"
        :icon="History"
        :title="filters.action || filters.status ? '当前筛选无数据' : '暂无发布记录'"
        description="新建模块或更新镜像即可产生发布记录"
        :cta-label="filters.action || filters.status ? '' : '新建模块'"
        cta-path="/modules/create" />
      <table v-else class="table">
        <thead>
          <tr>
            <th style="width: 130px">时间</th>
            <th style="width: 110px">操作</th>
            <th style="width: 200px">模块</th>
            <th style="width: 180px">版本变化</th>
            <th style="width: 120px">Git Commit</th>
            <th style="width: 110px">ArgoCD</th>
            <th style="width: 100px">状态</th>
            <th>操作人</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="d in list" :key="d.id">
            <td>
              <div class="mono text-xs" style="font-weight: 600; color: #0f172a">
                {{ (d.created_at || '').slice(11, 19) }}
              </div>
              <div class="text-muted text-xs mono">{{ (d.created_at || '').slice(0, 10) }}</div>
            </td>
            <td>
              <span class="chip" :class="actionChipClass(d.action)">{{ actionLabel(d.action) }}</span>
            </td>
            <td class="text-bold mono" style="font-size: 12px">{{ d.module_name || '—' }}</td>
            <td>
              <div v-if="d.from_tag || d.to_tag" class="mono text-xs" style="line-height: 1.5">
                <div v-if="d.from_tag" class="text-muted">{{ d.from_tag }}</div>
                <div v-if="d.to_tag" class="text-success">→ {{ d.to_tag }}</div>
              </div>
              <span v-else class="text-muted text-xs">—</span>
            </td>
            <td>
              <a v-if="d.git_commit_url" :href="d.git_commit_url" target="_blank" class="mono text-xs" style="color: #1e40af">
                {{ d.git_commit?.slice(0, 7) }}
              </a>
              <span v-else-if="d.git_commit" class="mono text-xs">{{ d.git_commit.slice(0, 7) }}</span>
              <span v-else class="text-muted text-xs">—</span>
            </td>
            <td>
              <span v-if="d.argocd_sync_status" class="chip" :class="argoBadge(d.argocd_sync_status)">
                {{ d.argocd_sync_status }}
              </span>
              <span v-else class="text-muted text-xs">—</span>
            </td>
            <td>
              <span class="chip" :class="statusChip(d.status)">
                <span class="dot" :class="dotClass(d.status)"></span>
                {{ d.status }}
              </span>
            </td>
            <td class="text-sm">{{ d.operator || 'system' }}</td>
          </tr>
        </tbody>
      </table>

      <div v-if="total > pageSize" class="pagination">
        <button class="btn btn-sm btn-outline" :disabled="page<=1" @click="page--; load()">
          <ChevronLeft :size="12" /> 上一页
        </button>
        <span style="font-size: 12px; color: #64748b; padding: 0 12px">
          第 <strong class="mono">{{ page }}</strong> / <span class="mono">{{ Math.ceil(total/pageSize) }}</span> 页 · 共 <span class="mono">{{ total }}</span> 条
        </span>
        <button class="btn btn-sm btn-outline" :disabled="page>=Math.ceil(total/pageSize)" @click="page++; load()">
          下一页 <ChevronRight :size="12" />
        </button>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { Rocket, Check, X, Clock, Users, RotateCw, History, ChevronLeft, ChevronRight } from 'lucide-vue-next'
import { deploymentsApi } from '../api'
import { error } from '../stores/ui'
import GhostEmpty from '../components/GhostEmpty.vue'

const list = ref([])
const total = ref(0)
const page = ref(1)
const pageSize = 30
const filters = ref({ action: '', status: '' })

const actions = [
  { value: 'create', label: '新建' },
  { value: 'update_image', label: '更镜像' },
  { value: 'update_config', label: '改配置' },
  { value: 'update_secret', label: '改Secret' },
  { value: 'restart', label: '重启' },
  { value: 'scale_zero', label: '停机' },
  { value: 'scale_up', label: '恢复' },
  { value: 'delete', label: '删除' },
  { value: 'rollback', label: '回滚' }
]

const stats = computed(() => {
  const s = list.value.filter(d => d.status === 'success').length
  const f = list.value.filter(d => d.status === 'failed').length
  const today = new Date().toISOString().slice(0, 10)
  const t = list.value.filter(d => (d.created_at || '').slice(0, 10) === today).length
  return { success: s, failed: f, today: t,
    successRate: list.value.length ? Math.round(s / list.value.length * 100) : 100 }
})

const uniqueOperators = computed(() => new Set(list.value.map(d => d.operator).filter(Boolean)).size)

function dotClass(s) { return { success: 'dot-success', failed: 'dot-danger', pending: 'dot-warning' }[s] || 'dot-gray' }
function statusChip(s) { return { success: 'chip-green', failed: 'chip-red', pending: 'chip-amber' }[s] || 'chip-gray' }
function argoBadge(s) { return { success: 'chip-green', failed: 'chip-red', skipped: 'chip-gray', pending: 'chip-amber' }[s] || 'chip-gray' }
function actionLabel(a) { return actions.find(x => x.value === a)?.label || a }
function actionChipClass(a) {
  return { create: 'chip-green', update_image: 'chip', update_config: 'chip', update_secret: 'chip-amber',
           restart: 'chip-amber', scale_zero: 'chip-gray', scale_up: 'chip-green',
           delete: 'chip-red', rollback: 'chip-amber' }[a] || 'chip-gray'
}

async function reload() { page.value = 1; await load() }
async function load() {
  try {
    const r = await deploymentsApi.list({ ...filters.value, page: page.value, page_size: pageSize })
    list.value = r.data?.list || []
    total.value = r.data?.total || 0
  } catch (e) { error('加载失败: ' + e.message) }
}

onMounted(load)
</script>

<style scoped>
.pagination { padding: 14px 16px; display: flex; align-items: center; justify-content: center; gap: 6px; border-top: 1px solid #f1f5f9; }
</style>
