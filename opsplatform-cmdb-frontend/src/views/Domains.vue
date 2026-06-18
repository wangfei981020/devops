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
                <b>解析记录</b><span class="muted" style="margin-left:8px">{{ row.name }} 的 A/CNAME 解析（同步自动导入，业务字段手动补；全量原始记录看「DNS 记录」页）</span>
                <el-button type="primary" size="small" style="float:right" @click="openAddReso(row)">+ 添加解析</el-button>
              </div>
              <el-table :data="records[row.ci_id] || []" size="small" v-loading="recordLoading[row.ci_id]">
                <el-table-column label="主机头" min-width="130"><template #default="{ row: r }"><span class="mono">{{ r.host }}</span></template></el-table-column>
                <el-table-column prop="project" label="项目" width="110" />
                <el-table-column label="环境" width="80"><template #default="{ row: r }"><el-tag v-if="r.env" size="small">{{ r.env }}</el-tag></template></el-table-column>
                <el-table-column prop="module" label="模块" width="100" />
                <el-table-column label="CDN" width="120"><template #default="{ row: r }">{{ r.cdn_name || '—' }}</template></el-table-column>
                <el-table-column label="回源 CNAME" min-width="160" show-overflow-tooltip><template #default="{ row: r }"><span class="mono">{{ r.cname || '—' }}</span></template></el-table-column>
                <el-table-column label="源站 IP" width="130"><template #default="{ row: r }"><span class="mono">{{ r.origin_ip || '—' }}</span></template></el-table-column>
                <el-table-column label="证书到期" width="110"><template #default="{ row: r }">
                  <span v-if="r.cert_expiry_at" :class="{warn: isNear(r.cert_expiry_at)}">{{ r.cert_expiry_at }}</span>
                  <el-tooltip v-else-if="r.cert_check_msg" :content="r.cert_check_msg"><span class="muted">检查失败</span></el-tooltip>
                  <span v-else class="muted">—</span>
                </template></el-table-column>
                <el-table-column prop="operator" label="操作人" width="90"><template #default="{ row: r }">{{ r.operator || '—' }}</template></el-table-column>
                <el-table-column prop="updated_at" label="更新时间" width="125" />
                <el-table-column label="操作" width="180"><template #default="{ row: r }">
                  <el-button link type="primary" :loading="checking[r.id]" @click="checkCert(row, r)">检测证书</el-button>
                  <el-button link type="primary" @click="openEditReso(row, r)">编辑</el-button>
                  <el-button link type="danger" @click="delReso(row, r)">删除</el-button>
                </template></el-table-column>
              </el-table>
              <el-empty v-if="!(records[row.ci_id] && records[row.ci_id].length) && !recordLoading[row.ci_id]" description="还没有解析记录，点右上添加" :image-size="50" />
            </div>
          </template>
        </el-table-column>
        <el-table-column prop="name" label="主域名" min-width="200" />
        <el-table-column prop="registrar_name" label="数据源/注册商" width="160"><template #default="{ row }">{{ row.registrar_name || '—' }}</template></el-table-column>
        <el-table-column label="域名到期" width="120"><template #default="{ row }"><span :class="{warn: isNear(row.expiry_at)}">{{ row.expiry_at || '—' }}</span></template></el-table-column>
        <el-table-column label="解析数" width="80"><template #default="{ row }">{{ row.reso_count ?? '—' }}</template></el-table-column>
        <el-table-column label="操作" width="190">
          <template #default="{ row }">
            <el-button link type="primary" :loading="refreshing[row.ci_id]" @click="refreshOne(row)">刷新</el-button>
            <el-button link type="primary" @click="openEdit(row)">编辑</el-button>
            <el-button link type="danger" @click="del(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
      <el-pagination v-model:current-page="currentPage" v-model:page-size="pageSize" :page-sizes="[10,20,50,100]"
        :total="filteredRows.length" layout="total, sizes, prev, pager, next" style="margin-top:12px; justify-content:flex-end" />
    </el-card>

    <!-- 主域名表单 -->
    <el-dialog v-model="dlg" :title="editing?'编辑主域名':'录入主域名'" width="480px">
      <el-form :model="form" label-width="100px">
        <el-form-item label="主域名"><el-input v-model="form.name" placeholder="example.com" /></el-form-item>
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

    <!-- 解析记录表单 -->
    <el-dialog v-model="rDlg" :title="rEditing?'编辑解析':'添加解析'" width="520px">
      <el-form :model="rForm" label-width="100px">
        <el-form-item label="主机头"><el-input v-model="rForm.host" placeholder="www / @ / api" style="width:200px" /></el-form-item>
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
        <el-form-item label="回源 CNAME"><el-input v-model="rForm.cname" placeholder="（可选）CDN 回源 CNAME" /></el-form-item>
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
  listRecords, createRecord, updateRecord, deleteRecord, checkRecordCert } from '../api/cmdb'
import { useAppStore } from '../stores/app'

const app = useAppStore()
const rows = ref([]), registrars = ref([]), loading = ref(false)
const records = ref({}), recordLoading = ref({})
const dlg = ref(false), editing = ref(false), form = ref({})
const rDlg = ref(false), rEditing = ref(false), rForm = ref({}), rCtx = ref(null)
const checking = ref({})
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

// 主域名
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
    await app.showConfirm(`确认删除主域名 ${row.name}？其下解析记录一并删除`)
    await deleteDomain(row.ci_id); ElMessage.success('已删除'); load()
  } catch (e) { if (e !== 'cancel') ElMessage.error('删除失败') }
}

// 解析记录
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
    await app.showConfirm(`删除解析 ${r.host}？`)
    await deleteRecord(r.id); loadRecords(row.ci_id)
  } catch (e) { if (e !== 'cancel') ElMessage.error('删除失败') }
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
</style>
