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
        <el-input v-model="df.kw.value" placeholder="搜索主域名" clearable :prefix-icon="Search" style="width:160px" @keyup.enter="doDomSearch" />
        <el-select v-model="domNameFilter" clearable filterable placeholder="域名" style="width:200px" @change="domPage=1">
          <el-option v-for="d in domNameOptions" :key="d" :label="d" :value="d" />
        </el-select>
        <el-select v-model="df.view.value" style="width:150px" @change="domPage=1">
          <el-option v-for="o in df.viewOptions.value" :key="o.value" :label="`${o.label}（${o.count}）`" :value="o.value" />
        </el-select>
        <el-select v-model="df.src.value" clearable placeholder="来源" style="width:130px" @change="domPage=1">
          <el-option v-for="s in df.sourceOptions.value" :key="s" :label="s" :value="s">
            <span :style="{ display:'inline-block', width:'8px', height:'8px', borderRadius:'50%', background: registrarColor(s), marginRight:'6px' }" />{{ s }}
          </el-option>
        </el-select>
        <el-select v-model="df.expiry.value" clearable placeholder="到期" style="width:120px" @change="domPage=1">
          <el-option label="🔴 已过期" value="expired" />
          <el-option label="🟠 30天内" value="soon" />
          <el-option label="🟢 正常" value="normal" />
        </el-select>
        <el-button type="primary" :icon="Search" @click="doDomSearch">搜索</el-button>
        <el-button @click="resetDomFilter">重置</el-button>
        <el-button v-if="selectedDoms.length && df.view.value!=='ignored'" type="warning" :icon="Hide" @click="ignoreDoms(selectedDoms)">批量忽略（{{ selectedDoms.length }}）</el-button>
        <el-button v-if="selectedDoms.length && df.view.value==='ignored'" type="success" @click="unignoreDoms(selectedDoms)">取消忽略（{{ selectedDoms.length }}）</el-button>
        <span class="muted" style="margin-left:auto">共 {{ domFiltered.length }} 个域名</span>
      </div>
    </el-card>

    <el-card shadow="never">
      <el-table :data="domPaged" size="small" row-key="ci_id" v-loading="loading" @expand-change="onExpand"
        @selection-change="(v) => selectedDoms = v">
        <el-table-column type="selection" width="42" reserve-selection />
        <el-table-column type="expand">
          <template #default="{ row }">
            <div class="reso" v-loading="recLoading[row.ci_id]">
              <div class="filter" style="margin-bottom:8px">
                <el-select v-model="recState[row.ci_id].type" clearable placeholder="类型" size="small" style="width:120px">
                  <el-option v-for="t in types" :key="t" :label="t" :value="t" />
                </el-select>
                <el-input v-model="recState[row.ci_id].kw" placeholder="搜索 主机/值" clearable size="small" style="width:220px" />
                <span class="muted">本域名 {{ recFiltered(row.ci_id).length }} 条</span>
                <el-tooltip v-if="!canWrite(row)" content="该域名未绑数据源或来源不支持写回，无法新增">
                  <span><el-button size="small" type="primary" :icon="Plus" disabled>新增解析</el-button></span>
                </el-tooltip>
                <template v-else>
                  <el-button size="small" :icon="DocumentAdd" style="margin-left:auto" @click="openBatch(row)">批量新增</el-button>
                  <el-button size="small" type="primary" :icon="Plus" @click="openCreate(row)">新增解析</el-button>
                </template>
              </div>
              <el-table :data="recPaged(row.ci_id)" size="small" max-height="360" row-key="id"
                @selection-change="(v) => onRecSel(row.ci_id, v)">
                <el-table-column type="selection" width="40" :selectable="recSelectable" reserve-selection />
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
                <el-table-column label="操作" width="120"><template #default="{ row: r }">
                  <template v-if="!r.protected && writableTypes.includes(r.type) && canWrite(row)">
                    <el-button link type="primary" :icon="Edit" @click="openEdit(row, r)">编辑</el-button>
                    <el-button link type="danger" :icon="Delete" :loading="delBusy[r.id]" @click="delRecord(row, r)">删除</el-button>
                  </template>
                  <span v-else class="muted">—</span>
                </template></el-table-column>
              </el-table>
              <div v-if="(recSel[row.ci_id] || []).length" class="batch-bar">
                <span>已选 <b>{{ recSel[row.ci_id].length }}</b> 条</span>
                <el-button size="small" type="primary" :icon="Edit" @click="openBatchEdit(row)">批量编辑</el-button>
                <el-button size="small" type="danger" :icon="Delete" :loading="batchDelBusy[row.ci_id]" @click="doBatchDelete(row)">批量删除</el-button>
              </div>
              <el-empty v-if="!(recMap[row.ci_id] && recMap[row.ci_id].length) && !recLoading[row.ci_id]"
                description="该域名暂无 DNS 记录，选好数据源点右上「从数据源同步」" :image-size="50" />
              <el-pagination v-else small background v-model:current-page="recState[row.ci_id].page" v-model:page-size="recState[row.ci_id].size"
                :page-sizes="[20,50,100]" :total="recFiltered(row.ci_id).length" layout="total, sizes, prev, pager, next"
                style="margin-top:8px; justify-content:flex-end" />
            </div>
          </template>
        </el-table-column>
        <el-table-column label="主域名" min-width="230"><template #default="{ row }">
          <span :class="{ stale: row.category !== 'active' && row.category !== 'pending' }">{{ row.name }}</span>
        </template></el-table-column>
        <el-table-column label="状态" width="120"><template #default="{ row }">
          <el-tooltip :disabled="!row.source_status" :content="'GoDaddy 状态：' + row.source_status">
            <el-tag size="small" :style="domainCatStyle(row.category)">{{ domainCatLabel(row.category) }}</el-tag>
          </el-tooltip>
        </template></el-table-column>
        <el-table-column label="来源" width="120"><template #default="{ row }">
          <el-tag v-if="row.origin === 'manual'" type="info" size="small">手动录入</el-tag>
          <el-tag v-else size="small" :style="registrarStyle(row.registrar_name)">{{ row.registrar_name || '同步' }}</el-tag>
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
        <el-table-column label="操作" width="150" fixed="right"><template #default="{ row }">
          <div style="display:flex;gap:8px;align-items:center">
            <el-tooltip v-if="row.origin !== 'manual' && !row.ignored" content="从数据源同步这个域名">
              <el-button link type="primary" :icon="Refresh" :loading="syncingOne[row.ci_id]" @click="syncOne(row)">同步</el-button>
            </el-tooltip>
            <el-tooltip v-if="!row.ignored" content="忽略（同步跳过、不报未同步）"><el-button link type="warning" :icon="Hide" @click="ignoreDoms([row])" /></el-tooltip>
            <el-tooltip v-else content="取消忽略"><el-button link type="success" :icon="RefreshLeft" @click="unignoreDoms([row])" /></el-tooltip>
          </div>
        </template></el-table-column>
      </el-table>
      <el-pagination v-model:current-page="domPage" v-model:page-size="domPageSize" :page-sizes="[10,20,50,100]"
        :total="domFiltered.length" layout="total, sizes, prev, pager, next" style="margin-top:12px; justify-content:flex-end" />
    </el-card>

    <!-- 解析写回 GoDaddy：新增/编辑（只点 保存/关闭/X 关闭，不点外部关） -->
    <el-dialog v-model="dlg.show" :title="dlg.editId ? '编辑解析（写回 GoDaddy）' : '新增解析（写回 GoDaddy）'"
      width="520px" :close-on-click-modal="false">
      <el-form label-width="88px" @submit.prevent>
        <el-form-item label="主域名"><span class="mono">{{ dlg.domainName }}</span></el-form-item>
        <el-form-item label="类型">
          <el-select v-model="dlg.form.type" :disabled="!!dlg.editId" style="width:140px">
            <el-option v-for="t in writableTypes" :key="t" :label="t" :value="t" />
          </el-select>
          <span class="muted" style="margin-left:8px">仅支持 A/AAAA/CNAME/TXT/MX</span>
        </el-form-item>
        <el-form-item label="主机名">
          <el-input v-model="dlg.form.name" :disabled="!!dlg.editId" placeholder="如 www、api，根记录填 @" style="width:220px" />
        </el-form-item>
        <el-form-item label="记录值">
          <el-input v-model="dlg.form.data" placeholder="A→IP / CNAME→目标域名 / TXT→文本" style="width:320px" />
        </el-form-item>
        <el-form-item label="TTL">
          <el-input-number v-model="dlg.form.ttl" :min="600" :step="600" controls-position="right" style="width:140px" />
          <span class="muted" style="margin-left:8px">秒，最小 600</span>
        </el-form-item>
        <el-form-item v-if="dlg.form.type === 'MX'" label="优先级">
          <el-input-number v-model="dlg.form.priority" :min="0" :max="65535" controls-position="right" style="width:140px" />
        </el-form-item>
        <el-alert type="warning" :closable="false" show-icon
          :title="`保存后立即写回 ${dlg.env}，并回拉核对`" style="margin-left:0" />
      </el-form>
      <template #footer>
        <el-button @click="dlg.show = false">关闭</el-button>
        <el-button type="primary" :loading="dlg.saving" @click="saveRecord">保存并写回</el-button>
      </template>
    </el-dialog>

    <!-- 批量新增解析（多行表格 + 粘贴导入，一次写回 GoDaddy） -->
    <el-dialog v-model="batch.show" title="批量新增解析（写回 GoDaddy）" width="780px" :close-on-click-modal="false">
      <div style="margin-bottom:8px">
        主域名 <span class="mono">{{ batch.domainName }}</span>
        <span class="muted" style="margin-left:8px">仅支持 A/AAAA/CNAME/TXT/MX；受保护记录会被跳过</span>
      </div>
      <el-table :data="batch.rows" size="small" max-height="300">
        <el-table-column label="类型" width="110"><template #default="{ row }">
          <el-select v-model="row.type" size="small">
            <el-option v-for="t in writableTypes" :key="t" :label="t" :value="t" />
          </el-select>
        </template></el-table-column>
        <el-table-column label="主机名" width="150"><template #default="{ row }">
          <el-input v-model="row.name" size="small" placeholder="www / @" />
        </template></el-table-column>
        <el-table-column label="记录值"><template #default="{ row }">
          <el-input v-model="row.data" size="small" placeholder="IP / 目标域名 / 文本" />
        </template></el-table-column>
        <el-table-column label="TTL" width="90"><template #default="{ row }">
          <el-input v-model.number="row.ttl" size="small" />
        </template></el-table-column>
        <el-table-column label="优先级" width="80"><template #default="{ row }">
          <el-input v-if="row.type==='MX'" v-model.number="row.priority" size="small" />
          <span v-else class="muted">—</span>
        </template></el-table-column>
        <el-table-column width="44"><template #default="{ $index }">
          <el-button link type="danger" :icon="Delete" @click="batch.rows.splice($index,1)" />
        </template></el-table-column>
      </el-table>
      <div style="margin-top:8px">
        <el-button size="small" :icon="Plus" @click="batch.rows.push({ type:'A', name:'', data:'', ttl:600, priority:10 })">加一行</el-button>
        <el-button size="small" @click="batch.rows=[]">清空</el-button>
        <el-button size="small" text @click="batch.showPaste=!batch.showPaste">{{ batch.showPaste?'收起粘贴':'粘贴导入' }}</el-button>
      </div>
      <div v-if="batch.showPaste" style="margin-top:8px">
        <el-input v-model="batch.pasteText" type="textarea" :rows="5"
          placeholder="每行一条，空格或逗号分隔：类型 主机名 值 [TTL]&#10;例：&#10;A     www   1.2.3.4   600&#10;A     api   1.2.3.5&#10;CNAME blog  x.cf.com&#10;TXT   @     v=spf1..." />
        <el-button size="small" type="primary" style="margin-top:6px" @click="doPaste">解析并填入表格</el-button>
        <span class="muted" style="margin-left:8px">TTL 可省，默认 600</span>
      </div>
      <el-alert type="warning" :closable="false" show-icon style="margin-top:10px"
        :title="`将新增 ${batch.rows.length} 条并写回 GoDaddy，保存后立即生效`" />
      <template #footer>
        <el-button @click="batch.show=false">关闭</el-button>
        <el-button type="primary" :loading="batch.saving" @click="saveBatch">批量写回（{{ batch.rows.length }} 条）</el-button>
      </template>
    </el-dialog>

    <!-- 批量编辑解析（可编辑网格 + 统一设 TTL，写回 GoDaddy） -->
    <el-dialog v-model="bedit.show" title="批量编辑解析（写回 GoDaddy）" width="760px" :close-on-click-modal="false">
      <div style="margin-bottom:8px">
        主域名 <span class="mono">{{ bedit.domainName }}</span>
        <span class="muted" style="margin-left:8px">类型/主机名不可改（改这些请删了重加）；受保护记录已排除</span>
      </div>
      <div class="filter" style="margin-bottom:8px">
        统一设 TTL <el-input-number v-model="bedit.bulkTtl" :min="600" :step="600" size="small" controls-position="right" style="width:130px" />
        <el-button size="small" @click="applyBulkTtl">应用到全部</el-button>
      </div>
      <el-table :data="bedit.rows" size="small" max-height="320">
        <el-table-column label="类型" width="90"><template #default="{ row }">
          <el-tag size="small" effect="plain" :style="typeStyle(row.type)">{{ row.type }}</el-tag>
        </template></el-table-column>
        <el-table-column label="主机名" width="150"><template #default="{ row }"><span class="mono">{{ row.name }}</span></template></el-table-column>
        <el-table-column label="记录值"><template #default="{ row }">
          <el-input v-model="row.data" size="small" />
        </template></el-table-column>
        <el-table-column label="TTL" width="100"><template #default="{ row }">
          <el-input v-model.number="row.ttl" size="small" />
        </template></el-table-column>
        <el-table-column label="优先级" width="80"><template #default="{ row }">
          <el-input v-if="row.type==='MX'" v-model.number="row.priority" size="small" />
          <span v-else class="muted">—</span>
        </template></el-table-column>
      </el-table>
      <el-alert type="warning" :closable="false" show-icon style="margin-top:10px"
        :title="`将编辑 ${bedit.rows.length} 条并写回 GoDaddy`" />
      <template #footer>
        <el-button @click="bedit.show=false">关闭</el-button>
        <el-button type="primary" :loading="bedit.saving" @click="saveBatchEdit">批量写回（{{ bedit.rows.length }} 条）</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted, onBeforeUnmount } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Refresh, Search, Hide, RefreshLeft, Plus, Edit, Delete, DocumentAdd } from '@element-plus/icons-vue'
import { listDomains, listRegistrars, listDnsRecords, syncSource, syncSourceStatus, syncDomainRecords, bulkIgnoreDomains,
  createDnsRecord, updateDnsRecord, deleteDnsRecord, batchCreateDnsRecords, batchDeleteDnsRecords, batchUpdateDnsRecords } from '../api/cmdb'
import { registrarStyle, registrarColor, domainCatLabel, domainCatStyle } from '../utils/cloud'
import { useDomainFilter } from '../composables/useDomainFilter'
import { useAppStore } from '../stores/app'
const app = useAppStore()
const sources = ref([]), domains = ref([]), loading = ref(false)
const sourceId = ref(null)
const syncing = ref(false)
const prog = ref({ total: 0, done: 0, imported_records: 0 })
const syncingOne = ref({})
const selectedDoms = ref([])
let pollTimer = null
const types = ['A', 'AAAA', 'CNAME', 'MX', 'TXT', 'NS', 'CAA', 'SRV']
// DNS 记录类型固定配色（冷色/中性，红橙留给到期告警）
const TYPE_COLOR = { A: '#3b7dd8', AAAA: '#5b7fd6', CNAME: '#2f9e8f', MX: '#6dc8ec', TXT: '#9270ca', NS: '#5d7092', CAA: '#0e7a6e', SRV: '#909399' }
function typeStyle(t) { const c = TYPE_COLOR[t] || '#909399'; return { color: c, borderColor: c + '66', background: c + '14' } }

// 数据源同步的域名，最近同步超 24h 或从未同步 = 需同步。已移出账号/已忽略的不再催同步。
function domStale(d) {
  if (d.origin === 'manual' || d.stale || d.ignored) return false
  if (!d.last_synced) return true
  return (Date.now() - new Date(d.last_synced.replace(' ', 'T')).getTime()) > 24 * 3600 * 1000
}
function isExpired(d) { return d && new Date(d) < new Date() }
function expiryClass(d) { if (!d) return ''; const days = (new Date(d) - Date.now()) / 86400000; return days < 0 ? 'exp-red' : (days < 30 ? 'exp-orange' : '') }
// 域名状态：ignored=已忽略；abnormal=已过期/已移出账号/DNS已迁移；否则活跃
function isAbnormal(d) { return d.stale || d.dns_migrated || isExpired(d.expiry_at) }
const staleCount = computed(() => domains.value.filter(domStale).length)

async function ignoreDoms(rows) {
  let reason = ''
  try {
    const r = await ElMessageBox.prompt('忽略原因（可留空）', `忽略 ${rows.length} 个域名`, {
      inputPlaceholder: '如：已下线 / 已迁走 / 不再使用', confirmButtonText: '忽略', cancelButtonText: '取消', closeOnClickModal: false,
    })
    reason = r.value || ''
  } catch (e) { return } // 取消
  try {
    await bulkIgnoreDomains(rows.map((r) => r.ci_id), 1, reason)
    ElMessage.success(`已忽略 ${rows.length} 个（同步将跳过）`); selectedDoms.value = []; await loadDomains()
  } catch (e) { ElMessage.error(e.response?.data?.error || '失败') }
}
async function unignoreDoms(rows) {
  try {
    await bulkIgnoreDomains(rows.map((r) => r.ci_id), 0, '')
    ElMessage.success(`已取消忽略 ${rows.length} 个`); selectedDoms.value = []; await loadDomains()
  } catch (e) { ElMessage.error(e.response?.data?.error || '失败') }
}
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

// 主域名列表筛选/分页（状态/来源/到期/关键词 统一筛选）
const domPage = ref(1), domPageSize = ref(20)
const df = useDomainFilter(domains)
const domNameFilter = ref(null)
const domNameOptions = computed(() => [...new Set(domains.value.map((d) => d.name).filter(Boolean))].sort())
const domFiltered = computed(() => {
  const base = df.filtered.value
  return domNameFilter.value ? base.filter((d) => d.name === domNameFilter.value) : base
})
const domPaged = computed(() => { const s = (domPage.value - 1) * domPageSize.value; return domFiltered.value.slice(s, s + domPageSize.value) })
function doDomSearch() { df.doSearch(); domPage.value = 1 }
function resetDomFilter() { df.reset(); domNameFilter.value = null; domPage.value = 1 }

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

// ---- DNS 解析写回 GoDaddy（增/改/删）----
const writableTypes = ['A', 'AAAA', 'CNAME', 'TXT', 'MX']
const delBusy = ref({})
// 能否写回：绑了数据源(非手动录入、有注册商)且未忽略。后端还会再校验 provider 是否支持写回。
function canWrite(row) { return !!(row && row.registrar_name && !row.ignored) }
const dlg = reactive({ show: false, saving: false, editId: null, editOldData: '', ciid: null, domainName: '', env: 'GoDaddy',
  form: { type: 'A', name: '', data: '', ttl: 600, priority: 10 } })

function openCreate(row) {
  dlg.editId = null; dlg.editOldData = ''; dlg.ciid = row.ci_id; dlg.domainName = row.name; dlg.env = 'GoDaddy'
  dlg.form = { type: 'A', name: '', data: '', ttl: 600, priority: 10 }
  dlg.show = true
}
function openEdit(row, r) {
  dlg.editId = r.id; dlg.editOldData = r.data; dlg.ciid = row.ci_id; dlg.domainName = row.name; dlg.env = 'GoDaddy'
  dlg.form = { type: r.type, name: r.name, data: r.data, ttl: r.ttl || 600, priority: r.priority ?? 10 }
  dlg.show = true
}
async function saveRecord() {
  if (!dlg.form.data?.trim()) { ElMessage.warning('记录值不能为空'); return }
  const body = { type: dlg.form.type, name: dlg.form.name?.trim() || '@', data: dlg.form.data.trim(), ttl: dlg.form.ttl }
  if (dlg.form.type === 'MX') body.priority = dlg.form.priority
  // 编辑=改动线上解析，二次确认（显示 旧值→新值）；新增是增量，弹窗内已有警告，直接存。
  if (dlg.editId) {
    try {
      await app.showConfirm(
        `确认把 ${body.type} ${body.name}.${dlg.domainName} 的值改为「${body.data}」并写回 GoDaddy？\n原值：${dlg.editOldData}`,
        '确认修改解析（写回 GoDaddy）')
    } catch (e) { return }
  }
  dlg.saving = true
  try {
    const r = dlg.editId ? await updateDnsRecord(dlg.editId, body) : await createDnsRecord(dlg.ciid, body)
    ElMessage.success(r.msg || '已写回')
    dlg.show = false
    await loadRecords(dlg.ciid); await loadDomains()
  } catch (e) {
    ElMessage.error(e.response?.data?.error || '写回失败')
  } finally { dlg.saving = false }
}
// ---- 批量选择 / 批量删除 / 批量编辑 ----
const recSel = ref({})          // ciid → 选中的记录行
const batchDelBusy = ref({})
function recSelectable(r) { return !r.protected && writableTypes.includes(r.type) } // 受保护/不支持类型不可选
function onRecSel(ciid, v) { recSel.value = { ...recSel.value, [ciid]: v } }
async function doBatchDelete(row) {
  const sel = recSel.value[row.ci_id] || []
  if (!sel.length) return
  try {
    await app.showConfirm(`将删除 ${sel.length} 条解析（${row.name}）并从 GoDaddy 删除，确认？`, '确认批量删除')
  } catch (e) { return }
  batchDelBusy.value = { ...batchDelBusy.value, [row.ci_id]: true }
  try {
    const r = await batchDeleteDnsRecords(row.ci_id, sel.map((x) => x.id))
    let msg = r.msg || `已删除 ${r.deleted} 条`
    if (r.skipped > 0 && r.errors?.length) msg += '；跳过/失败：' + r.errors.map((e) => `${e.detail} ${e.msg}`).join('；')
    if (r.skipped > 0) ElMessage.warning(msg); else ElMessage.success(msg)
    recSel.value = { ...recSel.value, [row.ci_id]: [] }
    await loadRecords(row.ci_id); await loadDomains()
  } catch (e) {
    ElMessage.error(e.response?.data?.error || '批量删除失败')
  } finally { batchDelBusy.value = { ...batchDelBusy.value, [row.ci_id]: false } }
}

const bedit = reactive({ show: false, saving: false, ciid: null, domainName: '', bulkTtl: 600, rows: [] })
function openBatchEdit(row) {
  const sel = recSel.value[row.ci_id] || []
  if (!sel.length) return
  bedit.ciid = row.ci_id; bedit.domainName = row.name; bedit.bulkTtl = 600
  bedit.rows = sel.map((r) => ({ id: r.id, type: r.type, name: r.name, data: r.data, ttl: r.ttl || 600, priority: r.priority ?? 10 }))
  bedit.show = true
}
function applyBulkTtl() { bedit.rows.forEach((r) => { r.ttl = bedit.bulkTtl }) }
async function saveBatchEdit() {
  const records = bedit.rows.map((r) => { const b = { id: r.id, data: (r.data || '').trim(), ttl: r.ttl || 600 }; if (r.type === 'MX') b.priority = r.priority; return b })
  if (records.some((r) => !r.data)) { ElMessage.warning('记录值不能为空'); return }
  try {
    await app.showConfirm(`将编辑 ${records.length} 条解析（${bedit.domainName}）并写回 GoDaddy？`, '确认批量编辑')
  } catch (e) { return }
  bedit.saving = true
  try {
    const r = await batchUpdateDnsRecords(bedit.ciid, records)
    let msg = r.msg || `已编辑 ${r.updated} 条`
    if (r.failed > 0 && r.errors?.length) msg += '；失败：' + r.errors.map((e) => `${e.detail} ${e.msg}`).join('；')
    if (r.failed > 0) ElMessage.warning(msg); else ElMessage.success(msg)
    bedit.show = false
    recSel.value = { ...recSel.value, [bedit.ciid]: [] }
    await loadRecords(bedit.ciid); await loadDomains()
  } catch (e) {
    ElMessage.error(e.response?.data?.error || '批量编辑失败')
  } finally { bedit.saving = false }
}

// ---- 批量新增 ----
const batch = reactive({ show: false, saving: false, showPaste: false, pasteText: '', ciid: null, domainName: '', rows: [] })
function openBatch(row) {
  batch.ciid = row.ci_id; batch.domainName = row.name
  batch.rows = [{ type: 'A', name: '', data: '', ttl: 600, priority: 10 }]
  batch.showPaste = false; batch.pasteText = ''; batch.show = true
}
// 粘贴解析：每行 "类型 主机名 值 [TTL]"，空格或逗号分隔
function doPaste() {
  const added = []
  for (const line of (batch.pasteText || '').split('\n')) {
    const t = line.trim()
    if (!t) continue
    const parts = t.split(/[\s,]+/).filter(Boolean)
    if (parts.length < 3) continue // 至少 类型/主机名/值
    const type = parts[0].toUpperCase()
    const name = parts[1]
    const data = parts[2]
    const ttl = parts[3] && !isNaN(+parts[3]) ? +parts[3] : 600
    added.push({ type: writableTypes.includes(type) ? type : 'A', name, data, ttl, priority: 10 })
  }
  if (!added.length) { ElMessage.warning('没解析到有效行（每行至少：类型 主机名 值）'); return }
  // 若当前只有一空行，替换；否则追加
  const cur = batch.rows.filter((r) => r.name || r.data)
  batch.rows = [...cur, ...added]
  ElMessage.success(`解析到 ${added.length} 行，已填入`)
}
async function saveBatch() {
  const records = batch.rows.filter((r) => (r.name || '').trim() !== '' || (r.data || '').trim() !== '')
    .map((r) => { const b = { type: r.type, name: (r.name || '@').trim(), data: (r.data || '').trim(), ttl: r.ttl || 600 }; if (r.type === 'MX') b.priority = r.priority; return b })
  if (!records.length) { ElMessage.warning('没有要新增的记录'); return }
  try {
    await app.showConfirm(`将新增 ${records.length} 条解析到 ${batch.domainName} 并写回 GoDaddy？`, '确认批量新增')
  } catch (e) { return }
  batch.saving = true
  try {
    const r = await batchCreateDnsRecords(batch.ciid, records)
    let msg = r.msg || `已新增 ${r.added} 条`
    if (r.skipped > 0 && r.errors?.length) msg += '；跳过：' + r.errors.map((e) => `第${e.row}行(${e.detail}) ${e.msg}`).join('；')
    if (r.skipped > 0) ElMessage.warning(msg); else ElMessage.success(msg)
    batch.show = false
    await loadRecords(batch.ciid); await loadDomains()
  } catch (e) {
    ElMessage.error(e.response?.data?.error || '批量写回失败')
  } finally { batch.saving = false }
}
async function delRecord(row, r) {
  try {
    await app.showConfirm(`删除解析 ${r.type} ${r.name}（${r.data}）？会直接从 GoDaddy 删除`, '删除解析')
  } catch (e) { return } // 取消
  delBusy.value = { ...delBusy.value, [r.id]: true }
  try {
    const res = await deleteDnsRecord(r.id)
    ElMessage.success(res.msg || '已删除')
    await loadRecords(row.ci_id); await loadDomains()
  } catch (e) {
    ElMessage.error(e.response?.data?.error || '删除失败')
  } finally { delBusy.value = { ...delBusy.value, [r.id]: false } }
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
.batch-bar { display: flex; gap: 10px; align-items: center; margin-top: 8px; padding: 6px 10px; background: #eef2f8; border-radius: 4px; }
.reso { padding: 8px 16px 12px; background: #fafbfc; }
.mono { font-family: ui-monospace, Menlo, Consolas, monospace; font-size: 12px; }
.stale { text-decoration: line-through; color: #b0b3bb; }
.muted { color: #909399; }
.exp-orange { color: #e6a23c; font-weight: 600; }
.exp-red { color: #f56c6c; font-weight: 600; }
</style>
