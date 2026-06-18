<template>
  <div class="page">
    <div class="page-head">
      <span class="page-title">DNS 记录</span>
      <div class="filter">
        <el-select v-model="sourceId" placeholder="数据源" style="width:170px">
          <el-option v-for="s in sources" :key="s.id" :label="`${s.name}（${s.provider}）`" :value="s.id" />
        </el-select>
        <el-button type="primary" :icon="Refresh" :loading="syncing" :disabled="countdown>0" @click="doSync">
          {{ countdown>0 ? `限流中 ${countdown}s` : '从数据源同步' }}
        </el-button>
      </div>
    </div>

    <el-alert v-if="rl" type="warning" :closable="false" show-icon style="margin-bottom:12px">
      <template #title>
        已达客户端限流：本分钟已用 {{ rl.used }}/{{ rl.limit }}（窗口 {{ rl.window }}）。
        <b>{{ countdown }}s</b> 后可重试（{{ rl.retry_at }}）。{{ partialMsg }}
      </template>
    </el-alert>

    <el-card shadow="never" style="margin-bottom:12px">
      <div class="filter">
        <el-select v-model="domainCiid" filterable placeholder="选择域名" style="width:260px" @change="loadRecords">
          <el-option v-for="d in domains" :key="d.ci_id" :label="d.name" :value="d.ci_id" />
        </el-select>
        <el-select v-model="typeFilter" clearable placeholder="记录类型" style="width:130px">
          <el-option v-for="t in types" :key="t" :label="t" :value="t" />
        </el-select>
        <el-input v-model="keyword" placeholder="搜索主机/值" clearable :prefix-icon="Search" style="width:200px" />
        <span class="muted" style="margin-left:auto">共 {{ filtered.length }} 条　最近同步：{{ syncedAt || '—' }}</span>
      </div>
    </el-card>

    <el-card shadow="never">
      <el-table :data="paged" size="small" v-loading="loading">
        <el-table-column prop="type" label="类型" width="80" />
        <el-table-column prop="name" label="主机名(Name)" min-width="160" />
        <el-table-column prop="data" label="记录值(Data)" min-width="260" show-overflow-tooltip><template #default="{ row }"><span class="mono" style="font-size:12px">{{ row.data }}</span></template></el-table-column>
        <el-table-column prop="ttl" label="TTL" width="80" />
        <el-table-column label="优先级" width="80"><template #default="{ row }">{{ row.priority ?? '—' }}</template></el-table-column>
        <el-table-column label="状态" width="100"><template #default="{ row }">
          <el-tag v-if="row.protected" type="warning" size="small">🔒 受保护</el-tag>
        </template></el-table-column>
        <el-table-column label="操作" width="140"><template #default="{ row }">
          <el-tooltip v-if="row.protected" content="关键记录（_acme-challenge/NS），禁止改动"><span class="muted">禁改</span></el-tooltip>
          <span v-else class="muted">编辑/删除（下一期）</span>
        </template></el-table-column>
      </el-table>
      <el-empty v-if="!domainCiid" description="先选数据源同步、再选域名查看 DNS 记录" :image-size="60" />
      <el-pagination v-if="domainCiid" v-model:current-page="page" v-model:page-size="size" :page-sizes="[10,20,50,100]"
        :total="filtered.length" layout="total, sizes, prev, pager, next" style="margin-top:12px; justify-content:flex-end" />
    </el-card>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { ElMessage } from 'element-plus'
import { Refresh, Search } from '@element-plus/icons-vue'
import { listDomains, listRegistrars, listDnsRecords, syncSource } from '../api/cmdb'

const sources = ref([]), domains = ref([])
const sourceId = ref(null), domainCiid = ref(null)
const records = ref([]), loading = ref(false), syncing = ref(false), syncedAt = ref('')
const typeFilter = ref(''), keyword = ref('')
const page = ref(1), size = ref(20)
const rl = ref(null), countdown = ref(0), partialMsg = ref('')
let timer = null
const types = ['A', 'AAAA', 'CNAME', 'MX', 'TXT', 'NS', 'CAA', 'SRV']

const filtered = computed(() => records.value.filter((r) =>
  (!typeFilter.value || r.type === typeFilter.value) &&
  (!keyword.value || r.name.toLowerCase().includes(keyword.value.toLowerCase()) || r.data.toLowerCase().includes(keyword.value.toLowerCase()))
))
const paged = computed(() => { const s = (page.value - 1) * size.value; return filtered.value.slice(s, s + size.value) })

async function loadRecords() {
  if (!domainCiid.value) return
  loading.value = true
  try {
    records.value = await listDnsRecords(domainCiid.value)
    syncedAt.value = records.value[0]?.synced_at || ''
    page.value = 1
  } catch (e) { records.value = [] } finally { loading.value = false }
}
function startCountdown(secs) {
  countdown.value = secs
  if (timer) clearInterval(timer)
  timer = setInterval(() => {
    countdown.value -= 1
    if (countdown.value <= 0) { clearInterval(timer); timer = null; rl.value = null }
  }, 1000)
}
async function doSync() {
  if (!sourceId.value) { ElMessage.warning('先选数据源'); return }
  syncing.value = true
  try {
    const r = await syncSource(sourceId.value)
    rl.value = null; partialMsg.value = ''
    ElMessage.success(`同步完成：${r.synced_domains} 个域名 / ${r.synced_records} 条记录`)
    domains.value = await listDomains()
    if (domainCiid.value) loadRecords()
  } catch (e) {
    const info = e.response?.data?.rate_limit
    if (info) {
      rl.value = info
      partialMsg.value = e.response?.data?.partial ? `已同步部分：${e.response.data.synced_domains} 域名 / ${e.response.data.synced_records} 记录。` : ''
      startCountdown(info.retry_after_seconds || 60)
      if (e.response?.data?.partial) { domains.value = await listDomains(); if (domainCiid.value) loadRecords() }
    } else ElMessage.error(e.response?.data?.error || '同步失败')
  } finally { syncing.value = false }
}
onMounted(async () => {
  sources.value = await listRegistrars()
  domains.value = await listDomains()
})
onBeforeUnmount(() => { if (timer) clearInterval(timer) })
</script>

<style scoped>.filter { display: flex; gap: 10px; align-items: center; }</style>
