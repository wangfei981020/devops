<template>
  <div class="page">
    <div class="page-head"><span class="page-title">设置</span></div>

    <!-- 通用设置 -->
    <el-card shadow="never" style="margin-bottom:14px">
      <template #header><b>通知与指标</b></template>
      <el-form :model="cfg" label-width="160px" style="max-width:760px">
        <el-form-item label="飞书 Webhook"><el-input v-model="cfg.feishu_webhook" placeholder="飞书自定义机器人 webhook" /></el-form-item>
        <el-form-item label="到期提醒天数"><el-input v-model="cfg.remind_days" placeholder="30,15,7,1" /><span class="muted" style="margin-left:8px">逗号分隔</span></el-form-item>
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
      <el-table :data="registrars" size="small">
        <el-table-column prop="name" label="名称" min-width="140" />
        <el-table-column prop="provider" label="provider" width="140" />
        <el-table-column label="凭据" width="90"><template #default="{ row }"><el-tag size="small" :type="row.has_cred?'success':'info'">{{ row.has_cred?'已配':'未配' }}</el-tag></template></el-table-column>
        <el-table-column label="操作" width="140">
          <template #default="{ row }"><el-button link type="primary" @click="editReg(row)">编辑</el-button><el-button link type="danger" @click="delReg(row)">删除</el-button></template>
        </el-table-column>
      </el-table>
    </el-card>

    <!-- ACME 账户 -->
    <el-card shadow="never">
      <template #header><b>ACME 账户</b><span class="muted" style="margin-left:8px">证书签发的 CA 账户（邮箱单独配）</span>
        <el-button type="primary" size="small" style="float:right" @click="openAcct">+ 添加账户</el-button></template>
      <el-table :data="accounts" size="small">
        <el-table-column prop="email" label="邮箱" min-width="200" />
        <el-table-column prop="ca" label="CA" width="140" />
        <el-table-column label="已注册" width="90"><template #default="{ row }"><el-tag size="small" :type="row.registered?'success':'info'">{{ row.registered?'是':'待签发' }}</el-tag></template></el-table-column>
        <el-table-column label="操作" width="100"><template #default="{ row }"><el-button link type="danger" @click="delAcct(row)">删除</el-button></template></el-table-column>
      </el-table>
    </el-card>

    <!-- 注册商弹窗 -->
    <el-dialog v-model="regDlg" :title="regEdit?'编辑注册商':'添加注册商'" width="500px">
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
    <el-dialog v-model="acctDlg" title="添加 ACME 账户" width="460px">
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
import { getSettings, updateSettings, listRegistrars, createRegistrar, updateRegistrar, deleteRegistrar, listAcme, createAcme, deleteAcme } from '../api/cmdb'
import { useAppStore } from '../stores/app'

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
const regDlg = ref(false), regEdit = ref(false), regForm = ref({ credential: {} })
const acctDlg = ref(false), acctForm = ref({})

async function load() {
  cfg.value = await getSettings()
  registrars.value = await listRegistrars()
  accounts.value = await listAcme()
}
async function saveCfg() { try { await updateSettings(cfg.value); ElMessage.success('已保存') } catch (e) { ElMessage.error('保存失败') } }

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
