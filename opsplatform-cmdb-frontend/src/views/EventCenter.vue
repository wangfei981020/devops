<template>
  <div class="page">
    <div class="page-head">
      <span class="page-title">事件中心</span>
      <span class="muted" style="margin-left:10px">平台统一时间线:到期 / 变更 / 同步失败 / K8s Warning。排障先看这里,再钻具体诊断</span>
    </div>
    <el-card shadow="never">
      <div class="bar">
        <el-select v-model="source" clearable placeholder="来源" style="width:150px" @change="load">
          <el-option label="到期" value="expiry" /><el-option label="变更" value="change" />
          <el-option label="同步失败" value="sync" /><el-option label="K8s Warning" value="k8s" />
        </el-select>
        <el-select v-model="level" clearable placeholder="级别" style="width:130px" @change="load">
          <el-option label="严重" value="critical" /><el-option label="警告" value="warning" /><el-option label="信息" value="info" />
        </el-select>
        <el-select v-model="days" style="width:120px" @change="load">
          <el-option :value="7" label="近7天" /><el-option :value="30" label="近30天" /><el-option :value="90" label="近90天" />
        </el-select>
        <el-button :icon="Refresh" :loading="loading" @click="load">刷新</el-button>
        <span class="muted" style="margin-left:auto">
          共 {{ rows.length }}
          <b v-if="cnt.critical" style="color:#f56c6c;margin-left:8px">严重 {{ cnt.critical }}</b>
          <b v-if="cnt.warning" style="color:#e6a23c;margin-left:8px">警告 {{ cnt.warning }}</b>
        </span>
      </div>
      <el-table :data="paged" size="small" v-loading="loading">
        <el-table-column label="时间" width="160" prop="time" />
        <el-table-column label="来源" width="110"><template #default="{ row }">
          <el-tag size="small" :type="srcType(row.source)">{{ srcText(row.source) }}</el-tag>
        </template></el-table-column>
        <el-table-column label="级别" width="80"><template #default="{ row }">
          <el-tag size="small" :type="row.level==='critical'?'danger':(row.level==='warning'?'warning':'info')">{{ lvlText(row.level) }}</el-tag>
        </template></el-table-column>
        <el-table-column label="对象" min-width="220" prop="object" show-overflow-tooltip />
        <el-table-column label="事件" min-width="150" prop="title" />
        <el-table-column label="详情" min-width="320" prop="message" show-overflow-tooltip />
        <el-table-column label="集群" width="130"><template #default="{ row }">{{ row.cluster || '—' }}</template></el-table-column>
      </el-table>
      <Pager :total="rows.length" v-model:page="page" v-model:page-size="pageSize" />
      <el-empty v-if="!loading && !rows.length" description="所选范围内无事件 🎉" />
    </el-card>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Refresh } from '@element-plus/icons-vue'
import { eventCenter } from '../api/cmdb'
import { usePager } from '../composables/usePager'
import Pager from '../components/Pager.vue'

const rows = ref([]); const loading = ref(false)
const source = ref(''); const level = ref(''); const days = ref(30)
const { page, pageSize, paged } = usePager(rows)
const cnt = computed(() => {
  const o = { critical: 0, warning: 0 }
  for (const r of rows.value) { if (r.level === 'critical') o.critical++; else if (r.level === 'warning') o.warning++ }
  return o
})
function srcText(s) { return { expiry: '到期', change: '变更', sync: '同步失败', k8s: 'K8s' }[s] || s }
function srcType(s) { return { expiry: 'warning', change: 'info', sync: 'danger', k8s: 'warning' }[s] || 'info' }
function lvlText(l) { return { critical: '严重', warning: '警告', info: '信息' }[l] || l }

async function load() {
  loading.value = true
  try {
    const p = { days: days.value }
    if (source.value) p.source = source.value
    if (level.value) p.level = level.value
    const r = await eventCenter(p)
    rows.value = r.events || []
  } catch (e) { ElMessage.error('加载失败') } finally { loading.value = false }
}
onMounted(load)
</script>

<style scoped>
.page-head { margin-bottom: 14px; }
.page-title { font-size: 18px; font-weight: 600; }
.muted { color: #909399; font-size: 12px; }
.bar { display: flex; gap: 10px; align-items: center; margin-bottom: 12px; }
</style>
