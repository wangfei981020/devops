<template>
  <div class="page">
    <div class="page-head">
      <span class="page-title">到期巡检</span>
      <el-button :icon="Refresh" :loading="loading" @click="load">刷新</el-button>
    </div>

    <!-- 口径说明：本页把三类对象拉平在一起统计，总览的「线上证书 30 天内到期」只数其中的
         线上证书一类，两个数不相等是正常的。不写出来就会被当成数据错误（CMDB-015）。 -->
    <div class="muted" style="margin-bottom:10px; font-size:12px">
      本页统计 <b>域名注册到期 + 线上实测证书 + ACME 签发证书</b> 三类；
      总览页的「线上证书 30 天内到期」只统计其中的<b>线上证书</b>一类，两者数量不同属正常。
      分类口径：已过期 = 剩余 &lt; 0 天，快到期 = 剩余 0~30 天，二者互斥。
    </div>

    <el-card shadow="never" style="margin-bottom:12px">
      <div class="filter">
        <el-select v-model="kindFilter" size="small" style="width:130px">
          <el-option label="全部类型" value="all" />
          <el-option label="域名注册" value="domain" />
          <el-option label="线上证书" value="online" />
          <el-option label="ACME证书" value="acme" />
        </el-select>
        <el-select v-model="domainFilter" size="small" clearable filterable placeholder="域名" style="width:190px">
          <el-option v-for="d in domainOptions" :key="d" :label="d" :value="d" />
        </el-select>
        <el-radio-group v-model="statusFilter" size="small">
          <!-- 计数在加载失败时给 —，不给 0：0 是"确实没有"的断言（CMDB-013） -->
          <el-radio-button v-for="s in statusTabs" :key="s.v" :value="s.v">{{ s.l }}（{{ error ? '—' : counts[s.v] }}）</el-radio-button>
        </el-radio-group>
        <el-input v-model="keyword" placeholder="搜索域名" clearable :prefix-icon="Search" style="width:200px; margin-left:auto" />
        <el-button :icon="sortAsc ? Sort : Sort" @click="sortAsc = !sortAsc">
          到期排序：{{ sortAsc ? '快到期在上' : '快到期在下' }}
        </el-button>
      </div>
    </el-card>

    <!-- 「检测失败」这一类往往是同一个原因批量命中（实测 113 条里全部是 DNS 解析不了），
         逐条列出来等于让人在 113 行相同的报错里翻找。先按原因归类，点一下再看明细。 -->
    <el-card v-if="!error && (statusFilter === 'failed' || statusFilter === 'inapplicable') && failReasons.length"
      shadow="never" style="margin-bottom:14px">
      <template #header><b>{{ statusFilter === 'inapplicable' ? '不适用原因分布' : '失败原因分布' }}</b>
        <span class="muted" style="margin-left:8px">共 {{ failTotal }} 条，归为 {{ failReasons.length }} 类；点一类只看它</span>
        <span v-if="statusFilter === 'failed' && counts.inapplicable" class="muted" style="margin-left:8px">
          另有 <b>{{ counts.inapplicable }}</b> 条解析到内网地址，公网探测本就不适用，已单列到「内网不适用」，不计在这里
        </span>
      </template>
      <div class="reasons">
        <el-tag v-for="r in failReasons" :key="r.key" size="large" effect="plain" class="reason"
          :type="reasonFilter === r.key ? 'danger' : 'info'" @click="reasonFilter = reasonFilter === r.key ? '' : r.key">
          {{ r.label }} <b style="margin-left:6px">{{ r.n }}</b>
        </el-tag>
        <el-button v-if="reasonFilter" link type="primary" style="margin-left:8px" @click="reasonFilter = ''">清除筛选</el-button>
      </div>
    </el-card>

    <el-card shadow="never">
      <LoadError :error="error" @retry="load" />
      <el-table v-if="!error" :data="paged" size="small" v-loading="loading">
        <el-table-column label="完整域名" min-width="220"><template #default="{ row }"><span class="mono">{{ row.fqdn }}</span></template></el-table-column>
        <el-table-column prop="domain" label="所属主域名" min-width="160" />
        <el-table-column label="域名状态" width="130"><template #default="{ row }">
          <el-tag v-if="row.domain_status" size="small" effect="plain" :style="chip(app.statusColor(row.domain_status))">{{ row.domain_status }}</el-tag>
          <span v-else class="muted">—</span>
        </template></el-table-column>
        <el-table-column label="类型" width="110"><template #default="{ row }">
          <el-tag size="small" :type="KIND[row.kind].tag">{{ KIND[row.kind].l }}</el-tag>
        </template></el-table-column>
        <el-table-column label="到期" width="120"><template #default="{ row }">{{ row.expiry_at || '—' }}</template></el-table-column>
        <el-table-column label="剩余" width="100"><template #default="{ row }">
          <span v-if="row._st==='ok' || row._st==='expiring'" :style="{color: row._days<=7?'#f56c6c':(row._days<=30?'#e6a23c':'#67c23a'), fontWeight:600}">{{ row._days }} 天</span>
          <span v-else-if="row._st==='expired'" style="color:#f56c6c; font-weight:600">已过期 {{ -row._days }} 天</span>
          <span v-else class="muted">—</span>
        </template></el-table-column>
        <el-table-column label="状态" width="130"><template #default="{ row }">
          <!-- 失败时把原因挂在标签上。接口一直带着 check_msg，页面却一个字不显示
               （状态格只有一个"检测失败"标签，无 title 无 tooltip），
               于是"为什么失败"只能靠上面的归类卡片猜（CMDB-041）。 -->
          <el-tooltip v-if="row._st === 'failed' && row.check_msg" placement="top" :content="row.check_msg">
            <el-tag size="small" type="danger" class="has-why">{{ ST[row._st].l }}</el-tag>
          </el-tooltip>
          <el-tag v-else size="small" :type="ST[row._st].tag">{{ ST[row._st].l }}</el-tag>
        </template></el-table-column>
        <el-table-column label="原因" min-width="260"><template #default="{ row }">
          <!-- 一列同时承载两种原因：失败原因（check_msg）和忽略原因。
               它们互斥，合成一列比空着半列强。
               归类结果放前面（一眼能分组），原始报文收进 tooltip（排查时才需要）。 -->
          <template v-if="(row._st === 'failed' || row._st === 'inapplicable') && row.check_msg">
            <el-tag size="small" effect="plain" :type="row._st === 'inapplicable' ? 'info' : 'danger'">
              {{ reasonOfRow(row).label }}
            </el-tag>
            <el-tooltip placement="top" :content="row.check_msg">
              <span class="fail-why">{{ row.check_msg }}</span>
            </el-tooltip>
          </template>
          <span v-else-if="row.ignored" class="muted">{{ row.ignore_reason || '—' }}</span>
          <span v-else class="muted">—</span>
        </template></el-table-column>
        <el-table-column label="操作" width="120" fixed="right"><template #default="{ row }">
          <div style="display:flex;gap:8px;align-items:center">
            <template v-if="row.kind==='online'">
              <el-tooltip v-if="canCert && !row.ignored" content="标记无需证书"><el-button link type="info" :icon="Hide" @click="openIgnore(row)" /></el-tooltip>
              <el-tooltip v-else-if="canCert" content="取消无需证书"><el-button link type="primary" :icon="View" @click="unignore(row)" /></el-tooltip>
              <el-tooltip content="去域名页"><el-button link type="primary" :icon="Link" @click="$router.push('/domains')" /></el-tooltip>
            </template>
            <el-tooltip v-else-if="row.kind==='domain'" content="去域名页"><el-button link type="primary" :icon="Link" @click="$router.push('/domains')" /></el-tooltip>
            <el-tooltip v-else content="去证书页"><el-button link type="primary" :icon="Link" @click="$router.push('/certs')" /></el-tooltip>
          </div>
        </template></el-table-column>
      </el-table>
      <el-pagination v-if="!error" v-model:current-page="page" v-model:page-size="size" :page-sizes="[20,50,100,200]"
        :total="filtered.length" layout="total, sizes, prev, pager, next" style="margin-top:12px; justify-content:flex-end" />
    </el-card>

    <el-dialog :close-on-click-modal="false" :close-on-press-escape="false" v-model="igDlg" title="标记无需证书" width="460px">
      <div class="muted" style="margin-bottom:10px">{{ igRow?.fqdn }} —— 标记为不需要证书，将从「快到期/检测失败」中排除，可在「无需证书」里查看。</div>
      <el-form label-width="70px">
        <el-form-item label="原因"><el-input v-model="igReason" type="textarea" :rows="2" placeholder="如：内网解析 / 邮件 MX / 纯跳转，无需证书" /></el-form-item>
      </el-form>
      <template #footer><el-button @click="igDlg=false">取消</el-button><el-button type="primary" @click="confirmIgnore">确认</el-button></template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, watch, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { useAuthStore } from '../stores/auth'
import { ElMessage } from 'element-plus'
import { Refresh, Search, Sort, Hide, View, Link } from '@element-plus/icons-vue'
import { listCertInspect, recordCertIgnore } from '../api/cmdb'
import { useAppStore } from '../stores/app'
import { useLoadState } from '../composables/useLoadState'
import LoadError from '../components/LoadError.vue'

const route = useRoute()
const auth = useAuthStore()
const canCert = computed(() => auth.hasButton('manage_certs'))
const { loading, error, run } = useLoadState()

const app = useAppStore()
function chip(c) { return c ? { color: c, borderColor: c + '66', background: c + '14' } : {} }

const KIND = {
  domain: { l: '域名注册', tag: 'primary' },
  online: { l: '线上证书', tag: 'info' },
  acme:   { l: 'ACME证书', tag: 'success' },
}
const ST = {
  ok:        { l: '正常',     tag: 'success' },
  expiring:  { l: '30天内到期', tag: 'warning' },
  expired:   { l: '已过期',   tag: 'danger' },
  failed:    { l: '检测失败', tag: 'danger' },
  // 内网地址被公网巡检器探测，连不上是必然的——这不是故障，混进"检测失败"
  // 会让那个数字整列没法用（生产 148 条里约 70 条是这类，CMDB-041）
  inapplicable: { l: '内网不适用', tag: 'info' },
  ignored:   { l: '无需证书',  tag: 'primary' },
  unknown:   { l: '未检测',   tag: 'info' },
}
const statusTabs = [
  { v: 'all', l: '全部' }, { v: 'expiring', l: '快到期' }, { v: 'expired', l: '已过期' },
  { v: 'failed', l: '检测失败' }, { v: 'inapplicable', l: '内网不适用' },
  { v: 'ignored', l: '无需证书' }, { v: 'ok', l: '正常' }, { v: 'unknown', l: '未检测' },
]

const rows = ref([])
const statusFilter = ref('all'), kindFilter = ref('all'), keyword = ref(''), sortAsc = ref(true)
// 失败原因筛选：只在「检测失败」tab 生效；切换 tab 时清掉，免得带着看不见的筛选条件
const reasonFilter = ref('')
const domainFilter = ref(null)
const domainOptions = computed(() => [...new Set(rows.value.map((r) => r.domain).filter(Boolean))].sort())
const page = ref(1), size = ref(50)
const igDlg = ref(false), igRow = ref(null), igReason = ref('')

function daysTo(d) { return Math.ceil((new Date(d) - Date.now()) / 86400000) }
// 分类定义（与后端 dashboard.go 逐字对齐，CMDB-015）：
//   已过期 = days < 0        快到期 = 0 <= days <= 30       正常 = days > 30
// 三者互斥。此前这里写的是 d < 30、后端写的是 SQL 秒级比较，边界项两边归类不同，
// 于是总览显示 158、本页显示 162，同一时刻同一指标对不上。
function statusOf(r) {
  if (r.ignored) return 'ignored'
  // scope 由后端判定（tls_error.go），前端不再自行推导内网——
  // 判据是 origin_ip，前端本来就拿不到，硬猜必然和后端不一致
  if (!r.expiry_at && r.check_msg && r.scope === 'internal') return 'inapplicable'
  if (!r.expiry_at) return r.check_msg ? 'failed' : 'unknown'
  const d = daysTo(r.expiry_at)
  if (d < 0) return 'expired'
  if (d <= 30) return 'expiring'
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
    if (kindFilter.value !== 'all' && r.kind !== kindFilter.value) continue
    c[r._st] = (c[r._st] || 0) + 1
    if (r._st !== 'ignored') c.all++ // 「全部」不含已忽略
  }
  return c
})
// 把一条巡检项归纳成"原因类别"。
//
//	归类的**权威在后端**（handlers/tls_error.go）：只有它能看到 origin_ip，
//	也只有它算出来的结果 MCP/AI 才拿得到。这里优先用后端给的 reason_key，
//	下面那套正则只作为旧数据的兜底——接口升级前采的记录没有这两个字段。
function reasonOfRow(r) {
  if (r.reason_key) return { key: r.reason_key, label: r.reason_label || r.reason_key }
  return reasonOf(r.check_msg)
}
// 兜底：原始消息带着域名和 IP，逐字分组会得到和条数一样多的组（每条都不同），
// 起不到归纳作用——必须先把可变部分抽掉。
function reasonOf(msg) {
  const m = (msg || '').trim()
  if (!m) return { key: 'unknown', label: '未记录原因' }
  if (/no such host|lookup .* on .*: /i.test(m)) return { key: 'dns', label: 'DNS 解析不到该域名' }
  if (/i\/o timeout|context deadline exceeded/i.test(m)) return { key: 'timeout', label: '连接 443 超时' }
  if (/connection refused/i.test(m)) return { key: 'refused', label: '443 端口拒绝连接' }
  if (/certificate has expired|x509: certificate/i.test(m)) return { key: 'x509', label: '证书链校验失败' }
  if (/handshake|tls/i.test(m)) return { key: 'tls', label: 'TLS 握手失败' }
  if (/no route to host|network is unreachable/i.test(m)) return { key: 'unreachable', label: '网络不可达' }
  // 认不出来的保留原文前缀，并且**必须能看见**——闷头归到"其他"会掩盖新出现的失败模式
  return { key: 'other:' + m.slice(0, 40), label: m.slice(0, 40) }
}
// 原因分布卡片对「检测失败」和「内网不适用」两个 tab 都要能用——
// 后者也需要看清是哪些域名、为什么被判成内网
const failItems = computed(() => enriched.value.filter((r) => r._st === statusFilter.value))
const failTotal = computed(() => failItems.value.length)
const failReasons = computed(() => {
  const m = new Map()
  for (const r of failItems.value) {
    const { key, label } = reasonOfRow(r)
    const cur = m.get(key) || { key, label, n: 0 }
    cur.n++
    m.set(key, cur)
  }
  return [...m.values()].sort((a, b) => b.n - a.n)
})

watch(statusFilter, () => { reasonFilter.value = '' })

const filtered = computed(() => {
  let list = enriched.value
  if (domainFilter.value) list = list.filter((r) => r.domain === domainFilter.value)
  if (kindFilter.value !== 'all') list = list.filter((r) => r.kind === kindFilter.value)
  if (reasonFilter.value) list = list.filter((r) => reasonOfRow(r).key === reasonFilter.value)
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
  await run(async () => { rows.value = await listCertInspect() })
  if (error.value) rows.value = []
}
function openIgnore(row) { igRow.value = row; igReason.value = ''; igDlg.value = true }
async function confirmIgnore() {
  try { await recordCertIgnore(igRow.value.record_id, { ignored: true, reason: igReason.value }); ElMessage.success('已标记无需证书'); igDlg.value = false; load() }
  catch (e) { ElMessage.error('操作失败') }
}
async function unignore(row) {
  try { await recordCertIgnore(row.record_id, { ignored: false }); ElMessage.success('已取消'); load() }
  catch (e) { ElMessage.error('操作失败') }
}
// 从总览的卡片跳过来时带着 ?status=&kind=，落地要直接停在那一类上。
// 只认已知的枚举值，别人手改 URL 也不会把页面带进一个不存在的 tab。
function applyQuery(q) {
  if (q.status && statusTabs.some((s) => s.v === q.status)) statusFilter.value = q.status
  if (q.kind && ['all', 'domain', 'online', 'acme'].includes(q.kind)) kindFilter.value = q.kind
}
onMounted(() => {
  applyQuery(route.query)
  load()
  if (!app.statuses.length) app.loadStatuses()
})
// 同一页面内二次跳转（已经在本页时再点总览卡片）query 变了但组件不重建
watch(() => route.query, (q) => applyQuery(q))
</script>

<style scoped>
/* 归类标签在前、原文在后：原文一行放不下就截断，完整内容在 tooltip 里 */
.fail-why { color: var(--el-text-color-secondary); font-size: 12px; margin-left: 6px;
  display: inline-block; max-width: 100%; vertical-align: bottom;
  white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.has-why { cursor: help; border-bottom: 1px dashed currentColor; }
.filter { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; }
.mono { font-family: ui-monospace, Menlo, Consolas, monospace; font-size: 12px; }
.reasons { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; }
.reason { cursor: pointer; }
</style>
