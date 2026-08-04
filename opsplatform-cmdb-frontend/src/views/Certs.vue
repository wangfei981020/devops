<template>
  <div class="page">
    <div class="page-head">
      <span class="page-title">证书</span>
      <div>
        <el-button :icon="Refresh" @click="load">刷新</el-button>
        <el-button v-if="canIssue" type="primary" :icon="Plus" @click="openApply">申请证书</el-button>
      </div>
    </div>

    <el-card shadow="never" style="margin-bottom:12px">
      <div class="filter">
        <el-input v-model="f.keyword" placeholder="搜索域名 CN" clearable :prefix-icon="Search" style="width:200px" @keyup.enter="doSearch" />
        <el-select v-model="f.project" clearable placeholder="项目" style="width:150px">
          <el-option v-for="p in app.projects" :key="p.id" :label="p.name" :value="p.name" />
        </el-select>
        <el-select v-model="f.env" clearable placeholder="环境" style="width:120px">
          <el-option v-for="e in app.environments" :key="e.id" :label="e.code" :value="e.code" />
        </el-select>
        <el-select v-model="f.status" clearable placeholder="状态" style="width:120px">
          <el-option v-for="s in statusOptions" :key="s.v" :label="s.l" :value="s.v" />
        </el-select>
        <el-button type="primary" :icon="Search" @click="doSearch">搜索</el-button>
        <el-button @click="resetFilter">重置</el-button>
        <span class="muted" style="margin-left:auto">共 {{ loadErr ? '—' : filteredRows.length + ' / ' + rows.length }} 条</span>
      </div>
    </el-card>

    <el-card shadow="never">
      <LoadError :error="loadErr" @retry="load" />
      <el-table :data="pagedRows" size="small" v-loading="loading">
        <el-table-column prop="cn" label="域名(CN)" min-width="180" />
        <el-table-column label="SAN" width="70"><template #default="{ row }">{{ row.sans?.length || 0 }}</template></el-table-column>
        <el-table-column prop="ca" label="CA" width="110" />
        <el-table-column label="到期" width="120"><template #default="{ row }">{{ row.expiry_at || '—' }}</template></el-table-column>
        <el-table-column label="自动续期" width="90"><template #default="{ row }"><el-tag size="small" :type="row.auto_renew?'success':'info'">{{ row.auto_renew?'开':'关' }}</el-tag></template></el-table-column>
        <el-table-column label="状态" width="110"><template #default="{ row }">
          <el-tag size="small" :type="statusType(row.status)">{{ statusLabel(row.status) }}</el-tag>
        </template></el-table-column>
        <el-table-column label="操作" width="148" fixed="right">
          <template #default="{ row }">
            <div style="display:flex;gap:8px;align-items:center">
              <el-tooltip content="详情"><el-button link type="primary" :icon="View" @click="$router.push('/certs/'+row.ci_id)" /></el-tooltip>
              <el-tooltip v-if="canIssue" content="续期"><el-button link type="primary" :icon="Refresh" @click="renew(row)" /></el-tooltip>
              <el-tooltip content="下载证书"><el-button link type="primary" :icon="Download" :disabled="row.status!=='active'" @click="download(row)" /></el-tooltip>
              <el-tooltip v-if="canCert" content="吊销"><el-button link type="danger" :icon="Delete" @click="revoke(row)" /></el-tooltip>
            </div>
          </template>
        </el-table-column>
      </el-table>
      <el-pagination v-model:current-page="currentPage" v-model:page-size="pageSize" :page-sizes="[10,20,50,100]"
        :total="filteredRows.length" layout="total, sizes, prev, pager, next" style="margin-top:12px; justify-content:flex-end" />
    </el-card>

    <!-- 申请证书向导 -->
    <el-dialog :close-on-click-modal="false" :close-on-press-escape="false" v-model="dlg" title="申请证书" width="600px">
      <el-form :model="form" label-width="110px">
        <el-form-item label="主域名 CN"><el-input v-model="form.cn" placeholder="example.com 或 *.example.com" /></el-form-item>
        <el-form-item label="SAN（可选）">
          <el-select v-model="form.sans" multiple filterable allow-create default-first-option placeholder="额外域名，回车添加" style="width:100%" />
        </el-form-item>
        <el-form-item label="关联域名">
          <el-select v-model="form.domain_ci_id" filterable placeholder="选 CMDB 域名（取 DNS 凭据 + 建关系）" style="width:100%">
            <el-option v-for="d in domains" :key="d.ci_id" :label="`${d.name}（${d.registrar_name||'无注册商'}）`" :value="d.ci_id" />
          </el-select>
        </el-form-item>
        <el-form-item label="验证方式">
          <el-radio-group v-model="form.challenge">
            <el-radio value="dns-01">DNS-01 自动（复用注册商 API 凭据）</el-radio>
            <el-radio value="manual-dns">手动 DNS（无 API，自己加 TXT 记录，如 GoDaddy）</el-radio>
            <el-radio value="http-01">HTTP-01（服务器 80 端口可达）</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="CA">
          <el-select v-model="form.ca" style="width:200px">
            <el-option label="Let's Encrypt" value="letsencrypt" />
          </el-select>
        </el-form-item>
        <el-form-item label="ACME 账户">
          <el-select v-model="form.acme_account_id" placeholder="选 ACME 账户（设置页配）" style="width:280px">
            <el-option v-for="a in accounts" :key="a.id" :label="`${a.email || '无邮箱'}（${a.ca}）`" :value="a.id" />
          </el-select>
        </el-form-item>
        <el-row :gutter="10">
          <el-col :span="12"><el-form-item label="项目" label-width="60px">
            <el-select v-model="form.project" filterable clearable placeholder="选择项目" style="width:100%">
              <el-option v-for="p in app.projects" :key="p.id" :label="p.name" :value="p.name" />
            </el-select>
          </el-form-item></el-col>
          <el-col :span="12"><el-form-item label="环境" label-width="60px">
            <el-select v-model="form.env" clearable placeholder="环境" style="width:100%">
              <el-option v-for="e in app.environments" :key="e.id" :label="e.code" :value="e.code" />
            </el-select>
          </el-form-item></el-col>
        </el-row>
        <el-form-item label="自动续期">
          <el-switch v-model="form.auto_renew" :active-value="1" :inactive-value="0" />
          <span style="margin-left:14px">到期前 <el-input-number v-model="form.renew_days" :min="1" :max="80" size="small" style="width:110px" /> 天续</span>
        </el-form-item>
        <el-form-item label="LE Staging">
          <el-switch v-model="form.staging" />
          <span class="muted" style="margin-left:10px">默认关=正式环境（浏览器信任）；仅首次调试/试限速时手动开（开=测试环境，签出的证书浏览器不信任，不能实际用）</span>
        </el-form-item>
      </el-form>
      <template #footer><el-button @click="dlg=false">取消</el-button><el-button type="primary" :loading="submitting" @click="submit">提交签发</el-button></template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useAuthStore } from '../stores/auth'
import { ElMessage } from 'element-plus'
import { Plus, Refresh, Search, View, Download, Delete } from '@element-plus/icons-vue'
import { listCerts, applyCert, renewCert, revokeCert, downloadCert, listDomains, listAcme } from '../api/cmdb'
import { useAppStore } from '../stores/app'
import { normalizeError } from '../api/http'
import LoadError from '../components/LoadError.vue'

// 申请/续期会真的调 CA（有速率限制），和"录入/吊销台账"是两个权限
const auth = useAuthStore()
const canCert = computed(() => auth.hasButton('manage_certs'))
const canIssue = computed(() => auth.hasButton('issue_cert'))
const app = useAppStore()
const rows = ref([]), domains = ref([]), accounts = ref([]), loading = ref(false)
const loadErr = ref('')
const dlg = ref(false), submitting = ref(false), form = ref({})

const statusOptions = [
  { v: 'active', l: '有效' }, { v: 'pending', l: '签发中' }, { v: 'await_dns', l: '待加DNS' },
  { v: 'expiring', l: '将到期' }, { v: 'expired', l: '已过期' }, { v: 'error', l: '失败' },
]
const f = ref({ keyword: '', project: '', env: '', status: '' })
const query = ref({ keyword: '', project: '', env: '', status: '' })
const currentPage = ref(1), pageSize = ref(10)
const filteredRows = computed(() => rows.value.filter((r) =>
  (!query.value.keyword || r.cn.toLowerCase().includes(query.value.keyword.toLowerCase())) &&
  (!query.value.project || r.project === query.value.project) &&
  (!query.value.env || r.env === query.value.env) &&
  (!query.value.status || r.status === query.value.status)
))
const pagedRows = computed(() => {
  const s = (currentPage.value - 1) * pageSize.value
  return filteredRows.value.slice(s, s + pageSize.value)
})
function doSearch() { query.value = { ...f.value }; currentPage.value = 1 }
function resetFilter() { f.value = { keyword: '', project: '', env: '', status: '' }; query.value = { ...f.value }; currentPage.value = 1 }

async function load() {
  loading.value = true
  loadErr.value = ''
  try { rows.value = await listCerts(); app.loadBasics() }
  catch (e) { loadErr.value = normalizeError(e).message; rows.value = [] }
  finally { loading.value = false }
}
async function openApply() {
  form.value = { cn: '', sans: [], domain_ci_id: null, challenge: 'dns-01', ca: 'letsencrypt', acme_account_id: null, project: '', env: '', module: '', auto_renew: 1, renew_days: 30, staging: false }
  domains.value = await listDomains(); accounts.value = await listAcme(); app.loadBasics()
  dlg.value = true
}
async function submit() {
  if (!form.value.cn || !form.value.acme_account_id) { ElMessage.warning('CN 与 ACME 账户必填'); return }
  submitting.value = true
  try {
    await applyCert(form.value)
    ElMessage.success('已提交，签发中（DNS-01 需等 DNS 传播，稍后刷新看状态）')
    dlg.value = false; load()
  } catch (e) { ElMessage.error(e.response?.data?.error || '提交失败') } finally { submitting.value = false }
}
async function renew(row) {
  try { await renewCert(row.ci_id); ElMessage.success('已触发续期，稍后刷新'); load() } catch (e) { ElMessage.error(e.response?.data?.error || '失败') }
}
async function revoke(row) {
  try { await app.showConfirm(`确认吊销并删除证书 ${row.cn}？`); await revokeCert(row.ci_id); ElMessage.success('已吊销'); load() } catch (e) { if (e !== 'cancel') ElMessage.error('失败') }
}
async function download(row) {
  try {
    const blob = await downloadCert(row.ci_id)
    const a = document.createElement('a')
    a.href = URL.createObjectURL(blob)
    a.download = `${row.cn.replace(/^\*\./, '')}.zip`
    a.click(); URL.revokeObjectURL(a.href)
    ElMessage.success('已下载证书 zip')
  } catch (e) { ElMessage.error('下载失败') }
}
const statusType = (s) => ({ active: 'success', pending: 'warning', await_dns: 'warning', expiring: 'warning', expired: 'danger', error: 'danger' }[s] || 'info')
const statusLabel = (s) => ({ active: '有效', pending: '签发中', await_dns: '待加DNS', expiring: '将到期', expired: '已过期', error: '失败' }[s] || s)
onMounted(load)
</script>

<style scoped>.filter { display: flex; gap: 10px; align-items: center; }</style>
