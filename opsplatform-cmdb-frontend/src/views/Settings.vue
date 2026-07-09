<template>
  <div class="page">
    <div class="page-head"><span class="page-title">设置</span></div>

    <!-- 通用设置 -->
    <el-card shadow="never" style="margin-bottom:14px">
      <template #header><b>指标导出</b><span class="muted" style="margin-left:8px">飞书通知 / 到期提醒 / 通知人 已移到「通知」页</span></template>
      <el-form :model="cfg" label-width="160px" style="max-width:760px">
        <el-form-item label="可导出 label 白名单">
          <el-input v-model="cfg.export_label_whitelist" placeholder="project,env,module,name,ca,registrar,team" />
          <div class="muted">只有列入白名单的自定义 label 才会进 Prometheus（控高基数，防 VM 写爆）</div>
        </el-form-item>
        <el-button type="primary" @click="saveCfg">保存设置</el-button>
      </el-form>
    </el-card>

    <!-- 注册商 / DNS 凭据 -->
    <el-card shadow="never" style="margin-bottom:14px">
      <template #header><b>注册商 / DNS 凭据</b><span class="muted" style="margin-left:8px">证书 DNS-01 验证复用这些凭据</span>
        <el-button type="primary" size="small" style="float:right" @click="openReg">+ 添加注册商</el-button></template>
      <el-table :data="rPaged" size="small">
        <el-table-column prop="name" label="名称" min-width="140" />
        <el-table-column prop="provider" label="provider" width="140" />
        <el-table-column label="凭据" width="90"><template #default="{ row }"><el-tag size="small" :type="row.has_cred?'success':'info'">{{ row.has_cred?'已配':'未配' }}</el-tag></template></el-table-column>
        <el-table-column label="操作" width="100" fixed="right">
          <template #default="{ row }">
            <div style="display:flex;gap:8px;align-items:center">
              <el-tooltip content="编辑"><el-button link type="primary" :icon="Edit" @click="editReg(row)" /></el-tooltip>
              <el-tooltip content="删除"><el-button link type="danger" :icon="Delete" @click="delReg(row)" /></el-tooltip>
            </div>
          </template>
        </el-table-column>
      </el-table>
      <el-pagination v-if="registrars.length > rSize" v-model:current-page="rPage" v-model:page-size="rSize" :page-sizes="[10,20,50,100]"
        :total="registrars.length" layout="total, sizes, prev, pager, next" style="margin-top:12px; justify-content:flex-end" />
    </el-card>

    <!-- 数据源 API 用量 -->
    <el-card shadow="never" style="margin-bottom:14px">
      <template #header><b>数据源 API 用量</b><span class="muted" style="margin-left:8px">客户端限流 {{ '50/分钟' }}（GoDaddy 真限 60，留 10 缓冲）</span>
        <el-button size="small" :icon="Refresh" :loading="usageLoading" style="float:right" @click="loadUsage">刷新用量</el-button></template>
      <el-table :data="registrars" size="small">
        <el-table-column prop="name" label="数据源" min-width="140" />
        <el-table-column prop="provider" label="provider" width="120" />
        <el-table-column label="本分钟用量" width="160"><template #default="{ row }">
          <span v-if="usage[row.id]">
            <b :class="{warn: usage[row.id].minute_used >= usage[row.id].limit}">{{ usage[row.id].minute_used }}</b> / {{ usage[row.id].limit }}
            <span class="muted">（剩 {{ Math.max(0, usage[row.id].limit - usage[row.id].minute_used) }}）</span>
          </span>
          <span v-else class="muted">—</span>
        </template></el-table-column>
        <el-table-column label="今日累计" width="100"><template #default="{ row }">{{ usage[row.id]?.today_total ?? '—' }}</template></el-table-column>
        <el-table-column label="最近一次限流" min-width="160"><template #default="{ row }">{{ usage[row.id]?.last_limited_at || '—' }}</template></el-table-column>
      </el-table>
      <div class="muted" style="margin-top:8px">用量为后端进程内计数，进程重启后清零；同步在「域名 / DNS 记录」页触发。</div>
    </el-card>

    <!-- ACME 账户 -->
    <el-card shadow="never">
      <template #header><b>ACME 账户</b><span class="muted" style="margin-left:8px">证书签发的 CA 账户（邮箱单独配）</span>
        <el-button type="primary" size="small" style="float:right" @click="openAcct">+ 添加账户</el-button></template>
      <el-table :data="aPaged" size="small">
        <el-table-column prop="email" label="邮箱" min-width="200" />
        <el-table-column prop="ca" label="CA" width="140" />
        <el-table-column label="已注册" width="90"><template #default="{ row }"><el-tag size="small" :type="row.registered?'success':'info'">{{ row.registered?'是':'待签发' }}</el-tag></template></el-table-column>
        <el-table-column label="操作" width="80" fixed="right"><template #default="{ row }"><el-tooltip content="删除"><el-button link type="danger" :icon="Delete" @click="delAcct(row)" /></el-tooltip></template></el-table-column>
      </el-table>
      <el-pagination v-if="accounts.length > aSize" v-model:current-page="aPage" v-model:page-size="aSize" :page-sizes="[10,20,50,100]"
        :total="accounts.length" layout="total, sizes, prev, pager, next" style="margin-top:12px; justify-content:flex-end" />
    </el-card>

    <!-- 注册商弹窗 -->
    <el-dialog :close-on-click-modal="false" :close-on-press-escape="false" v-model="regDlg" :title="regEdit?'编辑注册商':'添加注册商'" width="500px">
      <el-form :model="regForm" label-width="100px">
        <el-form-item label="名称"><el-input v-model="regForm.name" placeholder="如 dnspod-生产" /></el-form-item>
        <el-form-item label="provider">
          <el-select v-model="regForm.provider" style="width:240px">
            <el-option v-for="p in providers" :key="p" :label="p" :value="p" />
          </el-select>
        </el-form-item>
        <el-form-item v-for="f in credFields[regForm.provider] || []" :key="f.k" :label="f.label">
          <el-input v-model="regForm.credential[f.k]" :placeholder="regEdit?'留空=保留原值':''" show-password />
        </el-form-item>
      </el-form>
      <template #footer><el-button @click="regDlg=false">取消</el-button><el-button type="primary" @click="saveReg">保存</el-button></template>
    </el-dialog>

    <!-- ACME 账户弹窗 -->
    <el-dialog :close-on-click-modal="false" :close-on-press-escape="false" v-model="acctDlg" title="添加 ACME 账户" width="460px">
      <el-form :model="acctForm" label-width="80px">
        <el-form-item label="邮箱"><el-input v-model="acctForm.email" placeholder="可选，用飞书提醒可不填" /></el-form-item>
        <el-form-item label="CA">
          <el-select v-model="acctForm.ca" style="width:200px">
            <el-option label="Let's Encrypt" value="letsencrypt" />
            <el-option label="ZeroSSL" value="zerossl" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer><el-button @click="acctDlg=false">取消</el-button><el-button type="primary" @click="saveAcct">保存</el-button></template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Refresh, Edit, Delete } from '@element-plus/icons-vue'
import { getSettings, updateSettings, listRegistrars, createRegistrar, updateRegistrar, deleteRegistrar, listAcme, createAcme, deleteAcme, sourceUsage } from '../api/cmdb'
import { useAppStore } from '../stores/app'
import { usePaged } from '../composables/usePaged'

const app = useAppStore()
const providers = ['godaddy', 'dnspod', 'aliyun', 'cloudflare', 'tencent']
const credFields = {
  godaddy: [{ k: 'api_key', label: 'API Key' }, { k: 'api_secret', label: 'API Secret' }],
  dnspod: [{ k: 'id', label: 'ID' }, { k: 'token', label: 'Token' }],
  aliyun: [{ k: 'ak', label: 'AccessKey' }, { k: 'sk', label: 'SecretKey' }],
  cloudflare: [{ k: 'token', label: 'API Token' }],
  tencent: [{ k: 'secret_id', label: 'SecretId' }, { k: 'secret_key', label: 'SecretKey' }],
}

const cfg = ref({})
const registrars = ref([]), accounts = ref([])
const { page: rPage, size: rSize, paged: rPaged } = usePaged(registrars)
const { page: aPage, size: aSize, paged: aPaged } = usePaged(accounts)
const regDlg = ref(false), regEdit = ref(false), regForm = ref({ credential: {} })
const acctDlg = ref(false), acctForm = ref({})
const usage = ref({}), usageLoading = ref(false)

async function load() {
  cfg.value = await getSettings()
  registrars.value = await listRegistrars()
  accounts.value = await listAcme()
  loadUsage()
}
async function loadUsage() {
  if (!registrars.value.length) return
  usageLoading.value = true
  try {
    const entries = await Promise.all(registrars.value.map(async (r) => {
      try { return [r.id, await sourceUsage(r.id)] } catch (e) { return [r.id, null] }
    }))
    usage.value = Object.fromEntries(entries.filter(([, v]) => v))
  } finally { usageLoading.value = false }
}
async function saveCfg() { try { await updateSettings({ export_label_whitelist: cfg.value.export_label_whitelist || '' }); ElMessage.success('已保存') } catch (e) { ElMessage.error('保存失败') } }

function openReg() { regEdit.value = false; regForm.value = { name: '', provider: 'dnspod', credential: {}, enabled: 1 }; regDlg.value = true }
function editReg(row) { regEdit.value = true; regForm.value = { ...row, credential: {}, enabled: 1 }; regDlg.value = true }
async function saveReg() {
  try {
    if (regEdit.value) await updateRegistrar(regForm.value.id, regForm.value)
    else await createRegistrar(regForm.value)
    ElMessage.success('已保存'); regDlg.value = false; load()
  } catch (e) { ElMessage.error(e.response?.data?.error || '保存失败') }
}
async function delReg(row) {
  try { await app.showConfirm(`删除注册商 ${row.name}？`); await deleteRegistrar(row.id); ElMessage.success('已删除'); load() } catch (e) { if (e !== 'cancel') ElMessage.error('失败') }
}

function openAcct() { acctForm.value = { email: '', ca: 'letsencrypt' }; acctDlg.value = true }
async function saveAcct() {
  try { await createAcme(acctForm.value); ElMessage.success('已添加'); acctDlg.value = false; load() } catch (e) { ElMessage.error(e.response?.data?.error || '失败') }
}
async function delAcct(row) {
  try { await app.showConfirm(`删除 ACME 账户 ${row.email}？`); await deleteAcme(row.id); ElMessage.success('已删除'); load() } catch (e) { if (e !== 'cancel') ElMessage.error('失败') }
}
onMounted(load)
</script>

<style scoped>
.warn { color: #e6a23c; }
</style>
