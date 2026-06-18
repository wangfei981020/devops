<template>
  <div class="page">
    <div class="page-head">
      <span class="page-title">证书巡检</span>
      <el-button :icon="Refresh" :loading="loading" @click="load">刷新</el-button>
    </div>

    <el-card shadow="never" style="margin-bottom:12px">
      <div class="filter">
        <el-radio-group v-model="statusFilter" size="small">
          <el-radio-button v-for="s in statusTabs" :key="s.v" :value="s.v">{{ s.l }}（{{ counts[s.v] }}）</el-radio-button>
        </el-radio-group>
        <el-input v-model="keyword" placeholder="搜索域名" clearable :prefix-icon="Search" style="width:200px; margin-left:auto" />
        <el-button :icon="sortAsc ? Sort : Sort" @click="sortAsc = !sortAsc">
          到期排序：{{ sortAsc ? '快到期在上' : '快到期在下' }}
        </el-button>
      </div>
    </el-card>

    <el-card shadow="never">
      <el-table :data="paged" size="small" v-loading="loading">
        <el-table-column label="完整域名" min-width="220"><template #default="{ row }"><span class="mono">{{ row.fqdn }}</span></template></el-table-column>
        <el-table-column prop="domain" label="所属主域名" min-width="160" />
        <el-table-column label="类型" width="100"><template #default="{ row }">
          <el-tag size="small" :type="row.kind==='acme'?'success':'info'">{{ row.kind==='acme'?'ACME签发':'线上检测' }}</el-tag>
        </template></el-table-column>
        <el-table-column label="证书到期" width="120"><template #default="{ row }">{{ row.expiry_at || '—' }}</template></el-table-column>
        <el-table-column label="剩余" width="100"><template #default="{ row }">
          <span v-if="row._st==='ok' || row._st==='expiring'" :style="{color: row._days<=7?'#f56c6c':(row._days<=30?'#e6a23c':'#67c23a'), fontWeight:600}">{{ row._days }} 天</span>
          <span v-else-if="row._st==='expired'" style="color:#f56c6c; font-weight:600">已过期 {{ -row._days }} 天</span>
          <span v-else class="muted">—</span>
        </template></el-table-column>
        <el-table-column label="状态" width="130"><template #default="{ row }">
          <el-tag size="small" :type="ST[row._st].tag">{{ ST[row._st].l }}</el-tag>
        </template></el-table-column>
        <el-table-column label="忽略原因" min-width="160" show-overflow-tooltip><template #default="{ row }">
          <span v-if="row.ignored" class="muted">{{ row.ignore_reason || '—' }}</span>
        </template></el-table-column>
        <el-table-column label="操作" width="180" fixed="right"><template #default="{ row }">
          <template v-if="row.kind==='online'">
            <el-button v-if="!row.ignored" link type="info" @click="openIgnore(row)">忽略</el-button>
            <el-button v-else link type="primary" @click="unignore(row)">取消忽略</el-button>
            <el-button link type="primary" @click="$router.push('/domains')">去域名页</el-button>
          </template>
          <el-button v-else link type="primary" @click="$router.push('/certs')">去证书页</el-button>
        </template></el-table-column>
      </el-table>
      <el-pagination v-model:current-page="page" v-model:page-size="size" :page-sizes="[20,50,100,200]"
        :total="filtered.length" layout="total, sizes, prev, pager, next" style="margin-top:12px; justify-content:flex-end" />
    </el-card>

    <el-dialog v-model="igDlg" title="忽略证书巡检" width="460px">
      <div class="muted" style="margin-bottom:10px">{{ igRow?.fqdn }} —— 标记为不需要证书，将从「快到期/检测失败」中排除，可在「已忽略」里查看。</div>
      <el-form label-width="70px">
        <el-form-item label="原因"><el-input v-model="igReason" type="textarea" :rows="2" placeholder="如：内网解析 / 邮件 MX / 纯跳转，无需证书" /></el-form-item>
      </el-form>
      <template #footer><el-button @click="igDlg=false">取消</el-button><el-button type="primary" @click="confirmIgnore">确认忽略</el-button></template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Refresh, Search, Sort } from '@element-plus/icons-vue'
import { listCertInspect, recordCertIgnore } from '../api/cmdb'

const ST = {
  ok:        { l: '正常',     tag: 'success' },
  expiring:  { l: '30天内到期', tag: 'warning' },
  expired:   { l: '已过期',   tag: 'danger' },
  failed:    { l: '检测失败', tag: 'danger' },
  ignored:   { l: '已忽略',   tag: 'info' },
  unknown:   { l: '未检测',   tag: 'info' },
}
const statusTabs = [
  { v: 'all', l: '全部' }, { v: 'expiring', l: '快到期' }, { v: 'expired', l: '已过期' },
  { v: 'failed', l: '检测失败' }, { v: 'ignored', l: '已忽略' }, { v: 'ok', l: '正常' }, { v: 'unknown', l: '未检测' },
]

const rows = ref([]), loading = ref(false)
const statusFilter = ref('all'), keyword = ref(''), sortAsc = ref(true)
const page = ref(1), size = ref(50)
const igDlg = ref(false), igRow = ref(null), igReason = ref('')

function daysTo(d) { return Math.ceil((new Date(d) - Date.now()) / 86400000) }
function statusOf(r) {
  if (r.ignored) return 'ignored'
  if (!r.expiry_at) return r.check_msg ? 'failed' : 'unknown'
  const d = daysTo(r.expiry_at)
  if (d < 0) return 'expired'
  if (d < 30) return 'expiring'
  return 'ok'
}
const enriched = computed(() => rows.value.map((r) => {
  const st = statusOf(r)
  return { ...r, _st: st, _days: r.expiry_at ? daysTo(r.expiry_at) : null }
}))
const counts = computed(() => {
  const c = { all: 0 }
  for (const s of statusTabs) if (s.v !== 'all') c[s.v] = 0
  for (const r of enriched.value) {
    c[r._st] = (c[r._st] || 0) + 1
    if (r._st !== 'ignored') c.all++ // 「全部」不含已忽略
  }
  return c
})
const filtered = computed(() => {
  let list = enriched.value
  // 「全部」= 未忽略的巡检项；已忽略的只在「已忽略」tab 看
  if (statusFilter.value === 'all') list = list.filter((r) => r._st !== 'ignored')
  else list = list.filter((r) => r._st === statusFilter.value)
  if (keyword.value) { const k = keyword.value.toLowerCase(); list = list.filter((r) => r.fqdn.toLowerCase().includes(k) || r.domain.toLowerCase().includes(k)) }
  // 排序：有到期日的按到期排，无到期日(失败/未检测)排最后
  list = [...list].sort((a, b) => {
    if (!a.expiry_at && !b.expiry_at) return 0
    if (!a.expiry_at) return 1
    if (!b.expiry_at) return -1
    return sortAsc.value ? new Date(a.expiry_at) - new Date(b.expiry_at) : new Date(b.expiry_at) - new Date(a.expiry_at)
  })
  return list
})
const paged = computed(() => { const s = (page.value - 1) * size.value; return filtered.value.slice(s, s + size.value) })

async function load() {
  loading.value = true
  try { rows.value = await listCertInspect() } catch (e) { rows.value = [] } finally { loading.value = false }
}
function openIgnore(row) { igRow.value = row; igReason.value = ''; igDlg.value = true }
async function confirmIgnore() {
  try { await recordCertIgnore(igRow.value.record_id, { ignored: true, reason: igReason.value }); ElMessage.success('已忽略'); igDlg.value = false; load() }
  catch (e) { ElMessage.error('操作失败') }
}
async function unignore(row) {
  try { await recordCertIgnore(row.record_id, { ignored: false }); ElMessage.success('已取消忽略'); load() }
  catch (e) { ElMessage.error('操作失败') }
}
onMounted(load)
</script>

<style scoped>
.filter { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; }
.mono { font-family: ui-monospace, Menlo, Consolas, monospace; font-size: 12px; }
</style>
