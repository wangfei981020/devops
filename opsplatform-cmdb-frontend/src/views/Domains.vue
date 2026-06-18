<template>
  <div class="page">
    <div class="page-head">
      <span class="page-title">域名</span>
      <div>
        <el-button :icon="Refresh" :loading="refreshingAll" @click="refreshAll">一键刷新到期</el-button>
        <el-button type="primary" :icon="Plus" @click="openAdd">录入域名</el-button>
      </div>
    </div>

    <el-card shadow="never" style="margin-bottom:12px">
      <div class="filter">
        <el-input v-model="f.keyword" placeholder="搜索主域名" clearable :prefix-icon="Search" style="width:220px" @keyup.enter="doSearch" />
        <el-select v-model="f.registrar" clearable placeholder="数据源/注册商" style="width:180px">
          <el-option v-for="r in registrars" :key="r.id" :label="r.name" :value="r.id" />
        </el-select>
        <el-button type="primary" :icon="Search" @click="doSearch">搜索</el-button>
        <el-button @click="resetFilter">重置</el-button>
        <span class="muted" style="margin-left:auto">共 {{ filteredRows.length }} / {{ rows.length }} 个主域名</span>
      </div>
    </el-card>

    <el-card shadow="never">
      <el-table :data="pagedRows" size="small" row-key="ci_id" v-loading="loading" @expand-change="onExpand">
        <el-table-column type="expand">
          <template #default="{ row }">
            <div class="reso">
              <div class="reso-head">
                <b>解析记录</b>
                <span class="muted" style="margin-left:8px" v-if="row.origin === 'manual'">手动维护（手动录入的域名不从厂商同步，可增删改）</span>
                <span class="muted" style="margin-left:8px" v-else>从 GoDaddy 同步进来，只读；证书到期可检测；全量原始记录看「DNS 记录」页</span>
                <el-button v-if="row.origin === 'manual'" type="primary" size="small" :icon="Plus" style="float:right" @click="openAddReso(row)">添加解析</el-button>
                <el-button v-else type="primary" size="small" :icon="Refresh" :loading="syncingReso[row.ci_id]" style="float:right" @click="syncReso(row)">从 GoDaddy 同步解析</el-button>
              </div>
              <el-table :data="records[row.ci_id] || []" size="small" max-height="340" v-loading="recordLoading[row.ci_id]">
                <el-table-column label="主机头" min-width="150"><template #default="{ row: r }">
                  <span class="mono" :class="{stale: r.stale}">{{ r.host }}</span>
                  <el-tag v-if="r.stale" type="warning" size="small" style="margin-left:6px">厂商已删</el-tag>
                </template></el-table-column>
                <el-table-column prop="project" label="项目" width="110"><template #default="{ row: r }">{{ r.project || '—' }}</template></el-table-column>
                <el-table-column label="环境" width="80"><template #default="{ row: r }"><el-tag v-if="r.env" size="small">{{ r.env }}</el-tag><span v-else class="muted">—</span></template></el-table-column>
                <el-table-column prop="module" label="模块" width="100"><template #default="{ row: r }">{{ r.module || '—' }}</template></el-table-column>
                <el-table-column label="CDN" width="110"><template #default="{ row: r }">{{ r.cdn_name || '—' }}</template></el-table-column>
                <el-table-column label="回源" min-width="160" show-overflow-tooltip><template #default="{ row: r }"><span class="mono">{{ r.cname || '—' }}</span></template></el-table-column>
                <el-table-column label="源站 IP" width="130"><template #default="{ row: r }"><span class="mono">{{ r.origin_ip || '—' }}</span></template></el-table-column>
                <el-table-column label="证书到期" width="110"><template #default="{ row: r }">
                  <span v-if="r.cert_expiry_at" :class="{warn: isNear(r.cert_expiry_at)}">{{ r.cert_expiry_at }}</span>
                  <el-tooltip v-else-if="r.cert_check_msg" :content="r.cert_check_msg"><span class="muted">检查失败</span></el-tooltip>
                  <span v-else class="muted">—</span>
                </template></el-table-column>
                <el-table-column prop="operator" label="操作人" width="90"><template #default="{ row: r }">{{ r.operator || '—' }}</template></el-table-column>
                <el-table-column prop="updated_at" label="更新时间" width="125" />
                <el-table-column label="操作" width="170" fixed="right"><template #default="{ row: r }">
                  <el-button link type="primary" :loading="checking[r.id]" @click="checkCert(row, r)">检测证书</el-button>
                  <template v-if="row.origin === 'manual'">
                    <el-button link type="primary" @click="openEditReso(row, r)">编辑</el-button>
                    <el-button link type="danger" @click="delReso(row, r)">删除</el-button>
                  </template>
                  <el-button v-else-if="r.stale" link type="danger" @click="delReso(row, r)">移除</el-button>
                </template></el-table-column>
              </el-table>
              <el-empty v-if="!(records[row.ci_id] && records[row.ci_id].length) && !recordLoading[row.ci_id]"
                :description="row.origin === 'manual' ? '还没有解析，点右上「添加解析」' : '还没有解析，点右上「从 GoDaddy 同步解析」'" :image-size="50" />
            </div>
          </template>
        </el-table-column>
        <el-table-column label="主域名" min-width="220"><template #default="{ row }">
          <span :class="{stale: row.stale}">{{ row.name }}</span>
          <el-tag v-if="row.stale" type="warning" size="small" style="margin-left:6px">厂商已删</el-tag>
        </template></el-table-column>
        <el-table-column label="来源" width="150"><template #default="{ row }">
          <el-tag v-if="row.origin === 'manual'" type="info" size="small">手动录入</el-tag>
          <el-tag v-else type="success" size="small">{{ row.registrar_name || '同步' }}</el-tag>
        </template></el-table-column>
        <el-table-column label="域名到期" width="120"><template #default="{ row }"><span :class="{warn: isNear(row.expiry_at)}">{{ row.expiry_at || '—' }}</span></template></el-table-column>
        <el-table-column label="解析数" width="80"><template #default="{ row }">{{ row.reso_count ?? '—' }}</template></el-table-column>
        <el-table-column label="操作" width="250" fixed="right">
          <template #default="{ row }">
            <el-tooltip content="刷新域名注册到期(WHOIS) + 主域名证书(连443)"><el-button link type="primary" :loading="refreshing[row.ci_id]" @click="refreshOne(row)">刷到期</el-button></el-tooltip>
            <el-tooltip content="一键检测该域名下所有解析的证书到期"><el-button link type="primary" :loading="checkingAll[row.ci_id]" @click="checkAllCerts(row)">检测全部证书</el-button></el-tooltip>
            <template v-if="row.origin === 'manual'">
              <el-button link type="primary" @click="openEdit(row)">编辑</el-button>
              <el-button link type="danger" @click="del(row)">删除</el-button>
            </template>
            <el-tooltip v-else-if="row.stale" content="GoDaddy 已无此域名，确认后从台账移除"><el-button link type="danger" @click="del(row)">移除</el-button></el-tooltip>
          </template>
        </el-table-column>
      </el-table>
      <el-pagination v-model:current-page="currentPage" v-model:page-size="pageSize" :page-sizes="[10,20,50,100]"
        :total="filteredRows.length" layout="total, sizes, prev, pager, next" style="margin-top:12px; justify-content:flex-end" />
    </el-card>

    <!-- 主域名表单（仅手动录入/编辑手动域名） -->
    <el-dialog v-model="dlg" :title="editing?'编辑主域名':'录入主域名'" width="480px">
      <el-form :model="form" label-width="100px">
        <el-form-item label="主域名">
          <el-input v-model="form.name" :disabled="editing" placeholder="example.com（自动归一到注册域名）" style="width:240px" />
          <span v-if="editing" class="muted" style="margin-left:8px">不可改</span>
        </el-form-item>
        <el-form-item label="数据源/注册商">
          <el-select v-model="form.registrar_id" clearable placeholder="（可选，签证书 / 同步解析用）" style="width:240px">
            <el-option v-for="r in registrars" :key="r.id" :label="r.name" :value="r.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="DNS provider"><el-input v-model="form.dns_provider" placeholder="godaddy/aliyun/cloudflare（可选）" /></el-form-item>
        <el-form-item label="域名到期"><el-date-picker v-model="form.expiry_at" type="date" value-format="YYYY-MM-DD" placeholder="域名注册到期日" /></el-form-item>
      </el-form>
      <template #footer><el-button @click="dlg=false">取消</el-button><el-button type="primary" @click="save">保存</el-button></template>
    </el-dialog>

    <!-- 解析记录表单（仅手动域名可增删改，所有字段可填） -->
    <el-dialog v-model="rDlg" :title="rEditing?'编辑解析':'添加解析'" width="520px">
      <el-form :model="rForm" label-width="100px">
        <el-form-item label="主机头">
          <el-input v-model="rForm.host" placeholder="www / @ / api" style="width:200px" />
          <el-select v-model="rForm.record_type" style="width:110px; margin-left:8px">
            <el-option label="A" value="A" />
            <el-option label="CNAME" value="CNAME" />
          </el-select>
        </el-form-item>
        <el-form-item label="项目">
          <el-select v-model="rForm.project" filterable clearable placeholder="选择项目" style="width:200px">
            <el-option v-for="p in app.projects" :key="p.id" :label="p.name" :value="p.name" />
          </el-select>
        </el-form-item>
        <el-form-item label="环境">
          <el-select v-model="rForm.env" clearable placeholder="选择环境" style="width:200px">
            <el-option v-for="e in app.environments" :key="e.id" :label="e.code" :value="e.code" />
          </el-select>
        </el-form-item>
        <el-form-item label="模块"><el-input v-model="rForm.module" style="width:200px" /></el-form-item>
        <el-form-item label="CDN">
          <el-select v-model="rForm.cdn_id" clearable placeholder="（可选）内网解析可留空" style="width:200px">
            <el-option v-for="c in app.cdns" :key="c.id" :label="c.name" :value="c.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="回源"><el-input v-model="rForm.cname" placeholder="（可选）回源地址 / CNAME" /></el-form-item>
        <el-form-item label="源站 IP"><el-input v-model="rForm.origin_ip" placeholder="（可选）源站 IP" /></el-form-item>
        <el-form-item label="证书到期">
          <el-date-picker v-model="rForm.cert_expiry_at" type="date" value-format="YYYY-MM-DD" placeholder="可手动填，或保存后点「检测证书」自动读" style="width:240px" />
        </el-form-item>
      </el-form>
      <template #footer><el-button @click="rDlg=false">取消</el-button><el-button type="primary" @click="saveReso">保存</el-button></template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Plus, Refresh, Search } from '@element-plus/icons-vue'
import { listDomains, createDomain, updateDomain, deleteDomain, listRegistrars, refreshDomain, refreshAllDomains,
  listRecords, createRecord, updateRecord, deleteRecord, checkRecordCert, checkAllRecordCerts, syncDomainRecords } from '../api/cmdb'
import { useAppStore } from '../stores/app'

const app = useAppStore()
const rows = ref([]), registrars = ref([]), loading = ref(false)
const records = ref({}), recordLoading = ref({})
const dlg = ref(false), editing = ref(false), form = ref({})
const rDlg = ref(false), rEditing = ref(false), rForm = ref({}), rCtx = ref(null)
const checking = ref({}), checkingAll = ref({}), syncingReso = ref({})
const refreshing = ref({}), refreshingAll = ref(false)
const f = ref({ keyword: '', registrar: null })
const query = ref({ keyword: '', registrar: null })
const currentPage = ref(1), pageSize = ref(10)

const filteredRows = computed(() => rows.value.filter((r) =>
  (!query.value.keyword || r.name.toLowerCase().includes(query.value.keyword.toLowerCase())) &&
  (!query.value.registrar || r.registrar_id === query.value.registrar)
))
const pagedRows = computed(() => {
  const s = (currentPage.value - 1) * pageSize.value
  return filteredRows.value.slice(s, s + pageSize.value)
})
function doSearch() { query.value = { ...f.value }; currentPage.value = 1 }
function resetFilter() { f.value = { keyword: '', registrar: null }; query.value = { ...f.value }; currentPage.value = 1 }

async function load() {
  loading.value = true
  try { rows.value = await listDomains(); registrars.value = await listRegistrars(); app.loadBasics() } catch (e) {} finally { loading.value = false }
}
async function loadRecords(ciid) {
  recordLoading.value = { ...recordLoading.value, [ciid]: true }
  try {
    const list = await listRecords(ciid)
    records.value = { ...records.value, [ciid]: list }
    const row = rows.value.find((r) => r.ci_id === ciid)
    if (row) row.reso_count = list.length
  } catch (e) { records.value = { ...records.value, [ciid]: [] } }
  finally { recordLoading.value = { ...recordLoading.value, [ciid]: false } }
}
function onExpand(row, expandedRows) {
  const open = Array.isArray(expandedRows) ? expandedRows.some((r) => r.ci_id === row.ci_id) : false
  if (open) loadRecords(row.ci_id)
}

// 主域名：手动录入(origin=manual)可编辑/删除；同步(origin=sync)只读；失效(stale)可移除
function openAdd() { editing.value = false; form.value = { name: '', registrar_id: null, dns_provider: '', expiry_at: '' }; dlg.value = true }
function openEdit(row) { editing.value = true; form.value = { ...row, expiry_at: row.expiry_at || '' }; dlg.value = true }
async function save() {
  try {
    if (editing.value) await updateDomain(form.value.ci_id, form.value)
    else await createDomain(form.value)
    ElMessage.success('已保存'); dlg.value = false; load()
  } catch (e) { ElMessage.error(e.response?.data?.error || '保存失败') }
}
async function del(row) {
  try {
    const tip = row.stale ? `该域名 GoDaddy 已无，确认从台账移除 ${row.name}？其下解析一并删除`
                          : `确认删除手动录入的域名 ${row.name}？其下解析一并删除`
    await app.showConfirm(tip)
    await deleteDomain(row.ci_id); ElMessage.success('已删除'); load()
  } catch (e) { if (e !== 'cancel') ElMessage.error('删除失败') }
}

// 解析记录：仅手动域名可增删改；同步域名只读（仅检测证书 / 失效可移除）
function openAddReso(row) { rCtx.value = row; rEditing.value = false; rForm.value = { host: '', record_type: 'A', project: '', env: '', module: '', cdn_id: null, cname: '', origin_ip: '', cert_expiry_at: '' }; rDlg.value = true }
function openEditReso(row, r) { rCtx.value = row; rEditing.value = true; rForm.value = { ...r, cert_expiry_at: r.cert_expiry_at || '' }; rDlg.value = true }
async function saveReso() {
  if (!rForm.value.host) { ElMessage.warning('主机头必填'); return }
  try {
    if (rEditing.value) await updateRecord(rForm.value.id, rForm.value)
    else await createRecord(rCtx.value.ci_id, rForm.value)
    ElMessage.success('已保存'); rDlg.value = false; loadRecords(rCtx.value.ci_id)
  } catch (e) { ElMessage.error(e.response?.data?.error || '保存失败') }
}
async function delReso(row, r) {
  try {
    await app.showConfirm(r.stale ? `该解析 GoDaddy 已删除，确认从台账移除 ${r.host}？` : `删除解析 ${r.host}？`)
    await deleteRecord(r.id); loadRecords(row.ci_id)
  } catch (e) { if (e !== 'cancel') ElMessage.error('删除失败') }
}
async function syncReso(row) {
  syncingReso.value = { ...syncingReso.value, [row.ci_id]: true }
  try {
    const r = await syncDomainRecords(row.ci_id)
    ElMessage.success(`同步完成：${r.synced_records} 条 DNS / 新导入 ${r.imported_records} 条`)
    loadRecords(row.ci_id)
  } catch (e) {
    const info = e.response?.data?.rate_limit
    if (info) ElMessage.warning(`已限流，${info.retry_after_seconds}s 后重试（窗口 ${info.window}）`)
    else ElMessage.error(e.response?.data?.error || '同步失败')
  } finally { syncingReso.value = { ...syncingReso.value, [row.ci_id]: false } }
}
async function checkCert(row, r) {
  checking.value = { ...checking.value, [r.id]: true }
  try {
    const res = await checkRecordCert(r.id)
    if (res.ok) ElMessage.success(`${res.fqdn} 证书到期 ${res.cert_expiry_at}`)
    else ElMessage.warning(`${res.fqdn} 检测失败：${res.msg}`)
    loadRecords(row.ci_id)
  } catch (e) { ElMessage.error('检测失败') } finally { checking.value = { ...checking.value, [r.id]: false } }
}
async function checkAllCerts(row) {
  checkingAll.value = { ...checkingAll.value, [row.ci_id]: true }
  try {
    const r = await checkAllRecordCerts(row.ci_id)
    ElMessage.success(`检测完成：成功 ${r.success} / 失败 ${r.failed}（共 ${r.checked} 条解析）`)
    if (records.value[row.ci_id]) loadRecords(row.ci_id)
  } catch (e) { ElMessage.error(e.response?.data?.error || '检测失败') }
  finally { checkingAll.value = { ...checkingAll.value, [row.ci_id]: false } }
}

async function refreshOne(row) {
  refreshing.value = { ...refreshing.value, [row.ci_id]: true }
  try { const r = await refreshDomain(row.ci_id); ElMessage.success(r.msg || '已刷新'); await load() }
  catch (e) { ElMessage.error('刷新失败') } finally { refreshing.value = { ...refreshing.value, [row.ci_id]: false } }
}
async function refreshAll() {
  refreshingAll.value = true
  try { const r = await refreshAllDomains(); ElMessage.success(`已刷新 ${r.count} 个域名的到期时间`); await load() }
  catch (e) { ElMessage.error('刷新失败') } finally { refreshingAll.value = false }
}
function isNear(d) { if (!d) return false; return (new Date(d) - Date.now()) / 86400000 < 30 }
onMounted(load)
</script>

<style scoped>
.warn { color: #e6a23c; font-weight: 600; }
.filter { display: flex; gap: 10px; align-items: center; }
.reso { padding: 8px 16px 14px; background: #fafbfc; }
.reso-head { margin-bottom: 8px; }
.mono { font-family: ui-monospace, Menlo, Consolas, monospace; font-size: 12px; }
.stale { text-decoration: line-through; color: #b0b3bb; }
</style>
