<template>
  <div class="page">
    <el-page-header @back="$router.back()"><template #content><span class="mono">{{ cert.cn }}</span></template></el-page-header>

    <el-result v-if="notFound" icon="warning" title="证书不存在" :sub-title="notFound">
      <template #extra><el-button type="primary" @click="$router.push('/certs')">返回证书列表</el-button></template>
    </el-result>
    <template v-else>

    <el-alert v-if="cert.status === 'await_dns'" type="warning" :closable="false" show-icon style="margin-top:14px">
      <template #title>需要手动添加 DNS TXT 记录验证</template>
      <div style="line-height:2">
        去你的 DNS 控制台（GoDaddy）添加下面这条 TXT 记录，加好后点「继续验证」：
        <div>记录类型：<b>TXT</b></div>
        <div>主机记录 Name：<b class="mono">{{ txtName }}</b>　<span class="muted">（GoDaddy 里只填 <b>_acme-challenge</b> 那段，它会自动补主域名）</span></div>
        <div>记录值 Value：<b class="mono">{{ cert.challenge_value }}</b></div>
        <el-button type="primary" size="small" style="margin-top:8px" :loading="confirming" @click="confirmDns">我已添加，继续验证</el-button>
        <span v-if="polling" class="muted" style="margin-left:10px">⏳ 验证签发中，请稍候（约 1 分钟）…</span>
      </div>
    </el-alert>

    <el-row :gutter="14" style="margin-top:14px">
      <el-col :span="13">
        <el-card shadow="never"><template #header><b>基本属性</b></template>
          <el-descriptions :column="1" size="small" border>
            <el-descriptions-item label="CN">{{ cert.cn }}</el-descriptions-item>
            <el-descriptions-item label="SAN">{{ (cert.sans||[]).join(', ') || '—' }}</el-descriptions-item>
            <el-descriptions-item label="CA">{{ cert.ca }}</el-descriptions-item>
            <el-descriptions-item label="验证方式">{{ cert.challenge }}</el-descriptions-item>
            <el-descriptions-item label="状态"><el-tag size="small" :type="statusType(cert.status)">{{ statusLabel(cert.status) }}</el-tag></el-descriptions-item>
            <el-descriptions-item label="颁发时间">{{ cert.issued_at || '—' }}</el-descriptions-item>
            <el-descriptions-item label="到期时间">{{ cert.expiry_at || '—' }}</el-descriptions-item>
            <el-descriptions-item label="自动续期">{{ cert.auto_renew ? `开（到期前 ${cert.renew_days} 天）` : '关' }}</el-descriptions-item>
            <el-descriptions-item v-if="cert.last_error" label="最近错误"><span style="color:#f56c6c">{{ cert.last_error }}</span></el-descriptions-item>
          </el-descriptions>
        </el-card>
      </el-col>
      <el-col :span="11">
        <el-card shadow="never"><template #header><b>取证书（A+ 拉取式）</b></template>
          <div class="muted" style="margin-bottom:8px">目标机用 deploy_token 自助拉取，证书续期后自动更新，无需平台持有你的服务器凭据。</div>
          <p style="margin:6px 0">拉取地址：</p>
          <el-input :model-value="bundleURL" readonly size="small" class="mono" />
          <p style="margin:10px 0 6px">服务器 cron（每天）：</p>
          <el-input :model-value="cronCmd" type="textarea" :rows="3" readonly class="mono" />
        </el-card>
      </el-col>
    </el-row>

    <el-card shadow="never" style="margin-top:14px"><template #header><b>续期 / 签发历史</b></template>
      <el-table :data="cert.history || []" size="small">
        <el-table-column prop="at" label="时间" width="160" />
        <el-table-column label="动作" width="100"><template #default="{ row }">{{ ({issue:'签发',renew:'续期',revoke:'吊销'})[row.action]||row.action }}</template></el-table-column>
        <el-table-column label="结果" width="90"><template #default="{ row }"><el-tag size="small" :type="row.result==='success'?'success':'danger'">{{ row.result==='success'?'成功':'失败' }}</el-tag></template></el-table-column>
        <el-table-column prop="detail" label="详情" min-width="240" />
      </el-table>
    </el-card>
    </template>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import { getCert, certDnsReady } from '../api/cmdb'
import { normalizeError } from '../api/http'

const route = useRoute()
const cert = ref({})
const confirming = ref(false)
const polling = ref(false)
let pollTimer = null
const txtName = computed(() => (cert.value.challenge_fqdn || '').replace(/\.$/, ''))
const bundleURL = computed(() => `${location.origin}/api/certs/${route.params.id}/bundle?token=${cert.value.deploy_token || '<token>'}`)
const cronCmd = computed(() => `0 3 * * * CMDB_URL=${location.origin} /opt/fetch-cert.sh ${route.params.id} ${cert.value.deploy_token || '<token>'} /etc/nginx/certs`)
const statusType = (s) => ({ active: 'success', pending: 'warning', await_dns: 'warning', expiring: 'warning', expired: 'danger', error: 'danger' }[s] || 'info')
const statusLabel = (s) => ({ active: '有效', pending: '签发中', await_dns: '待加DNS', expiring: '将到期', expired: '已过期', error: '失败' }[s] || s)

async function confirmDns() {
  confirming.value = true
  try {
    await certDnsReady(route.params.id)
    ElMessage.success('已提交，正在验证签发，请稍候…')
    polling.value = true
    let tries = 0
    pollTimer = setInterval(async () => {
      tries++
      cert.value = await getCert(route.params.id)
      if (cert.value.status !== 'await_dns' || tries > 25) {
        clearInterval(pollTimer); polling.value = false
        if (cert.value.status === 'active') ElMessage.success('证书已签发！')
        else if (cert.value.status === 'error') ElMessage.error('签发失败：' + (cert.value.last_error || ''))
      }
    }, 6000)
  } catch (e) { ElMessage.error('提交失败') } finally { confirming.value = false }
}

// 非法/不存在的证书 ID 要给出明确的「不存在」，而不是把 el-page-header 渲染成一片空白（CMDB-018）
const notFound = ref('')
onMounted(async () => {
  try {
    cert.value = await getCert(route.params.id)
  } catch (e) {
    const err = normalizeError(e)
    notFound.value = err.status === 404 ? `没有找到 ID 为 ${route.params.id} 的证书` : err.message
    cert.value = {}
  }
})
onBeforeUnmount(() => clearInterval(pollTimer))
</script>
