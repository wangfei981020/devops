<template>
  <div class="page">
    <div class="page-head">
      <span class="page-title">主机</span>
      <div>
        <el-button :icon="Money" @click="openRates">成本费率</el-button>
        <el-button :icon="Cloudy" @click="openAccounts">云账号</el-button>
      </div>
    </div>
    <div class="muted" style="margin-bottom:12px">GCP Compute 主机（只读同步，不能改/删）。成本为<b>估算·目录价</b>（可在「成本费率」调整）；真实账单待接 BigQuery。</div>

    <el-card shadow="never" style="margin-bottom:12px">
      <div class="filter">
        <el-input v-model="f.kw" placeholder="搜索 实例名/IP" clearable :prefix-icon="Search" style="width:200px" />
        <el-select v-model="f.project" clearable placeholder="项目" style="width:150px"><el-option v-for="p in opts.project" :key="p" :label="p" :value="p" /></el-select>
        <el-select v-model="f.zone" clearable placeholder="区域" style="width:150px"><el-option v-for="z in opts.zone" :key="z" :label="z" :value="z" /></el-select>
        <el-select v-model="f.status" clearable placeholder="状态" style="width:130px"><el-option v-for="s in opts.status" :key="s" :label="s" :value="s" /></el-select>
        <span class="muted" style="margin-left:auto">共 {{ filtered.length }} / {{ rows.length }} 台　月估合计 ${{ monthSum }}</span>
      </div>
    </el-card>

    <el-card shadow="never">
      <el-table :data="paged" size="small" v-loading="loading">
        <el-table-column label="项目" min-width="130"><template #default="{ row }">{{ row.project_name || row.project }}</template></el-table-column>
        <el-table-column label="项目ID" width="150"><template #default="{ row }"><span class="mono">{{ row.project }}</span></template></el-table-column>
        <el-table-column prop="zone" label="区域" width="130" />
        <el-table-column label="实例名" min-width="150"><template #default="{ row }">
          <span :class="{ stale: row.stale }">{{ row.name }}</span>
          <el-tag v-if="row.stale" type="warning" size="small" style="margin-left:6px">已删</el-tag>
        </template></el-table-column>
        <el-table-column label="CPU" width="70" align="right"><template #default="{ row }">{{ row.vcpu }}核</template></el-table-column>
        <el-table-column label="内存" width="80" align="right"><template #default="{ row }">{{ gb(row.mem_mb) }}G</template></el-table-column>
        <el-table-column label="磁盘" width="90" align="right"><template #default="{ row }">{{ row.disk_total_gb }}G</template></el-table-column>
        <el-table-column label="内网IP" width="130"><template #default="{ row }"><span class="mono">{{ row.internal_ip || '—' }}</span></template></el-table-column>
        <el-table-column label="外网IP" width="130"><template #default="{ row }"><span class="mono">{{ row.external_ip || '—' }}</span></template></el-table-column>
        <el-table-column label="状态" width="90"><template #default="{ row }"><el-tag :type="stTag(row.status)" size="small">{{ stLabel(row.status) }}</el-tag></template></el-table-column>
        <el-table-column label="日均($)" width="90" align="right"><template #default="{ row }">${{ row.cost_daily }}</template></el-table-column>
        <el-table-column label="本月估($)" width="100" align="right"><template #default="{ row }">${{ row.cost_month }}</template></el-table-column>
        <el-table-column label="累计($)" width="100" align="right"><template #default="{ row }">${{ row.cost_total }}</template></el-table-column>
        <el-table-column label="操作" width="70" fixed="right"><template #default="{ row }">
          <el-tooltip content="查看详情"><el-button link type="primary" :icon="View" @click="openDetail(row)" /></el-tooltip>
        </template></el-table-column>
      </el-table>
      <el-pagination v-model:current-page="page" v-model:page-size="size" :page-sizes="[10,20,50,100]"
        :total="filtered.length" layout="total, sizes, prev, pager, next" style="margin-top:12px; justify-content:flex-end" />
    </el-card>

    <!-- 主机详情 -->
    <el-dialog :close-on-click-modal="false" :close-on-press-escape="false" v-model="dDlg" title="主机详情" width="620px">
      <template v-if="detail">
        <el-descriptions :column="2" border size="small">
          <el-descriptions-item label="项目">{{ detail.host.project_name || detail.host.project }}</el-descriptions-item>
          <el-descriptions-item label="项目ID"><span class="mono">{{ detail.host.project }}</span></el-descriptions-item>
          <el-descriptions-item label="实例名">{{ detail.host.name }}</el-descriptions-item>
          <el-descriptions-item label="区域">{{ detail.host.zone }}</el-descriptions-item>
          <el-descriptions-item label="状态"><el-tag :type="stTag(detail.host.status)" size="small">{{ stLabel(detail.host.status) }}</el-tag></el-descriptions-item>
          <el-descriptions-item label="CPU">{{ detail.host.vcpu }} vCPU</el-descriptions-item>
          <el-descriptions-item label="内存">{{ gb(detail.host.mem_mb) }} GB</el-descriptions-item>
          <el-descriptions-item label="机型">{{ detail.host.machine_type }}</el-descriptions-item>
          <el-descriptions-item label="操作系统">{{ detail.host.os || '—' }}</el-descriptions-item>
          <el-descriptions-item label="内网IP"><span class="mono">{{ detail.host.internal_ip || '—' }}</span></el-descriptions-item>
          <el-descriptions-item label="外网IP"><span class="mono">{{ detail.host.external_ip || '—' }}</span></el-descriptions-item>
          <el-descriptions-item label="创建时间">{{ detail.host.gcp_created_at || '—' }}</el-descriptions-item>
          <el-descriptions-item label="云账号">{{ detail.host.account_name }}</el-descriptions-item>
          <el-descriptions-item label="标签" :span="2">
            <el-tag v-for="(v,k) in detail.host.labels" :key="k" size="small" style="margin:2px 4px 2px 0">{{ k }}:{{ v }}</el-tag>
            <span v-if="!detail.host.labels || !Object.keys(detail.host.labels).length" class="muted">—</span>
          </el-descriptions-item>
        </el-descriptions>

        <h4 class="sec">磁盘（{{ detail.disks.length }} 块，合计 {{ detail.host.disk_total_gb }}G）</h4>
        <el-table :data="detail.disks" size="small" max-height="200">
          <el-table-column prop="name" label="名称" min-width="140" />
          <el-table-column label="大小" width="90" align="right"><template #default="{ row: d }">{{ d.size_gb }}G</template></el-table-column>
          <el-table-column label="类型" width="120"><template #default="{ row: d }"><el-tag size="small" effect="plain">{{ d.type }}</el-tag></template></el-table-column>
          <el-table-column label="启动盘" width="80"><template #default="{ row: d }"><span v-if="d.is_boot">✓</span><span v-else class="muted">—</span></template></el-table-column>
        </el-table>

        <h4 class="sec">🔗 关联业务域名（源站IP 命中）</h4>
        <div v-if="detail.related_domains.length" style="display:flex;flex-wrap:wrap;gap:6px">
          <el-tag v-for="(r,i) in detail.related_domains" :key="i" size="small" type="success" effect="plain">{{ r.fqdn }}</el-tag>
        </div>
        <div v-else class="muted">无（没有业务域名的源站IP 指向此主机）</div>

        <h4 class="sec">💰 成本估算（USD · 目录价）　<el-date-picker v-model="asOf" type="date" size="small" value-format="YYYY-MM-DD" placeholder="累计到" style="width:150px" @change="reloadDetail" /></h4>
        <el-descriptions :column="3" border size="small">
          <el-descriptions-item label="时价">${{ detail.cost_hourly }}/h</el-descriptions-item>
          <el-descriptions-item label="日均">${{ detail.host.cost_daily }}/天</el-descriptions-item>
          <el-descriptions-item label="本月估">${{ detail.host.cost_month }}</el-descriptions-item>
          <el-descriptions-item label="累计" :span="3">
            <b>${{ detail.host.cost_total }}</b>
            <span class="muted" style="margin-left:8px">（{{ detail.host.gcp_created_at }} 创建 → {{ detail.as_of }}）</span>
          </el-descriptions-item>
        </el-descriptions>
        <div class="muted" style="margin-top:6px;font-size:12px">※ 估算基于目录价，未含持续使用/承诺折扣，也未精确按实际停机时段计。真实账单待接 BigQuery。</div>
      </template>
      <template #footer><el-button @click="dDlg=false">关闭</el-button></template>
    </el-dialog>

    <!-- 云账号管理 -->
    <el-dialog :close-on-click-modal="false" :close-on-press-escape="false" v-model="acctDlg" title="云账号" width="720px">
      <div style="text-align:right;margin-bottom:10px"><el-button type="primary" size="small" :icon="Plus" @click="openAcctForm()">添加云账号</el-button></div>
      <el-table :data="accounts" size="small">
        <el-table-column prop="name" label="名称" width="130" />
        <el-table-column label="provider" width="90"><template #default="{ row }"><el-tag size="small">{{ row.provider }}</el-tag></template></el-table-column>
        <el-table-column prop="projects" label="项目" min-width="180" show-overflow-tooltip />
        <el-table-column label="最近同步" width="140"><template #default="{ row }">{{ row.last_sync_at || '—' }}</template></el-table-column>
        <el-table-column label="操作" width="180" fixed="right"><template #default="{ row }">
          <div style="display:flex;gap:6px;align-items:center">
            <el-button link type="primary" :icon="Refresh" :loading="syncing[row.id]" @click="syncAcct(row)">同步</el-button>
            <el-tooltip content="编辑"><el-button link type="primary" :icon="Edit" @click="openAcctForm(row)" /></el-tooltip>
            <el-tooltip content="删除（连主机）"><el-button link type="danger" :icon="Delete" @click="delAcct(row)" /></el-tooltip>
          </div>
        </template></el-table-column>
      </el-table>
      <el-empty v-if="!accounts.length" description="还没有云账号，点右上添加" :image-size="50" />
    </el-dialog>

    <el-dialog :close-on-click-modal="false" :close-on-press-escape="false" v-model="acctForm.dlg" :title="acctForm.id?'编辑云账号':'添加云账号'" width="560px">
      <el-form :model="acctForm" label-width="120px">
        <el-form-item label="名称"><el-input v-model="acctForm.name" placeholder="如 GCP-主账号" style="width:260px" /></el-form-item>
        <el-form-item label="项目(逗号分隔)"><el-input v-model="acctForm.projects" placeholder="g32-prod,g32-uat" style="width:320px" /></el-form-item>
        <el-form-item label="SA JSON 凭据">
          <el-input v-model="acctForm.cred_json" type="textarea" :rows="4" :placeholder="acctForm.id ? '留空=不改凭据' : 'service account JSON key（只读权限 compute.viewer 即可）'" />
        </el-form-item>
        <el-form-item label="BigQuery账单集">
          <el-input v-model="acctForm.billing_export_dataset" placeholder="（预留，二期真实账单用，可留空）" style="width:320px" />
        </el-form-item>
      </el-form>
      <template #footer><el-button @click="acctForm.dlg=false">取消</el-button><el-button type="primary" @click="saveAcct">保存</el-button></template>
    </el-dialog>

    <!-- 成本费率 -->
    <el-dialog :close-on-click-modal="false" :close-on-press-escape="false" v-model="rateDlg" title="成本费率（USD·目录价估算）" width="480px">
      <div class="muted" style="margin-bottom:12px">用于估算主机成本，可按 GCP 官方目录价调整。停机主机只计磁盘费。</div>
      <el-form :model="rateForm" label-width="150px">
        <el-form-item label="vCPU 每小时($)"><el-input-number v-model="rateForm.vcpu_hour_usd" :precision="6" :step="0.001" :controls="false" style="width:160px" /></el-form-item>
        <el-form-item label="内存 GB 每小时($)"><el-input-number v-model="rateForm.ram_gb_hour_usd" :precision="6" :step="0.001" :controls="false" style="width:160px" /></el-form-item>
        <el-form-item label="SSD盘 GB/月($)"><el-input-number v-model="rateForm.disk_ssd_gb_month" :precision="6" :step="0.01" :controls="false" style="width:160px" /></el-form-item>
        <el-form-item label="标准盘 GB/月($)"><el-input-number v-model="rateForm.disk_std_gb_month" :precision="6" :step="0.01" :controls="false" style="width:160px" /></el-form-item>
      </el-form>
      <template #footer><el-button @click="rateDlg=false">取消</el-button><el-button type="primary" @click="saveRate">保存</el-button></template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Search, View, Refresh, Edit, Delete, Plus, Money, Cloudy } from '@element-plus/icons-vue'
import { listHosts, getHost, listCloudAccounts, createCloudAccount, updateCloudAccount, deleteCloudAccount,
  syncCloudAccount, listCloudRates, updateCloudRate } from '../api/cmdb'
import { useAppStore } from '../stores/app'

const app = useAppStore()
const rows = ref([]), loading = ref(false)
const f = ref({ kw: '', project: null, zone: null, status: null })
const page = ref(1), size = ref(10)
const dDlg = ref(false), detail = ref(null), detailCiid = ref(null), asOf = ref('')
const acctDlg = ref(false), accounts = ref([]), syncing = ref({})
const acctForm = ref({ dlg: false })
const rateDlg = ref(false), rateForm = ref({}), rateId = ref(null)

function gb(mb) { return mb ? Math.round(mb / 1024 * 10) / 10 : 0 }
function stLabel(s) { return ({ RUNNING: '运行', TERMINATED: '停止', STOPPING: '停止中', PROVISIONING: '创建中', STAGING: '启动中' }[s] || s || '—') }
function stTag(s) { return s === 'RUNNING' ? 'success' : (s === 'TERMINATED' || s === 'STOPPING' ? 'info' : 'warning') }

const projName = (r) => r.project_name || r.project
const opts = computed(() => ({
  project: [...new Set(rows.value.map(projName).filter(Boolean))].sort(),
  zone: [...new Set(rows.value.map((r) => r.zone).filter(Boolean))].sort(),
  status: [...new Set(rows.value.map((r) => r.status).filter(Boolean))].sort(),
}))
const filtered = computed(() => rows.value.filter((r) => {
  const kw = f.value.kw?.toLowerCase()
  return (!kw || r.name.toLowerCase().includes(kw) || (r.internal_ip || '').includes(kw) || (r.external_ip || '').includes(kw)) &&
    (!f.value.project || projName(r) === f.value.project) &&
    (!f.value.zone || r.zone === f.value.zone) &&
    (!f.value.status || r.status === f.value.status)
}))
const paged = computed(() => { const s = (page.value - 1) * size.value; return filtered.value.slice(s, s + size.value) })
const monthSum = computed(() => Math.round(filtered.value.reduce((n, r) => n + (r.cost_month || 0), 0)))

async function load() {
  loading.value = true
  try { rows.value = await listHosts() } catch (e) {} finally { loading.value = false }
}
async function openDetail(row) { detailCiid.value = row.ci_id; asOf.value = ''; detail.value = await getHost(row.ci_id); dDlg.value = true }
async function reloadDetail() { if (detailCiid.value) detail.value = await getHost(detailCiid.value, asOf.value || undefined) }

async function openAccounts() { accounts.value = await listCloudAccounts(); acctDlg.value = true }
function openAcctForm(row) {
  acctForm.value = row
    ? { dlg: true, id: row.id, name: row.name, projects: row.projects, cred_json: '', billing_export_dataset: row.billing_export_dataset }
    : { dlg: true, id: null, name: '', projects: '', cred_json: '', billing_export_dataset: '' }
}
async function saveAcct() {
  if (!acctForm.value.name) { ElMessage.warning('名称必填'); return }
  try {
    const b = { name: acctForm.value.name, projects: acctForm.value.projects, cred_json: acctForm.value.cred_json, billing_export_dataset: acctForm.value.billing_export_dataset }
    acctForm.value.id ? await updateCloudAccount(acctForm.value.id, b) : await createCloudAccount({ ...b, provider: 'gcp' })
    ElMessage.success('已保存'); acctForm.value.dlg = false; accounts.value = await listCloudAccounts()
  } catch (e) { ElMessage.error(e.response?.data?.error || '失败') }
}
async function delAcct(row) {
  try { await app.showConfirm(`删除云账号 ${row.name}？其下同步来的主机一并清除`); await deleteCloudAccount(row.id); accounts.value = await listCloudAccounts(); load() }
  catch (e) { if (e !== 'cancel') ElMessage.error('失败') }
}
async function syncAcct(row) {
  syncing.value = { ...syncing.value, [row.id]: true }
  try { const r = await syncCloudAccount(row.id); ElMessage.success(`同步完成：${r.synced} 台，失效 ${r.stale}`); accounts.value = await listCloudAccounts(); load() }
  catch (e) { ElMessage.error(e.response?.data?.error || '同步失败') }
  finally { syncing.value = { ...syncing.value, [row.id]: false } }
}

async function openRates() {
  const list = await listCloudRates()
  const r = list[0] || {}
  rateId.value = r.id
  rateForm.value = { vcpu_hour_usd: r.vcpu_hour_usd, ram_gb_hour_usd: r.ram_gb_hour_usd, disk_ssd_gb_month: r.disk_ssd_gb_month, disk_std_gb_month: r.disk_std_gb_month }
  rateDlg.value = true
}
async function saveRate() {
  try { await updateCloudRate(rateId.value, rateForm.value); ElMessage.success('已保存，成本会按新费率重算'); rateDlg.value = false; load() }
  catch (e) { ElMessage.error('失败') }
}
onMounted(load)
</script>

<style scoped>
.filter { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; }
.mono { font-family: ui-monospace, Menlo, Consolas, monospace; font-size: 12px; }
.stale { text-decoration: line-through; color: #b0b3bb; }
.muted { color: #909399; }
.sec { font-size: 14px; margin: 18px 0 8px; display: flex; align-items: center; gap: 10px; }
</style>
