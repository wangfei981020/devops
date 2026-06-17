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
        <el-input v-model="f.keyword" placeholder="搜索域名" clearable :prefix-icon="Search" style="width:200px" @keyup.enter="doSearch" />
        <el-select v-model="f.project" clearable placeholder="项目" style="width:150px">
          <el-option v-for="p in app.projects" :key="p.id" :label="p.name" :value="p.name" />
        </el-select>
        <el-select v-model="f.env" clearable placeholder="环境" style="width:120px">
          <el-option v-for="e in app.environments" :key="e.id" :label="e.code" :value="e.code" />
        </el-select>
        <el-select v-model="f.module" clearable placeholder="模块" style="width:150px">
          <el-option v-for="m in moduleOptions" :key="m" :label="m" :value="m" />
        </el-select>
        <el-button type="primary" :icon="Search" @click="doSearch">搜索</el-button>
        <el-button @click="resetFilter">重置</el-button>
        <span class="muted" style="margin-left:auto">共 {{ filteredRows.length }} / {{ rows.length }} 条</span>
      </div>
    </el-card>

    <el-card shadow="never">
      <el-table :data="pagedRows" size="small" v-loading="loading">
        <el-table-column prop="name" label="域名" min-width="180" />
        <el-table-column prop="project" label="项目" width="120" />
        <el-table-column label="环境" width="90"><template #default="{ row }"><el-tag v-if="row.env" size="small">{{ row.env }}</el-tag></template></el-table-column>
        <el-table-column prop="module" label="模块" width="110" />
        <el-table-column prop="registrar_name" label="注册商" width="130" />
        <el-table-column label="域名到期" width="115"><template #default="{ row }"><span :class="{warn: isNear(row.expiry_at)}">{{ row.expiry_at || '—' }}</span></template></el-table-column>
        <el-table-column label="证书到期" width="115"><template #default="{ row }">
          <span v-if="row.cert_expiry_at" :class="{warn: isNear(row.cert_expiry_at)}">{{ row.cert_expiry_at }}</span>
          <el-tooltip v-else-if="row.cert_check_msg" :content="row.cert_check_msg"><span class="muted">检查失败</span></el-tooltip>
          <span v-else class="muted">—</span>
        </template></el-table-column>
        <el-table-column label="关联证书" width="80"><template #default="{ row }">{{ row.cert_count }}</template></el-table-column>
        <el-table-column label="操作" width="180">
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

    <el-dialog v-model="dlg" :title="editing?'编辑域名':'录入域名'" width="520px">
      <el-form :model="form" label-width="90px">
        <el-form-item label="域名"><el-input v-model="form.name" placeholder="example.com" /></el-form-item>
        <el-form-item label="项目">
          <el-select v-model="form.project" filterable clearable placeholder="选择项目（基础配置维护）" style="width:200px">
            <el-option v-for="p in app.projects" :key="p.id" :label="p.name" :value="p.name" />
          </el-select>
        </el-form-item>
        <el-form-item label="环境">
          <el-select v-model="form.env" clearable placeholder="选择环境" style="width:200px">
            <el-option v-for="e in app.environments" :key="e.id" :label="e.code" :value="e.code" />
          </el-select>
        </el-form-item>
        <el-form-item label="模块"><el-input v-model="form.module" /></el-form-item>
        <el-form-item label="负责人"><el-input v-model="form.owner" /></el-form-item>
        <el-form-item label="注册商">
          <el-select v-model="form.registrar_id" clearable placeholder="（可选，签证书用）" style="width:200px">
            <el-option v-for="r in registrars" :key="r.id" :label="r.name" :value="r.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="DNS provider"><el-input v-model="form.dns_provider" placeholder="dnspod/aliyun/cloudflare/tencent" /></el-form-item>
        <el-form-item label="到期日"><el-date-picker v-model="form.expiry_at" type="date" value-format="YYYY-MM-DD" placeholder="域名注册到期" /></el-form-item>
      </el-form>
      <template #footer><el-button @click="dlg=false">取消</el-button><el-button type="primary" @click="save">保存</el-button></template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Plus, Refresh, Search } from '@element-plus/icons-vue'
import { listDomains, createDomain, updateDomain, deleteDomain, listRegistrars, refreshDomain, refreshAllDomains } from '../api/cmdb'
import { useAppStore } from '../stores/app'

const app = useAppStore()
const rows = ref([]), registrars = ref([]), loading = ref(false)
const dlg = ref(false), editing = ref(false), form = ref({})
const refreshing = ref({}), refreshingAll = ref(false)
const f = ref({ keyword: '', project: '', env: '', module: '' })       // 输入中的条件
const query = ref({ keyword: '', project: '', env: '', module: '' })   // 已应用（点搜索后）的条件
const currentPage = ref(1), pageSize = ref(10)
const moduleOptions = computed(() => [...new Set(rows.value.map((r) => r.module).filter(Boolean))])
const filteredRows = computed(() => rows.value.filter((r) =>
  (!query.value.keyword || r.name.toLowerCase().includes(query.value.keyword.toLowerCase())) &&
  (!query.value.project || r.project === query.value.project) &&
  (!query.value.env || r.env === query.value.env) &&
  (!query.value.module || r.module === query.value.module)
))
const pagedRows = computed(() => {
  const s = (currentPage.value - 1) * pageSize.value
  return filteredRows.value.slice(s, s + pageSize.value)
})
function doSearch() { query.value = { ...f.value }; currentPage.value = 1 }
function resetFilter() { f.value = { keyword: '', project: '', env: '', module: '' }; query.value = { ...f.value }; currentPage.value = 1 }

async function load() {
  loading.value = true
  try { rows.value = await listDomains(); registrars.value = await listRegistrars(); app.loadBasics() } catch (e) {} finally { loading.value = false }
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
function openAdd() { editing.value = false; form.value = { name: '', project: '', env: '', module: '', owner: '', registrar_id: null, dns_provider: '', expiry_at: '' }; dlg.value = true }
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
    await app.showConfirm(`确认删除域名 ${row.name}？`)
    await deleteDomain(row.ci_id); ElMessage.success('已删除'); load()
  } catch (e) { if (e !== 'cancel') ElMessage.error('删除失败') }
}
function isNear(d) { if (!d) return false; return (new Date(d) - Date.now()) / 86400000 < 30 }
onMounted(load)
</script>

<style scoped>
.warn { color: #e6a23c; font-weight: 600; }
.filter { display: flex; gap: 10px; align-items: center; }
</style>
