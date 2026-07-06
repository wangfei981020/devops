<template>
  <div class="page">
    <div class="page-head">
      <span class="page-title">域名（主机头台账）</span>
      <div>
        <el-button v-if="selected.length" type="warning" :icon="EditPen" @click="openBulk">批量设置（{{ selected.length }}）</el-button>
        <el-button :icon="Download" @click="exportCsv">导出Excel</el-button>
        <el-button type="warning" :icon="Operation" @click="openAssign">批量分配</el-button>
        <el-button :icon="Refresh" :loading="syncingAll" @click="syncAll">从 GoDaddy 同步</el-button>
        <el-button type="primary" :icon="Plus" @click="openAdd">录入解析</el-button>
      </div>
    </div>

    <el-card shadow="never" style="margin-bottom:12px">
      <div class="filter">
        <el-input v-model="f.keyword" placeholder="搜索 主机头/域名" clearable :prefix-icon="Search" style="width:200px" @keyup.enter="doSearch" />
        <el-select v-model="f.project" clearable placeholder="项目" style="width:150px">
          <el-option v-for="p in projectOptions" :key="p" :label="p" :value="p" />
        </el-select>
        <el-select v-model="f.env" clearable placeholder="环境" style="width:120px">
          <el-option v-for="e in envOptions" :key="e" :label="e" :value="e" />
        </el-select>
        <el-select v-model="f.module" clearable placeholder="模块" style="width:130px">
          <el-option v-for="m in moduleOptions" :key="m" :label="m" :value="m" />
        </el-select>
        <el-select v-model="f.source" clearable placeholder="数据源" style="width:150px">
          <el-option v-for="r in registrars" :key="r.id" :label="r.name" :value="r.name" />
        </el-select>
        <el-button type="primary" :icon="Search" @click="doSearch">搜索</el-button>
        <el-button @click="resetFilter">重置</el-button>
        <span class="muted" style="margin-left:auto">共 {{ filteredRows.length }} / {{ rows.length }} 条主机头</span>
      </div>
    </el-card>

    <el-card shadow="never">
      <el-table :data="pagedRows" size="small" row-key="id" v-loading="loading" :default-sort="{ prop: 'project' }"
        @selection-change="(v) => selected = v">
        <el-table-column type="selection" width="42" reserve-selection />
        <el-table-column prop="project" label="项目" width="130" sortable><template #default="{ row }">
          <el-tag v-if="row.project" size="small" effect="plain" :style="tagStyle(projectColor(row.project))">{{ row.project }}</el-tag>
          <span v-else class="muted">—</span>
        </template></el-table-column>
        <el-table-column prop="env" label="环境" width="100" sortable><template #default="{ row }">
          <el-tag v-if="row.env" size="small" effect="plain" :style="tagStyle(envColor(row.env))">{{ row.env }}</el-tag>
          <span v-else class="muted">—</span>
        </template></el-table-column>
        <el-table-column prop="module" label="模块" width="120"><template #default="{ row }">{{ row.module || '—' }}</template></el-table-column>
        <el-table-column prop="fqdn" label="域名" min-width="230" sortable show-overflow-tooltip><template #default="{ row }">
          <span class="mono" :class="{ stale: row.stale }">{{ row.fqdn }}</span>
          <el-tag v-if="row.stale" type="warning" size="small" style="margin-left:6px">厂商已删</el-tag>
        </template></el-table-column>
        <el-table-column prop="cdn_name" label="CDN厂商" width="120" show-overflow-tooltip><template #default="{ row }">
          <span v-if="row.cdn_name">{{ row.cdn_name }}</span><span v-else class="muted">—</span>
        </template></el-table-column>
        <el-table-column label="回源CNAME" min-width="180" show-overflow-tooltip><template #default="{ row }">
          <span class="mono">{{ row.cname || '—' }}</span>
        </template></el-table-column>
        <el-table-column label="源站IP" min-width="150" show-overflow-tooltip><template #default="{ row }">
          <span class="mono">{{ row.origin_ip || '—' }}</span>
        </template></el-table-column>
        <el-table-column label="证书到期" width="140" sortable :sort-method="sortByCertExpiry"><template #default="{ row }">
          <el-tooltip :disabled="!row.cert_check_msg" :content="row.cert_check_msg">
            <el-tag :type="certState(row).type" size="small" effect="light">{{ certState(row).text }}</el-tag>
          </el-tooltip>
        </template></el-table-column>
        <el-table-column label="操作" width="146" fixed="right"><template #default="{ row }">
          <div style="display:flex;gap:8px;align-items:center">
            <el-tooltip content="查看详情"><el-button link type="primary" :icon="View" @click="openDetail(row)" /></el-tooltip>
            <el-tooltip content="编辑"><el-button link type="primary" :icon="Edit" @click="openEdit(row)" /></el-tooltip>
            <el-tooltip content="检测证书"><el-button link type="primary" :loading="checking[row.id]" :icon="CircleCheck" @click="checkCert(row)" /></el-tooltip>
            <el-tooltip v-if="row.origin === 'manual' || row.stale" :content="row.stale ? '移除（厂商已删）' : '删除'">
              <el-button link type="danger" :icon="Delete" @click="del(row)" />
            </el-tooltip>
          </div>
        </template></el-table-column>
      </el-table>
      <el-pagination v-model:current-page="currentPage" v-model:page-size="pageSize" :page-sizes="[10,20,50,100]"
        :total="filteredRows.length" layout="total, sizes, prev, pager, next" style="margin-top:12px; justify-content:flex-end" />
    </el-card>

    <!-- 编辑解析（业务字段 + 回源/源站；域名只读） -->
    <el-dialog :close-on-click-modal="false" :close-on-press-escape="false" v-model="dlg" :title="editing ? '编辑解析' : '录入解析'" width="520px">
      <el-form :model="form" label-width="96px">
        <el-form-item v-if="!editing" label="主域名">
          <el-select v-model="form.domain_ci_id" filterable placeholder="选择主域名" style="width:260px">
            <el-option v-for="d in allDomains" :key="d.ci_id" :label="d.name" :value="d.ci_id" />
          </el-select>
        </el-form-item>
        <el-form-item label="主机头" v-if="!editing">
          <el-input v-model="form.host" placeholder="www / @ / api" style="width:180px" />
          <el-select v-model="form.record_type" style="width:100px; margin-left:8px">
            <el-option label="A" value="A" />
            <el-option label="CNAME" value="CNAME" />
          </el-select>
        </el-form-item>
        <el-form-item v-else label="域名">
          <span class="mono">{{ form.fqdn }}</span>
          <span v-if="synced" class="muted" style="margin-left:8px">同步来的，域名/回源CNAME 只读，可改业务字段</span>
        </el-form-item>
        <el-form-item label="项目">
          <el-select v-model="form.project" filterable clearable placeholder="选择项目" style="width:260px">
            <el-option v-for="p in app.projects" :key="p.id" :label="p.name" :value="p.name" />
          </el-select>
        </el-form-item>
        <el-form-item label="环境">
          <el-select v-model="form.env" clearable placeholder="选择环境" style="width:260px">
            <el-option v-for="e in app.environments" :key="e.id" :label="e.code" :value="e.code" />
          </el-select>
        </el-form-item>
        <el-form-item label="模块"><el-input v-model="form.module" style="width:260px" /></el-form-item>
        <el-form-item label="CDN">
          <el-select v-model="form.cdn_id" clearable placeholder="（可选）内网/直连可留空" style="width:260px">
            <el-option v-for="c in app.cdns" :key="c.id" :label="c.name" :value="c.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="回源CNAME"><el-input v-model="form.cname" :disabled="synced" placeholder="走 CDN 时的回源地址 / CNAME" /></el-form-item>
        <el-form-item label="源站IP"><el-input v-model="form.origin_ip" placeholder="源站 IP（多个逗号分隔）" /></el-form-item>
        <el-form-item label="证书到期">
          <el-date-picker v-model="form.cert_expiry_at" type="date" value-format="YYYY-MM-DD" placeholder="可手填，或保存后点「检测证书」自动读" style="width:260px" />
        </el-form-item>
      </el-form>
      <template #footer><el-button @click="dlg=false">取消</el-button><el-button type="primary" @click="save">保存</el-button></template>
    </el-dialog>

    <!-- 批量设置项目/环境/模块（只改勾选的字段，留空的不动） -->
    <el-dialog :close-on-click-modal="false" :close-on-press-escape="false" v-model="bDlg" title="批量设置" width="480px">
      <div class="muted" style="margin-bottom:12px">对选中的 {{ selected.length }} 条解析统一设置。只改打开开关的字段，关闭的保持原值不动。</div>
      <el-form :model="bForm" label-width="72px">
        <el-form-item label="项目">
          <el-switch v-model="bForm.setProject" style="margin-right:10px" />
          <el-select v-model="bForm.project" :disabled="!bForm.setProject" filterable clearable placeholder="选择项目" style="width:240px">
            <el-option v-for="p in app.projects" :key="p.id" :label="p.name" :value="p.name" />
          </el-select>
        </el-form-item>
        <el-form-item label="环境">
          <el-switch v-model="bForm.setEnv" style="margin-right:10px" />
          <el-select v-model="bForm.env" :disabled="!bForm.setEnv" clearable placeholder="选择环境" style="width:240px">
            <el-option v-for="e in app.environments" :key="e.id" :label="e.code" :value="e.code" />
          </el-select>
        </el-form-item>
        <el-form-item label="模块">
          <el-switch v-model="bForm.setModule" style="margin-right:10px" />
          <el-input v-model="bForm.module" :disabled="!bForm.setModule" placeholder="模块" style="width:240px" />
        </el-form-item>
      </el-form>
      <template #footer><el-button @click="bDlg=false">取消</el-button><el-button type="primary" @click="saveBulk">应用到 {{ selected.length }} 条</el-button></template>
    </el-dialog>

    <!-- 批量分配：项目/环境选一次，多个模块各带一批域名（完整 FQDN 精确匹配，找不到跳过） -->
    <el-dialog :close-on-click-modal="false" :close-on-press-escape="false" v-model="aDlg" title="批量分配" width="660px">
      <template v-if="!aResult">
        <div style="display:flex;gap:20px;align-items:center;margin-bottom:10px">
          <div>项目
            <el-select v-model="aForm.project" filterable clearable placeholder="选择项目" style="width:190px">
              <el-option v-for="p in app.projects" :key="p.id" :label="p.name" :value="p.name" />
            </el-select>
          </div>
          <div>环境
            <el-select v-model="aForm.env" clearable placeholder="选择环境" style="width:150px">
              <el-option v-for="e in app.environments" :key="e.id" :label="e.code" :value="e.code" />
            </el-select>
          </div>
          <span class="muted">整批统一</span>
        </div>
        <div class="muted" style="margin-bottom:8px">每个模块各配一批域名（完整 FQDN，一行一个 或 逗号分隔）；项目/环境留空则不改</div>
        <div v-for="(b, i) in aForm.blocks" :key="i" class="assign-block">
          <div style="display:flex;align-items:center;gap:8px;margin-bottom:6px">
            <span>模块</span>
            <el-input v-model="b.module" placeholder="如 门户 / 交易（留空则不改模块）" style="width:220px" />
            <span class="muted">已识别 {{ countDomains(b.domains) }} 个</span>
            <el-button v-if="aForm.blocks.length > 1" link type="danger" :icon="Delete" style="margin-left:auto" @click="aForm.blocks.splice(i, 1)">删除</el-button>
          </div>
          <el-input v-model="b.domains" type="textarea" :rows="3" placeholder="www.sync-shop.com&#10;m.sync-shop.com" />
        </div>
        <div style="margin-top:10px">
          <el-button :icon="Plus" @click="aForm.blocks.push({ module: '', domains: '' })">添加模块</el-button>
          <span class="muted" style="margin-left:12px">合计 {{ aForm.blocks.length }} 模块 / {{ totalAssignDomains }} 域名</span>
        </div>
      </template>
      <template v-else>
        <el-result icon="success" :title="`成功更新 ${aResult.updated} 条`"
          :sub-title="`项目=${aForm.project || '（不改）'}　环境=${aForm.env || '（不改）'}`">
          <template #extra>
            <div v-if="aResult.groups.length" class="muted">{{ aResult.groups.map(g => `${g.module} ${g.count} 条`).join('　·　') }}</div>
          </template>
        </el-result>
        <el-alert v-if="aResult.notFound.length" type="warning" :closable="false"
          :title="`${aResult.notFound.length} 个未找到，已跳过（台账里没有这些 FQDN）`" style="margin-top:8px">
          <div class="mono" style="white-space:pre-line;max-height:160px;overflow:auto">{{ aResult.notFound.join('\n') }}</div>
        </el-alert>
      </template>
      <template #footer>
        <template v-if="!aResult">
          <el-button @click="aDlg = false">取消</el-button>
          <el-button type="primary" :loading="assigning" :disabled="!totalAssignDomains" @click="applyAssign">应用（{{ totalAssignDomains }} 条）</el-button>
        </template>
        <template v-else>
          <el-button @click="resetAssign">继续分配</el-button>
          <el-button type="primary" @click="aDlg = false; load()">完成关闭</el-button>
        </template>
      </template>
    </el-dialog>

    <!-- 查看详情（只读，长字段全展开） -->
    <el-dialog :close-on-click-modal="false" :close-on-press-escape="false" v-model="dDlg" title="解析详情" width="560px">
      <el-descriptions v-if="detail" :column="1" border size="small">
        <el-descriptions-item label="域名"><span class="mono detail-val">{{ detail.fqdn }}</span></el-descriptions-item>
        <el-descriptions-item label="主机头 / 类型"><span class="mono">{{ detail.host || '@' }}</span> / {{ detail.record_type }}</el-descriptions-item>
        <el-descriptions-item label="项目">
          <el-tag v-if="detail.project" size="small" effect="plain" :style="tagStyle(projectColor(detail.project))">{{ detail.project }}</el-tag>
          <span v-else class="muted">—</span>
        </el-descriptions-item>
        <el-descriptions-item label="环境">
          <el-tag v-if="detail.env" size="small" effect="plain" :style="tagStyle(envColor(detail.env))">{{ detail.env }}</el-tag>
          <span v-else class="muted">—</span>
        </el-descriptions-item>
        <el-descriptions-item label="模块">{{ detail.module || '—' }}</el-descriptions-item>
        <el-descriptions-item label="CDN厂商">{{ detail.cdn_name || '—' }}</el-descriptions-item>
        <el-descriptions-item label="回源CNAME"><span class="mono detail-val">{{ detail.cname || '—' }}</span></el-descriptions-item>
        <el-descriptions-item label="源站IP"><span class="mono detail-val">{{ detail.origin_ip || '—' }}</span></el-descriptions-item>
        <el-descriptions-item label="证书到期">
          <el-tag :type="certState(detail).type" size="small" effect="light">{{ certState(detail).text }}</el-tag>
          <div v-if="detail.cert_check_msg" class="muted" style="margin-top:4px">{{ detail.cert_check_msg }}</div>
        </el-descriptions-item>
        <el-descriptions-item label="来源">
          <el-tag v-if="detail.origin === 'manual'" type="info" size="small">手动录入</el-tag>
          <span v-else>{{ detail.source_name || '同步' }}</span>
          <el-tag v-if="detail.stale" type="warning" size="small" style="margin-left:6px">厂商已删</el-tag>
        </el-descriptions-item>
        <el-descriptions-item label="操作人">{{ detail.operator || '—' }}</el-descriptions-item>
        <el-descriptions-item label="更新时间">{{ detail.updated_at || '—' }}</el-descriptions-item>
      </el-descriptions>
      <template #footer><el-button @click="dDlg=false">关闭</el-button><el-button type="primary" :icon="Edit" @click="editFromDetail">编辑</el-button></template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Plus, Refresh, Search, CircleCheck, Edit, Delete, EditPen, View, Download, Operation } from '@element-plus/icons-vue'
import { listAllRecords, createRecord, updateRecord, bulkUpdateRecords, deleteRecord, checkRecordCert,
  syncDomainRecords, listDomains, listRegistrars } from '../api/cmdb'
import { useAppStore } from '../stores/app'

const app = useAppStore()
const rows = ref([]), allDomains = ref([]), registrars = ref([]), loading = ref(false)
const checking = ref({}), syncingAll = ref(false)
const dlg = ref(false), editing = ref(false), form = ref({})
const selected = ref([])
const bDlg = ref(false), bForm = ref({})
const dDlg = ref(false), detail = ref(null)
const aDlg = ref(false), aForm = ref({ project: '', env: '', blocks: [{ module: '', domains: '' }] }), aResult = ref(null), assigning = ref(false)
const synced = computed(() => editing.value && form.value.origin && form.value.origin !== 'manual')
const f = ref({ keyword: '', project: null, env: null, module: null, source: null })
const query = ref({ ...f.value })
const currentPage = ref(1), pageSize = ref(20)

// —— 配色：项目/环境优先用「基础配置」里配的颜色，没配则按名字 hash 出冷色（红橙只留给证书告警）——
const COOL = ['#3b7dd8', '#5b8ff9', '#269a99', '#5ad8a6', '#6dc8ec', '#9270ca', '#5d7092', '#0e7a6e', '#7d5fd6', '#2f9e8f', '#3d76c9', '#417ec0']
function hashColor(s) { if (!s) return '#909399'; let h = 0; for (let i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) >>> 0; return COOL[h % COOL.length] }
const ENV_MAP = { PROD: '#3b7dd8', PRD: '#3b7dd8', 生产: '#3b7dd8', UAT: '#269a99', SIT: '#5d7092', PRE: '#9270ca', DEV: '#5ad8a6', 开发: '#5ad8a6', TEST: '#6dc8ec', 测试: '#6dc8ec' }
function projectColor(name) { const p = app.projects.find((x) => x.name === name); return (p && p.color) || hashColor(name) }
function envColor(e) { if (!e) return '#909399'; const env = app.environments.find((x) => x.code === e); if (env && env.color) return env.color; return ENV_MAP[e.toUpperCase()] || ENV_MAP[e] || hashColor(e) }
function tagStyle(color) { return { color, borderColor: color + '66', background: color + '14' } }
// 证书到期：语义告警色（过期红 / 快到期橙 / 正常绿 / 未检测·失败灰）
function certState(r) {
  if (r.cert_expiry_at) {
    const days = (new Date(r.cert_expiry_at) - Date.now()) / 86400000
    if (days < 0) return { type: 'danger', text: '已过期 ' + r.cert_expiry_at }
    if (days < 30) return { type: 'warning', text: r.cert_expiry_at }
    return { type: 'success', text: r.cert_expiry_at }
  }
  if (r.cert_check_msg) return { type: 'info', text: '检测失败' }
  return { type: 'info', text: '未检测' }
}

const distinct = (key) => [...new Set(rows.value.map((r) => r[key]).filter(Boolean))].sort()
const projectOptions = computed(() => distinct('project'))
const envOptions = computed(() => distinct('env'))
const moduleOptions = computed(() => distinct('module'))

const filteredRows = computed(() => rows.value.filter((r) => {
  const q = query.value
  const kw = q.keyword?.toLowerCase()
  return (!kw || r.fqdn.toLowerCase().includes(kw) || (r.project || '').toLowerCase().includes(kw)) &&
    (!q.project || r.project === q.project) &&
    (!q.env || r.env === q.env) &&
    (!q.module || r.module === q.module) &&
    (!q.source || r.source_name === q.source)
}))
const pagedRows = computed(() => {
  const s = (currentPage.value - 1) * pageSize.value
  return filteredRows.value.slice(s, s + pageSize.value)
})
function doSearch() { query.value = { ...f.value }; currentPage.value = 1 }
function resetFilter() { f.value = { keyword: '', project: null, env: null, module: null, source: null }; query.value = { ...f.value }; currentPage.value = 1 }

async function load() {
  loading.value = true
  try {
    rows.value = await listAllRecords()
    allDomains.value = await listDomains()
    registrars.value = await listRegistrars()
    app.loadBasics()
  } catch (e) {} finally { loading.value = false }
}

function openAdd() {
  editing.value = false
  form.value = { domain_ci_id: null, host: '', record_type: 'A', project: '', env: '', module: '', cdn_id: null, cname: '', origin_ip: '', cert_expiry_at: '' }
  dlg.value = true
}
function openEdit(row) {
  editing.value = true
  form.value = { ...row, cert_expiry_at: row.cert_expiry_at || '' }
  dlg.value = true
}
async function save() {
  try {
    if (editing.value) {
      await updateRecord(form.value.id, form.value)
    } else {
      if (!form.value.domain_ci_id) { ElMessage.warning('请选择主域名'); return }
      if (!form.value.host) { ElMessage.warning('主机头必填'); return }
      await createRecord(form.value.domain_ci_id, form.value)
    }
    ElMessage.success('已保存'); dlg.value = false; load()
  } catch (e) { ElMessage.error(e.response?.data?.error || '保存失败') }
}
async function del(row) {
  try {
    await app.showConfirm(row.stale ? `该解析 GoDaddy 已删除，确认从台账移除 ${row.fqdn}？` : `删除解析 ${row.fqdn}？`)
    await deleteRecord(row.id); ElMessage.success('已删除'); load()
  } catch (e) { if (e !== 'cancel') ElMessage.error('删除失败') }
}
function openDetail(row) { detail.value = row; dDlg.value = true }
function editFromDetail() { dDlg.value = false; openEdit(detail.value) }
function openBulk() {
  bForm.value = { setProject: false, project: '', setEnv: false, env: '', setModule: false, module: '' }
  bDlg.value = true
}
async function saveBulk() {
  const b = bForm.value
  const payload = { ids: selected.value.map((r) => r.id) }
  if (b.setProject) payload.project = b.project || ''
  if (b.setEnv) payload.env = b.env || ''
  if (b.setModule) payload.module = b.module || ''
  if (payload.project === undefined && payload.env === undefined && payload.module === undefined) {
    ElMessage.warning('请至少打开一个要设置的字段'); return
  }
  try {
    const r = await bulkUpdateRecords(payload)
    ElMessage.success(`已批量更新 ${r.updated} 条`); bDlg.value = false; load()
  } catch (e) { ElMessage.error(e.response?.data?.error || '批量设置失败') }
}
// —— 批量分配：项目/环境统一，多个模块块各带一批 FQDN，完整精确匹配 ——
function parseDomains(s) { return (s || '').split(/[\n,;，；]+/).map((x) => x.trim().toLowerCase()).filter(Boolean) }
function countDomains(s) { return parseDomains(s).length }
const totalAssignDomains = computed(() => aForm.value.blocks.reduce((n, b) => n + countDomains(b.domains), 0))
function openAssign() { aForm.value = { project: '', env: '', blocks: [{ module: '', domains: '' }] }; aResult.value = null; aDlg.value = true }
function resetAssign() { aForm.value = { project: '', env: '', blocks: [{ module: '', domains: '' }] }; aResult.value = null }
async function applyAssign() {
  const fqdnMap = new Map(rows.value.map((r) => [r.fqdn.toLowerCase(), r.id]))
  const groups = [], notFound = []
  let updated = 0
  assigning.value = true
  try {
    for (const b of aForm.value.blocks) {
      const doms = parseDomains(b.domains)
      if (!doms.length) continue
      const ids = []
      for (const d of doms) { const id = fqdnMap.get(d); if (id) ids.push(id); else notFound.push(d) }
      const payload = { ids }
      if (aForm.value.project) payload.project = aForm.value.project
      if (aForm.value.env) payload.env = aForm.value.env
      if (b.module) payload.module = b.module
      if (!ids.length || (payload.project === undefined && payload.env === undefined && payload.module === undefined)) continue
      const r = await bulkUpdateRecords(payload)
      updated += r.updated || 0
      groups.push({ module: b.module || '(未设模块)', count: r.updated || 0 })
    }
    aResult.value = { updated, groups, notFound: [...new Set(notFound)] }
  } catch (e) { ElMessage.error(e.response?.data?.error || '批量分配失败') }
  finally { assigning.value = false }
}
// 导出当前筛选结果为 CSV（Excel 可直接打开，UTF-8 BOM 防中文乱码）
function exportCsv() {
  const headers = ['项目', '环境', '模块', '域名', 'CDN厂商', '回源CNAME', '源站IP', '证书到期', '来源', '操作人', '更新时间']
  const esc = (v) => { v = (v == null ? '' : String(v)); return /[",\n]/.test(v) ? '"' + v.replace(/"/g, '""') + '"' : v }
  const lines = [headers.join(',')]
  for (const r of filteredRows.value) {
    lines.push([r.project, r.env, r.module, r.fqdn, r.cdn_name, r.cname, r.origin_ip, r.cert_expiry_at,
      r.origin === 'manual' ? '手动录入' : (r.source_name || '同步'), r.operator, r.updated_at].map(esc).join(','))
  }
  const blob = new Blob(['\ufeff' + lines.join('\n')], { type: 'text/csv;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url; a.download = 'cmdb-主机头台账.csv'; a.click()
  URL.revokeObjectURL(url)
}
async function checkCert(row) {
  checking.value = { ...checking.value, [row.id]: true }
  try {
    const res = await checkRecordCert(row.id)
    if (res.ok) ElMessage.success(`${res.fqdn} 证书到期 ${res.cert_expiry_at}`)
    else ElMessage.warning(`${res.fqdn} 检测失败：${res.msg}`)
    load()
  } catch (e) { ElMessage.error('检测失败') } finally { checking.value = { ...checking.value, [row.id]: false } }
}
async function syncAll() {
  const targets = allDomains.value.filter((d) => d.origin !== 'manual' && !d.stale)
  if (!targets.length) { ElMessage.info('没有可从数据源同步的域名'); return }
  syncingAll.value = true
  let ok = 0, imported = 0, failed = 0
  for (const d of targets) {
    try { const r = await syncDomainRecords(d.ci_id); ok++; imported += r.imported_records || 0 }
    catch (e) { failed++ }
  }
  ElMessage.success(`同步完成：${ok} 个域名，新导入 ${imported} 条解析${failed ? `，${failed} 个失败` : ''}`)
  syncingAll.value = false; load()
}
// 证书到期排序：无到期日（未检测/失败）排最后
function sortByCertExpiry(a, b) {
  if (!a.cert_expiry_at && !b.cert_expiry_at) return 0
  if (!a.cert_expiry_at) return 1
  if (!b.cert_expiry_at) return -1
  return new Date(a.cert_expiry_at) - new Date(b.cert_expiry_at)
}
onMounted(load)
</script>

<style scoped>
.filter { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; }
.mono { font-family: ui-monospace, Menlo, Consolas, monospace; font-size: 12px; }
.stale { text-decoration: line-through; color: #b0b3bb; }
.detail-val { word-break: break-all; }
</style>
