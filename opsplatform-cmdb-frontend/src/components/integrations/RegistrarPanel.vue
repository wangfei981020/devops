<template>
  <div>
    <div class="tip">
      域名注册商 / DNS 服务商的凭据。<b>证书 DNS-01 验证复用这里的凭据</b>，所以这些 Token 需要
      <span class="warn">写 DNS 记录的权限</span>（要创建 TXT 记录），与「CDN」tab 里的只读 Token 不是同一份。
    </div>

    <div style="margin-bottom:10px">
      <el-button type="primary" size="small" @click="openReg">+ 添加注册商</el-button>
      <el-button size="small" :icon="Refresh" :loading="usageLoading" @click="loadUsage">刷新 API 用量</el-button>
      <span class="muted" style="margin-left:10px">客户端限流 50/分钟（GoDaddy 真限 60，留 10 缓冲）</span>
    </div>

    <el-table :data="registrars" size="small" v-loading="loading">
      <el-table-column prop="name" label="名称" min-width="140" />
      <el-table-column prop="provider" label="provider" width="120" />
      <el-table-column label="凭据" width="90"><template #default="{ row }">
        <el-tag size="small" :type="row.has_cred ? 'success' : 'info'">{{ row.has_cred ? '已配' : '未配' }}</el-tag>
      </template></el-table-column>
      <el-table-column label="模式" width="110"><template #default="{ row }">
        <el-tag v-if="row.dry_run" size="small" type="warning">🧪 预演</el-tag>
        <el-tag v-else size="small" type="danger">真实操作</el-tag>
      </template></el-table-column>
      <el-table-column label="本分钟用量" width="150"><template #default="{ row }">
        <span v-if="usage[row.id]">
          <b :class="{ warn: usage[row.id].minute_used >= usage[row.id].limit }">{{ usage[row.id].minute_used }}</b>
          / {{ usage[row.id].limit }}
        </span>
        <span v-else class="muted">—</span>
      </template></el-table-column>
      <el-table-column label="今日累计" width="90"><template #default="{ row }">{{ usage[row.id]?.today_total ?? '—' }}</template></el-table-column>
      <el-table-column label="最近限流" min-width="150"><template #default="{ row }">{{ usage[row.id]?.last_limited_at || '—' }}</template></el-table-column>
      <el-table-column label="操作" width="100" fixed="right"><template #default="{ row }">
        <el-button link type="primary" size="small" @click="editReg(row)">编辑</el-button>
        <el-button link type="danger" size="small" @click="delReg(row)">删除</el-button>
      </template></el-table-column>
    </el-table>
    <el-empty v-if="!loading && !registrars.length" description="还没配注册商" :image-size="60" />
    <div class="muted" style="margin-top:8px">用量为后端进程内计数，进程重启后清零；同步在「域名 / DNS 记录」页触发。</div>

    <el-dialog :close-on-click-modal="false" :close-on-press-escape="false" v-model="regDlg"
      :title="regEdit ? '编辑注册商' : '添加注册商'" width="500px">
      <el-form :model="regForm" label-width="100px">
        <el-form-item label="名称"><el-input v-model="regForm.name" placeholder="如 dnspod-生产" /></el-form-item>
        <el-form-item label="provider">
          <el-select v-model="regForm.provider" style="width:240px">
            <el-option v-for="p in providers" :key="p" :label="p" :value="p" />
          </el-select>
        </el-form-item>
        <el-form-item v-for="f in credFields[regForm.provider] || []" :key="f.k" :label="f.label">
          <el-input v-model="regForm.credential[f.k]" :placeholder="regEdit ? '留空=保留原值' : ''" show-password />
        </el-form-item>
        <el-form-item label="预演模式">
          <el-switch v-model="regForm.dry_run" />
          <span class="muted" style="margin-left:8px">开启后：解析写回 / 域名续费只打日志、不真发请求、不扣费</span>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="regDlg = false">取消</el-button>
        <el-button type="primary" @click="saveReg">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Refresh } from '@element-plus/icons-vue'
import { listRegistrars, createRegistrar, updateRegistrar, deleteRegistrar, sourceUsage } from '../../api/cmdb'
import { useAppStore } from '../../stores/app'

const app = useAppStore()
const providers = ['godaddy', 'dnspod', 'aliyun', 'cloudflare', 'tencent']
const credFields = {
  godaddy: [{ k: 'api_key', label: 'API Key' }, { k: 'api_secret', label: 'API Secret' }],
  dnspod: [{ k: 'id', label: 'ID' }, { k: 'token', label: 'Token' }],
  aliyun: [{ k: 'ak', label: 'AccessKey' }, { k: 'sk', label: 'SecretKey' }],
  cloudflare: [{ k: 'token', label: 'API Token' }],
  tencent: [{ k: 'secret_id', label: 'SecretId' }, { k: 'secret_key', label: 'SecretKey' }],
}

const registrars = ref([]); const loading = ref(false)
const regDlg = ref(false); const regEdit = ref(false); const regForm = ref({ credential: {} })
const usage = ref({}); const usageLoading = ref(false)

async function load() {
  loading.value = true
  try { registrars.value = await listRegistrars(); loadUsage() }
  catch (e) { ElMessage.error('加载失败') } finally { loading.value = false }
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

function openReg() { regEdit.value = false; regForm.value = { name: '', provider: 'dnspod', credential: {}, dry_run: false, enabled: 1 }; regDlg.value = true }
function editReg(row) { regEdit.value = true; regForm.value = { ...row, credential: {}, dry_run: !!row.dry_run, enabled: 1 }; regDlg.value = true }
async function saveReg() {
  try {
    if (regEdit.value) await updateRegistrar(regForm.value.id, regForm.value)
    else await createRegistrar(regForm.value)
    ElMessage.success('已保存'); regDlg.value = false; load()
  } catch (e) { ElMessage.error(e.response?.data?.error || '保存失败') }
}
async function delReg(row) {
  try { await app.showConfirm(`删除注册商 ${row.name}？`); await deleteRegistrar(row.id); ElMessage.success('已删除'); load() }
  catch (e) { if (e !== 'cancel') ElMessage.error('失败') }
}

onMounted(load)
</script>

<style scoped>
.tip { background: #f4f4f5; border-left: 3px solid #909399; padding: 8px 12px; margin-bottom: 12px; font-size: 12px; line-height: 1.7; color: #606266; }
.warn { color: #e6a23c; font-weight: 600; }
.muted { color: #909399; font-size: 12px; }
</style>
