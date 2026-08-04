<template>
  <div>
    <div class="tip">证书签发用的 CA 账户。签发走 DNS-01 验证，验证时使用「域名注册商」tab 里配的凭据。</div>

    <div style="margin-bottom:10px">
      <el-button v-if="canIntegr" type="primary" size="small" @click="openAcct">+ 添加账户</el-button>
    </div>

    <el-table :data="accounts" size="small" v-loading="loading">
      <el-table-column prop="email" label="邮箱" min-width="220" />
      <el-table-column prop="ca" label="CA" width="160" />
      <el-table-column label="已注册" width="100"><template #default="{ row }">
        <el-tag size="small" :type="row.registered ? 'success' : 'info'">{{ row.registered ? '是' : '待签发' }}</el-tag>
      </template></el-table-column>
      <el-table-column label="操作" width="90" fixed="right"><template #default="{ row }">
        <el-button v-if="canIntegr" link type="danger" size="small" @click="delAcct(row)">删除</el-button>
      </template></el-table-column>
    </el-table>
    <el-empty v-if="!loading && !accounts.length" description="还没配 ACME 账户" :image-size="60" />

    <el-dialog :close-on-click-modal="false" :close-on-press-escape="false" v-model="acctDlg" title="添加 ACME 账户" width="460px">
      <el-form :model="acctForm" label-width="80px">
        <el-form-item label="邮箱"><el-input v-model="acctForm.email" placeholder="可选，用飞书提醒可不填" /></el-form-item>
        <el-form-item label="CA">
          <el-select v-model="acctForm.ca" style="width:200px">
            <el-option label="Let's Encrypt" value="letsencrypt" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="acctDlg = false">取消</el-button>
        <el-button type="primary" @click="saveAcct">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, ref, onMounted } from 'vue'
import { useAuthStore } from '../../stores/auth'
import { ElMessage } from 'element-plus'
import { listAcme, createAcme, deleteAcme } from '../../api/cmdb'
import { useAppStore } from '../../stores/app'

// 接入凭据 = 各系统的钥匙，权限最高危；同步是另一回事，不能顺带给出去
const auth = useAuthStore()
const canIntegr = computed(() => auth.hasButton('manage_integrations'))
const app = useAppStore()
const accounts = ref([]); const loading = ref(false)
const acctDlg = ref(false); const acctForm = ref({})

async function load() {
  loading.value = true
  try { accounts.value = await listAcme() } catch (e) { ElMessage.error('加载失败') } finally { loading.value = false }
}
function openAcct() { acctForm.value = { email: '', ca: 'letsencrypt' }; acctDlg.value = true }
async function saveAcct() {
  try { await createAcme(acctForm.value); ElMessage.success('已添加'); acctDlg.value = false; load() }
  catch (e) { ElMessage.error(e.response?.data?.error || '失败') }
}
async function delAcct(row) {
  try { await app.showConfirm(`删除 ACME 账户 ${row.email}？`); await deleteAcme(row.id); ElMessage.success('已删除'); load() }
  catch (e) { if (e !== 'cancel') ElMessage.error('失败') }
}

onMounted(load)
</script>

<style scoped>
.tip { background: #f4f4f5; border-left: 3px solid #909399; padding: 8px 12px; margin-bottom: 12px; font-size: 12px; line-height: 1.7; color: #606266; }
</style>
