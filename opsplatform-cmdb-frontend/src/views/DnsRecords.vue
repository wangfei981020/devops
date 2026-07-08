<template>
  <div class="page">
    <div class="page-head">
      <span class="page-title">DNS 记录</span>
      <div class="filter">
        <el-select v-model="sourceId" placeholder="数据源" style="width:170px">
          <el-option v-for="s in sources" :key="s.id" :label="`${s.name}（${s.provider}）`" :value="s.id" />
        </el-select>
        <el-button type="primary" :icon="Refresh" :loading="syncing" @click="doSync">
          {{ syncing ? `同步中 ${prog.done}/${prog.total || '…'}` : '从数据源同步' }}
        </el-button>
      </div>
    </div>

    <el-alert v-if="syncing" type="info" :closable="false" show-icon style="margin-bottom:12px">
      <template #title>
        后台同步中（域名较多，限流下约 1-2 分钟）：已处理 <b>{{ prog.done }}/{{ prog.total || '…' }}</b> 个域名 · 导入 {{ prog.imported_records || 0 }} 条解析。完成后自动刷新。
      </template>
    </el-alert>

    <el-alert v-if="staleCount" type="warning" :closable="false" show-icon style="margin-bottom:12px"
      :title="`有 ${staleCount} 个域名的 DNS 数据超过 24 小时未同步，建议同步`" />

    <el-card shadow="never" style="margin-bottom:12px">
      <div class="filter">
        <el-input v-model="domKeyword" placeholder="搜索主域名" clearable :prefix-icon="Search" style="width:220px" @keyup.enter="doDomSearch" />
        <el-button type="primary" :icon="Search" @click="doDomSearch">搜索</el-button>
        <el-button @click="domKeyword=''; doDomSearch()">重置</el-button>
        <span class="muted" style="margin-left:auto">共 {{ domFiltered.length }} 个域名</span>
      </div>
    </el-card>

    <el-card shadow="never">
      <el-table :data="domPaged" size="small" row-key="ci_id" v-loading="loading" @expand-change="onExpand">
        <el-table-column type="expand">
          <template #default="{ row }">
            <div class="reso" v-loading="recLoading[row.ci_id]">
              <div class="filter" style="margin-bottom:8px">
                <el-select v-model="recState[row.ci_id].type" clearable placeholder="类型" size="small" style="width:120px">
                  <el-option v-for="t in types" :key="t" :label="t" :value="t" />
                </el-select>
                <el-input v-model="recState[row.ci_id].kw" placeholder="搜索 主机/值" clearable size="small" style="width:220px" />
                <span class="muted">本域名 {{ recFiltered(row.ci_id).length }} 条</span>
              </div>
              <el-table :data="recPaged(row.ci_id)" size="small" max-height="360">
                <el-table-column label="类型" width="90"><template #default="{ row: r }">
                  <el-tag size="small" effect="plain" :style="typeStyle(r.type)">{{ r.type }}</el-tag>
                </template></el-table-column>
                <el-table-column prop="name" label="主机名(Name)" min-width="160" show-overflow-tooltip />
                <el-table-column prop="data" label="记录值(Data)" min-width="280" show-overflow-tooltip><template #default="{ row: r }"><span class="mono">{{ r.data }}</span></template></el-table-column>
                <el-table-column prop="ttl" label="TTL" width="80" />
                <el-table-column label="优先级" width="80"><template #default="{ row: r }">{{ r.priority ?? '—' }}</template></el-table-column>
                <el-table-column label="状态" width="110"><template #default="{ row: r }">
                  <el-tag v-if="r.protected" type="warning" size="small">🔒 受保护</el-tag>
                </template></el-table-column>
              </el-table>
              <el-empty v-if="!(recMap[row.ci_id] && recMap[row.ci_id].length) && !recLoading[row.ci_id]"
                description="该域名暂无 DNS 记录，选好数据源点右上「从数据源同步」" :image-size="50" />
              <el-pagination v-else small background v-model:current-page="recState[row.ci_id].page" v-model:page-size="recState[row.ci_id].size"
                :page-sizes="[20,50,100]" :total="recFiltered(row.ci_id).length" layout="total, sizes, prev, pager, next"
                style="margin-top:8px; justify-content:flex-end" />
            </div>
          </template>
        </el-table-column>
        <el-table-column label="主域名" min-width="240"><template #default="{ row }">
          <span :class="{ stale: row.stale }">{{ row.name }}</span>
          <el-tag v-if="row.stale" type="danger" size="small" style="margin-left:6px">已移出账号</el-tag>
          <el-tag v-else-if="row.dns_migrated" size="small" style="margin-left:6px;background:#7a5c8a;color:#fff;border-color:#7a5c8a">DNS已迁移</el-tag>
        </template></el-table-column>
        <el-table-column label="来源" width="130"><template #default="{ row }">
          <el-tag v-if="row.origin === 'manual'" type="info" size="small">手动录入</el-tag>
          <el-tag v-else type="success" size="small">{{ row.registrar_name || '同步' }}</el-tag>
        </template></el-table-column>
        <el-table-column label="域名到期" width="150"><template #default="{ row }">
          <template v-if="row.expiry_at">
            <span :class="expiryClass(row.expiry_at)">{{ row.expiry_at }}</span>
            <el-tag v-if="isExpired(row.expiry_at)" type="danger" size="small" style="margin-left:6px">已过期</el-tag>
          </template>
          <span v-else class="muted">—</span>
        </template></el-table-column>
        <el-table-column label="最近同步" width="170"><template #default="{ row }">
          <span v-if="domStale(row)" class="exp-orange">{{ row.last_synced || '未同步' }} ⚠️</span>
          <span v-else :class="{ muted: !row.last_synced }">{{ row.last_synced || '—' }}</span>
        </template></el-table-column>
        <el-table-column label="记录数" width="80"><template #default="{ row }">
          <span :class="{ muted: !row.dns_count }">{{ row.dns_count || 0 }}</span>
        </template></el-table-column>
        <el-table-column label="操作" width="120" fixed="right"><template #default="{ row }">
          <el-tooltip v-if="row.origin !== 'manual'" content="从数据源同步这个域名">
            <el-button link type="primary" :icon="Refresh" :loading="syncingOne[row.ci_id]" @click="syncOne(row)">同步</el-button>
          </el-tooltip>
          <span v-else class="muted">无需同步</span>
        </template></el-table-column>
      </el-table>
      <el-pagination v-model:current-page="domPage" v-model:page-size="domPageSize" :page-sizes="[10,20,50,100]"
        :total="domFiltered.length" layout="total, sizes, prev, pager, next" style="margin-top:12px; justify-content:flex-end" />
    </el-card>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { ElMessage } from 'element-plus'
import { Refresh, Search } from '@element-plus/icons-vue'
import { listDomains, listRegistrars, listDnsRecords, syncSource, syncSourceStatus, syncDomainRecords } from '../api/cmdb'

const sources = ref([]), domains = ref([]), loading = ref(false)
const sourceId = ref(null)
const syncing = ref(false)
const prog = ref({ total: 0, done: 0, imported_records: 0 })
const syncingOne = ref({})
let pollTimer = null
const types = ['A', 'AAAA', 'CNAME', 'MX', 'TXT', 'NS', 'CAA', 'SRV']
// DNS 记录类型固定配色（冷色/中性，红橙留给到期告警）
const TYPE_COLOR = { A: '#3b7dd8', AAAA: '#5b7fd6', CNAME: '#2f9e8f', MX: '#6dc8ec', TXT: '#9270ca', NS: '#5d7092', CAA: '#0e7a6e', SRV: '#909399' }
function typeStyle(t) { const c = TYPE_COLOR[t] || '#909399'; return { color: c, borderColor: c + '66', background: c + '14' } }

// 数据源同步的域名，最近同步超 24h 或从未同步 = 需同步。已移出账号(stale)的不再催同步。
function domStale(d) {
  if (d.origin === 'manual' || d.stale) return false
  if (!d.last_synced) return true
  return (Date.now() - new Date(d.last_synced.replace(' ', 'T')).getTime()) > 24 * 3600 * 1000
}
function isExpired(d) { return d && new Date(d) < new Date() }
function expiryClass(d) { if (!d) return ''; const days = (new Date(d) - Date.now()) / 86400000; return days < 0 ? 'exp-red' : (days < 30 ? 'exp-orange' : '') }
const staleCount = computed(() => domains.value.filter(domStale).length)
async function syncOne(row) {
  syncingOne.value = { ...syncingOne.value, [row.ci_id]: true }
  try {
    const r = await syncDomainRecords(row.ci_id)
    ElMessage.success(`同步完成：${r.synced_records} 条 DNS / 新导入 ${r.imported_records || 0} 条`)
    const m = { ...recMap.value }; delete m[row.ci_id]; recMap.value = m // 清缓存，展开时重拉
    await loadDomains()
  } catch (e) {
    ElMessage.error(e.response?.data?.error || '同步失败')
  } finally { syncingOne.value = { ...syncingOne.value, [row.ci_id]: false } }
}

// 主域名列表筛选/分页
const domKeyword = ref(''), domQuery = ref(''), domPage = ref(1), domPageSize = ref(20)
const domFiltered = computed(() => domains.value.filter((d) => !domQuery.value || d.name.toLowerCase().includes(domQuery.value.toLowerCase())))
const domPaged = computed(() => { const s = (domPage.value - 1) * domPageSize.value; return domFiltered.value.slice(s, s + domPageSize.value) })
function doDomSearch() { domQuery.value = domKeyword.value; domPage.value = 1 }

// 每个域名的 DNS 记录（懒加载）+ 各自的筛选/分页状态
const recMap = ref({}), recLoading = ref({}), recState = ref({})
function ensureState() {
  const s = { ...recState.value }
  for (const d of domains.value) if (!s[d.ci_id]) s[d.ci_id] = { type: '', kw: '', page: 1, size: 20 }
  recState.value = s
}
function recFiltered(ciid) {
  const st = recState.value[ciid] || {}
  const kw = st.kw?.toLowerCase()
  return (recMap.value[ciid] || []).filter((r) =>
    (!st.type || r.type === st.type) &&
    (!kw || r.name.toLowerCase().includes(kw) || (r.data || '').toLowerCase().includes(kw)))
}
function recPaged(ciid) {
  const st = recState.value[ciid] || { page: 1, size: 20 }
  const s = (st.page - 1) * st.size
  return recFiltered(ciid).slice(s, s + st.size)
}
async function loadRecords(ciid) {
  recLoading.value = { ...recLoading.value, [ciid]: true }
  try { recMap.value = { ...recMap.value, [ciid]: await listDnsRecords(ciid) } }
  catch (e) { recMap.value = { ...recMap.value, [ciid]: [] } }
  finally { recLoading.value = { ...recLoading.value, [ciid]: false } }
}
function onExpand(row, expandedRows) {
  const open = Array.isArray(expandedRows) ? expandedRows.some((r) => r.ci_id === row.ci_id) : false
  if (open && !recMap.value[row.ci_id]) loadRecords(row.ci_id)
}

async function loadDomains() {
  loading.value = true
  try { domains.value = await listDomains(); ensureState() } catch (e) {} finally { loading.value = false }
}
// 全量同步：后台异步 + 轮询进度。点击瞬间置灰(防连点)+立即查一次状态。
async function doSync() {
  if (!sourceId.value) { ElMessage.warning('先选数据源'); return }
  if (syncing.value) return // 防连点
  syncing.value = true      // 点击瞬间置灰、立即显示"同步中"
  prog.value = { total: 0, done: 0, imported_records: 0 }
  try {
    await syncSource(sourceId.value) // 202：已在后台启动
    ElMessage.success('已在后台同步，完成后自动刷新')
    pollStatus(sourceId.value)
  } catch (e) {
    if (e.response?.status === 409) { pollStatus(sourceId.value) } // 已在同步中，接着轮询
    else { syncing.value = false; ElMessage.error(e.response?.data?.error || '同步启动失败') }
  }
}
function pollStatus(id) {
  if (pollTimer) clearInterval(pollTimer)
  const tick = async () => {
    try {
      const s = await syncSourceStatus(id)
      prog.value = { total: s.total || 0, done: s.done || 0, imported_records: s.imported_records || 0 }
      if (!s.running) {
        clearInterval(pollTimer); pollTimer = null; syncing.value = false
        recMap.value = {}; await loadDomains()
        if (s.error) ElMessage.error('同步出错：' + s.error)
        else if (s.started) ElMessage.success(`同步完成：${s.synced_domains} 个域名 / ${s.synced_records} 条 DNS 记录，导入 ${s.imported_records || 0} 条解析，标记失效 ${s.stale_domains || 0} 个`)
      }
    } catch (e) {}
  }
  tick() // 立即查一次，进度不等 2.5s
  pollTimer = setInterval(tick, 2500)
}
onMounted(async () => {
  sources.value = await listRegistrars(); await loadDomains()
  // 若某数据源正在后台同步（比如刷新了页面），恢复进度显示
  for (const s of sources.value) {
    try { const st = await syncSourceStatus(s.id); if (st.running) { sourceId.value = s.id; syncing.value = true; pollStatus(s.id); break } } catch (e) {}
  }
})
onBeforeUnmount(() => { if (pollTimer) clearInterval(pollTimer) })
</script>

<style scoped>
.filter { display: flex; gap: 10px; align-items: center; }
.reso { padding: 8px 16px 12px; background: #fafbfc; }
.mono { font-family: ui-monospace, Menlo, Consolas, monospace; font-size: 12px; }
.stale { text-decoration: line-through; color: #b0b3bb; }
.muted { color: #909399; }
.exp-orange { color: #e6a23c; font-weight: 600; }
.exp-red { color: #f56c6c; font-weight: 600; }
</style>
