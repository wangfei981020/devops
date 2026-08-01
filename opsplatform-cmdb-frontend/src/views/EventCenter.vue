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
          <template v-if="loadErr">共 —</template>
          <!-- 截断时把"显示 N / 共 M"分开写清楚：只写一个 500 会被当成真实总数（CMDB-019） -->
          <template v-else-if="truncated">显示 {{ rows.length }} / 共 {{ total }}</template>
          <template v-else>共 {{ total }}</template>
          <b v-if="!loadErr && cnt.critical" style="color:#f56c6c;margin-left:8px">严重 {{ cnt.critical }}</b>
          <b v-if="!loadErr && cnt.warning" style="color:#e6a23c;margin-left:8px">警告 {{ cnt.warning }}</b>
        </span>
      </div>
      <LoadError :error="loadErr" @retry="load" />
      <el-alert v-if="truncated" type="warning" :closable="false" show-icon style="margin-bottom:10px"
        :title="`只显示最近 ${limit} 条，选定范围内实际有 ${total} 条`"
        description="更早的事件没有展示出来。请缩小时间范围，或按来源/级别筛选后再看。" />
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
      <!-- 「无事件 🎉」是结论，加载失败时不能这么说（CMDB-013） -->
      <el-empty v-if="!loading && !loadErr && !rows.length" description="所选范围内无事件 🎉" />
    </el-card>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { Refresh } from '@element-plus/icons-vue'
import { eventCenter } from '../api/cmdb'
import { usePager } from '../composables/usePager'
import { normalizeError } from '../api/http'
import Pager from '../components/Pager.vue'
import LoadError from '../components/LoadError.vue'

const rows = ref([]); const loading = ref(false); const loadErr = ref('')
// total/truncated 来自后端：total 是截断**前**的真实总量
const total = ref(0); const truncated = ref(false); const limit = ref(500)
// by_level 也在截断前统计，所以严重/警告的计数不会因为截断而变小
const byLevel = ref(null)
const source = ref(''); const level = ref(''); const days = ref(30)
const { page, pageSize, paged } = usePager(rows)
const cnt = computed(() => {
  // 优先用后端给的全量分级计数；老版本后端没有这个字段时才回退到按当前列表数
  if (byLevel.value) return { critical: byLevel.value.critical || 0, warning: byLevel.value.warning || 0 }
  const o = { critical: 0, warning: 0 }
  for (const r of rows.value) { if (r.level === 'critical') o.critical++; else if (r.level === 'warning') o.warning++ }
  return o
})
function srcText(s) { return { expiry: '到期', change: '变更', sync: '同步失败', k8s: 'K8s' }[s] || s }
function srcType(s) { return { expiry: 'warning', change: 'info', sync: 'danger', k8s: 'warning' }[s] || 'info' }
function lvlText(l) { return { critical: '严重', warning: '警告', info: '信息' }[l] || l }

async function load() {
  loading.value = true
  loadErr.value = ''
  try {
    const p = { days: days.value }
    if (source.value) p.source = source.value
    if (level.value) p.level = level.value
    const r = await eventCenter(p)
    rows.value = r.events || []
    total.value = r.total ?? rows.value.length
    truncated.value = !!r.truncated
    limit.value = r.limit || 500
    byLevel.value = r.by_level || null
  } catch (e) {
    loadErr.value = normalizeError(e).message
    rows.value = []; total.value = 0; truncated.value = false; byLevel.value = null
  } finally { loading.value = false }
}
onMounted(load)
</script>

<style scoped>
.page-head { margin-bottom: 14px; }
.page-title { font-size: 18px; font-weight: 600; }
.muted { color: #909399; font-size: 12px; }
.bar { display: flex; gap: 10px; align-items: center; margin-bottom: 12px; }
</style>
