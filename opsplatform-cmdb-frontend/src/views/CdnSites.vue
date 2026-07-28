<template>
  <div class="page">
    <div class="page-head">
      <span class="page-title">CDN 站点</span>
      <span class="muted" style="margin-left:10px">
        Cloudflare 只读数据。域名类故障排查的最前面一跳——解析到哪、走没走 CDN、回源加不加密。
      </span>
      <el-button size="small" :icon="Refresh" :loading="loading" style="margin-left:auto" @click="reload">刷新</el-button>
    </div>

    <el-card shadow="never">
      <el-tabs v-model="tab" @tab-change="onTab">
        <el-tab-pane label="站点" name="zones">
          <el-table :data="zones" size="small" v-loading="loading">
            <el-table-column prop="name" label="站点" min-width="170" />
            <el-table-column prop="account" label="账号" width="130" show-overflow-tooltip />
            <el-table-column label="状态" width="90"><template #default="{ row }">
              <el-tag size="small" :type="row.status === 'active' ? 'success' : 'warning'">{{ row.status }}</el-tag>
            </template></el-table-column>
            <el-table-column prop="plan" label="套餐" width="110" show-overflow-tooltip />
            <el-table-column label="SSL 模式" width="110"><template #default="{ row }">
              <el-tag size="small" :type="sslType(row.ssl_mode)">{{ row.ssl_mode || '—' }}</el-tag>
            </template></el-table-column>
            <el-table-column prop="dns_count" label="解析数" width="80" />
            <el-table-column label="风险" min-width="240"><template #default="{ row }">
              <span v-if="!row.risk" class="muted">—</span>
              <el-tooltip v-else :content="row.risk"><span class="risk">{{ row.risk }}</span></el-tooltip>
            </template></el-table-column>
            <el-table-column prop="synced_at" label="同步时间" width="160" />
          </el-table>
          <el-empty v-if="!loading && !zones.length" :image-size="60"
            description="没有站点数据。请先到「接入管理 → CDN」配置只读 Token 并点「同步」" />
        </el-tab-pane>

        <el-tab-pane label="解析记录" name="dns">
          <div class="filters">
            <el-select v-model="f.zone" clearable placeholder="全部站点" size="small" style="width:190px" @change="loadDns">
              <el-option v-for="z in zones" :key="z.zone_id" :label="z.name" :value="z.name" />
            </el-select>
            <el-select v-model="f.type" clearable placeholder="全部类型" size="small" style="width:120px" @change="loadDns">
              <el-option v-for="t in ['A', 'AAAA', 'CNAME', 'TXT', 'MX', 'NS', 'SRV']" :key="t" :label="t" :value="t" />
            </el-select>
            <el-input v-model="f.q" clearable placeholder="域名或解析目标关键词" size="small" style="width:230px"
              @keyup.enter="loadDns" @clear="loadDns" />
            <el-button size="small" type="primary" @click="loadDns">查询</el-button>
            <span class="muted">共 {{ dns.length }} 条</span>
          </div>
          <el-table :data="dns" size="small" v-loading="loading" max-height="560">
            <el-table-column prop="name" label="记录名" min-width="220" show-overflow-tooltip />
            <el-table-column prop="type" label="类型" width="80" />
            <el-table-column prop="content" label="解析目标" min-width="200" show-overflow-tooltip />
            <el-table-column label="经过 CDN" width="100"><template #default="{ row }">
              <el-tag size="small" :type="row.via_cdn ? 'success' : 'info'">{{ row.via_cdn ? '是' : '否（直连源站）' }}</el-tag>
            </template></el-table-column>
            <el-table-column prop="ttl" label="TTL" width="80" />
            <el-table-column prop="zone" label="站点" min-width="150" show-overflow-tooltip />
          </el-table>
          <el-empty v-if="!loading && !dns.length" description="没有匹配的解析记录" :image-size="60" />
        </el-tab-pane>

        <el-tab-pane label="一致性校验" name="check">
          <!-- scope 是这个功能能不能被采信的前提，必须显示在结论前面。
               纳管范围不完整时「查不到」不等于「不属于我方」——CMDB-005 就栽在这里。 -->
          <el-alert v-if="scope && scope.warning" type="warning" :closable="false" show-icon style="margin-bottom:12px">
            <template #title>判定范围不完整，以下结论仅供参考</template>
            {{ scope.warning }}
          </el-alert>
          <el-alert v-else-if="scope" type="success" :closable="false" show-icon style="margin-bottom:12px"
            title="判定范围完整，结论可采信" />

          <div v-if="scope" class="scope">
            比对基准：已纳管 <b>{{ scope.managed_clusters }}</b> 个集群、<b>{{ scope.managed_hosts }}</b> 台主机，
            汇总出 <b>{{ scope.owned_ingress_ips }}</b> 个我方入口 IP。
            <span class="muted">解析目标不在这个集合里，只说明「在已纳管范围内查不到」。</span>
          </div>

          <div class="filters">
            <el-select v-model="f.checkZone" clearable placeholder="全部站点" size="small" style="width:190px" @change="loadCheck">
              <el-option v-for="z in zones" :key="z.zone_id" :label="z.name" :value="z.name" />
            </el-select>
            <el-checkbox v-model="f.onlyIssues" size="small" @change="loadCheck">只看有问题的</el-checkbox>
            <span v-if="summary" class="muted">
              共 {{ summary.total }} 条 ·
              <b class="sev-high">high {{ summary.high }}</b> ·
              <b class="sev-medium">medium {{ summary.medium }}</b> ·
              low {{ summary.low }}
            </span>
          </div>

          <el-table :data="checks" size="small" v-loading="loading" max-height="520">
            <el-table-column label="级别" width="90"><template #default="{ row }">
              <el-tag size="small" :type="sevType(row.severity)">{{ row.severity }}</el-tag>
            </template></el-table-column>
            <el-table-column prop="fqdn" label="域名" min-width="200" show-overflow-tooltip />
            <el-table-column prop="type" label="类型" width="75" />
            <el-table-column prop="content" label="解析目标" min-width="160" show-overflow-tooltip />
            <el-table-column label="经过 CDN" width="90"><template #default="{ row }">
              <el-tag size="small" :type="row.via_cdn ? 'success' : 'info'">{{ row.via_cdn ? '是' : '否' }}</el-tag>
            </template></el-table-column>
            <el-table-column prop="issue" label="判定" min-width="260" show-overflow-tooltip />
            <el-table-column prop="action" label="建议" min-width="220" show-overflow-tooltip />
          </el-table>
          <el-empty v-if="!loading && !checks.length" :image-size="60"
            description="没有校验结果。若还没同步过 CDN 数据，请先到「接入管理 → CDN」点同步" />
        </el-tab-pane>
      </el-tabs>
    </el-card>
  </div>
</template>

<script setup>
import { ref, reactive } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { Refresh } from '@element-plus/icons-vue'
import { listCdnZones, listCdnDnsRecords, cdnDomainCheck } from '../api/cmdb'

const route = useRoute()
const router = useRouter()
const valid = ['zones', 'dns', 'check']
const tab = ref(valid.includes(route.query.tab) ? route.query.tab : 'zones')

const loading = ref(false)
const zones = ref([]); const dns = ref([]); const checks = ref([])
const summary = ref(null); const scope = ref(null)
const f = reactive({ zone: '', type: '', q: '', checkZone: '', onlyIssues: true })
// 已加载过的 tab 不重复请求；站点列表三个 tab 都要用，单独标记
const done = reactive({ zones: false, dns: false, check: false })

async function loadZones() {
  if (done.zones) return
  loading.value = true
  try { zones.value = await listCdnZones(); done.zones = true }
  catch (e) { ElMessage.error('加载站点失败') } finally { loading.value = false }
}

async function loadDns() {
  loading.value = true
  try {
    dns.value = await listCdnDnsRecords({ zone: f.zone || undefined, type: f.type || undefined, q: f.q || undefined })
    done.dns = true
  } catch (e) { ElMessage.error('加载解析记录失败') } finally { loading.value = false }
}

async function loadCheck() {
  loading.value = true
  try {
    const r = await cdnDomainCheck({ zone: f.checkZone || undefined, only: f.onlyIssues ? 'issues' : undefined })
    checks.value = r.items || []
    summary.value = r.summary || null
    scope.value = r.scope || null
    done.check = true
  } catch (e) { ElMessage.error('校验失败') } finally { loading.value = false }
}

function onTab(name) {
  router.replace({ query: { ...route.query, tab: name } })
  loadZones()
  if (name === 'dns' && !done.dns) loadDns()
  if (name === 'check' && !done.check) loadCheck()
}

// flexible = CDN 到源站明文，浏览器却显示小锁——这是最容易被忽略的一类风险
function sslType(m) {
  const s = String(m || '').toLowerCase()
  if (s === 'flexible') return 'danger'
  if (s === 'full') return 'warning'
  if (s === 'strict' || s === 'full_strict') return 'success'
  return 'info'
}
function sevType(s) { return s === 'high' ? 'danger' : s === 'medium' ? 'warning' : 'info' }

function reload() {
  done.zones = false
  loadZones()
  if (tab.value === 'dns') loadDns()
  if (tab.value === 'check') loadCheck()
}

loadZones()
if (tab.value === 'dns') loadDns()
if (tab.value === 'check') loadCheck()
</script>

<style scoped>
.page-head { display: flex; align-items: center; }
.filters { display: flex; gap: 8px; align-items: center; margin-bottom: 10px; flex-wrap: wrap; }
.scope { font-size: 12px; color: #606266; background: #f4f4f5; border-left: 3px solid #909399; padding: 7px 12px; margin-bottom: 10px; }
.muted { color: #909399; font-size: 12px; }
.risk { color: #e6a23c; font-size: 12px; }
.sev-high { color: #f56c6c; }
.sev-medium { color: #e6a23c; }
</style>
