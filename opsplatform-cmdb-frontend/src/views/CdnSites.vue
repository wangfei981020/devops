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

        <el-tab-pane label="规则" name="rules">
          <!-- 规则分析放在列表前面：数量逼近套餐上限、同一匹配式重复配置这类问题，
               光看列表看不出来，得先给结论再给明细。 -->
          <div v-if="ruleFindings.length" class="findings">
            <!-- 循环变量刻意不叫 f：外层 f 是筛选条件对象，同名虽不冲突但读代码时极易搞混 -->
            <div v-for="(fd, i) in ruleFindings" :key="i" class="finding" :class="fd.severity">
              <el-tag size="small" :type="sevType(fd.severity)">{{ fd.severity }}</el-tag>
              <b style="margin:0 6px">{{ fd.zone }}</b>{{ fd.issue }}
              <div class="muted">{{ fd.action }}</div>
            </div>
          </div>
          <el-alert v-else-if="ruleAnalyzed" type="success" :closable="false" show-icon
            title="规则配置未发现问题" style="margin-bottom:10px" />

          <div class="filters">
            <el-select v-model="f.ruleZone" clearable placeholder="全部站点" size="small" style="width:190px" @change="loadRules">
              <el-option v-for="z in zones" :key="z.zone_id" :label="z.name" :value="z.name" />
            </el-select>
            <el-select v-model="f.ruleSource" clearable placeholder="全部体系" size="small" style="width:150px" @change="loadRules">
              <el-option label="Page Rules（老）" value="pagerule" />
              <el-option label="Rulesets（新）" value="ruleset" />
            </el-select>
            <el-checkbox v-model="f.hideManaged" size="small">隐藏 Cloudflare 托管规则集</el-checkbox>
            <span class="muted">共 {{ shownRules.length }} 条</span>
          </div>
          <el-table :data="shownRules" size="small" v-loading="loading" max-height="520">
            <el-table-column prop="zone" label="站点" width="150" show-overflow-tooltip />
            <el-table-column prop="name" label="规则名" min-width="200" show-overflow-tooltip />
            <el-table-column label="类别" width="200"><template #default="{ row }">
              <span class="mono">{{ phaseLabel(row.phase) }}</span>
            </template></el-table-column>
            <el-table-column label="状态" width="90"><template #default="{ row }">
              <el-tag size="small" :type="row.status === 'disabled' ? 'danger' : 'success'">
                {{ row.status === 'disabled' ? '已禁用' : '启用' }}</el-tag>
            </template></el-table-column>
            <el-table-column prop="actions" label="动作" min-width="150" show-overflow-tooltip />
            <el-table-column prop="expression" label="匹配条件" min-width="260" show-overflow-tooltip />
          </el-table>
          <el-empty v-if="!loading && !shownRules.length" :image-size="60"
            description="没有规则数据。若确认配过规则，检查 token 是否有 Page Rules·Read / Config·Read 权限" />
        </el-tab-pane>

        <el-tab-pane label="边缘证书" name="certs">
          <!-- 必须写清这跟「证书 / 到期巡检」不是一套：边缘证书由 Cloudflare 自动续期，
               源站证书要自己管，两者到期时间互相独立，混起来会得出反向结论。 -->
          <el-alert type="info" :closable="false" show-icon style="margin-bottom:10px">
            <template #title>这里是 Cloudflare <b>边缘</b>上的证书</template>
            与「证书」「到期巡检」里我方<b>源站</b>的证书是两套，到期时间互相独立——
            边缘证书没过期不代表源站没过期；反过来源站过期时若 SSL 模式不是 strict，用户侧甚至看不出异常。
          </el-alert>
          <div class="filters">
            <el-select v-model="f.certZone" clearable placeholder="全部站点" size="small" style="width:190px" @change="loadCerts">
              <el-option v-for="z in zones" :key="z.zone_id" :label="z.name" :value="z.name" />
            </el-select>
            <span class="muted">共 {{ certs.length }} 张</span>
          </div>
          <el-table :data="certs" size="small" v-loading="loading" max-height="520">
            <el-table-column prop="zone" label="站点" width="150" show-overflow-tooltip />
            <el-table-column prop="hosts" label="覆盖域名" min-width="240" show-overflow-tooltip />
            <el-table-column prop="type" label="类型" width="110" />
            <el-table-column prop="issuer" label="签发方" width="150" show-overflow-tooltip />
            <el-table-column label="剩余" width="150"><template #default="{ row }">
              <span v-if="row.expires_on === ''" class="muted">{{ row.note || '未取到到期时间' }}</span>
              <span v-else>
                <el-tag size="small" :type="certType(row.days_left)">{{ row.days_left }} 天</el-tag>
                <span class="muted" style="margin-left:6px">{{ row.expires_on }}</span>
              </span>
            </template></el-table-column>
            <el-table-column prop="issue" label="提示" min-width="200" show-overflow-tooltip />
          </el-table>
          <el-empty v-if="!loading && !certs.length" :image-size="60"
            description="没有边缘证书数据。启用了 CDN 代理的站点一定有 Universal SSL，空结果基本等于 token 缺 SSL and Certificates·Read 权限" />
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
import { ref, reactive, computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { Refresh } from '@element-plus/icons-vue'
import { listCdnZones, listCdnDnsRecords, cdnDomainCheck, listCdnRules, cdnRuleAnalysis, listCdnCertificates } from '../api/cmdb'

const route = useRoute()
const router = useRouter()
const valid = ['zones', 'dns', 'rules', 'certs', 'check']
const tab = ref(valid.includes(route.query.tab) ? route.query.tab : 'zones')

const loading = ref(false)
const zones = ref([]); const dns = ref([]); const checks = ref([])
const summary = ref(null); const scope = ref(null)
const rules = ref([]); const ruleFindings = ref([]); const ruleAnalyzed = ref(false); const certs = ref([])
const f = reactive({ zone: '', type: '', q: '', checkZone: '', onlyIssues: true,
  ruleZone: '', ruleSource: '', hideManaged: true, certZone: '' })
// 已加载过的 tab 不重复请求；站点列表三个 tab 都要用，单独标记
const done = reactive({ zones: false, dns: false, rules: false, certs: false, check: false })

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


async function loadRules() {
  loading.value = true
  try {
    const r = await listCdnRules({ zone: f.ruleZone || undefined, source: f.ruleSource || undefined })
    rules.value = r.rules || []
    // 分析结果与列表一起取：先看结论再看明细，顺序反了没人会去逐条看 51 行规则
    const a = await cdnRuleAnalysis({ zone: f.ruleZone || undefined })
    ruleFindings.value = a.findings || []
    ruleAnalyzed.value = (a.total_rules || 0) > 0
    done.rules = true
  } catch (e) { ElMessage.error('加载规则失败') } finally { loading.value = false }
}

async function loadCerts() {
  loading.value = true
  try {
    const r = await listCdnCertificates({ zone: f.certZone || undefined })
    certs.value = r.certificates || []
    done.certs = true
  } catch (e) { ElMessage.error('加载证书失败') } finally { loading.value = false }
}

// Cloudflare 托管规则集（WAF/DDoS 那些）条目多、不可改，默认折叠掉，
// 否则 51 条里 30 多条是它们，自己配的规则反而找不到。
const shownRules = computed(() =>
  f.hideManaged ? rules.value.filter((r) => r.kind !== 'managed') : rules.value)

// phase 是 Cloudflare 的内部命名，直接显示没人看得懂
const PHASE_LABEL = {
  http_request_cache_settings: '缓存设置',
  http_request_dynamic_redirect: '动态重定向',
  http_request_origin: '源站路由',
  http_request_firewall_custom: 'WAF 自定义规则',
  http_request_firewall_managed: 'WAF 托管规则',
  http_request_sanitize: 'URL 规范化',
  http_ratelimit: '限速',
  ddos_l7: 'DDoS 防护(L7)',
  http_request_transform: '请求改写',
  http_response_headers_transform: '响应头改写',
}
function phaseLabel(p) { return PHASE_LABEL[p] || p || '—' }
function certType(d) { return d < 0 ? 'danger' : d <= 14 ? 'warning' : d <= 30 ? 'warning' : 'success' }

function onTab(name) {
  router.replace({ query: { ...route.query, tab: name } })
  loadZones()
  if (name === 'dns' && !done.dns) loadDns()
  if (name === 'rules' && !done.rules) loadRules()
  if (name === 'certs' && !done.certs) loadCerts()
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
  if (tab.value === 'rules') loadRules()
  if (tab.value === 'certs') loadCerts()
  if (tab.value === 'check') loadCheck()
}

loadZones()
if (tab.value === 'dns') loadDns()
if (tab.value === 'rules') loadRules()
if (tab.value === 'certs') loadCerts()
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
.mono { font-family: ui-monospace, Menlo, monospace; font-size: 12px; }
.findings { margin-bottom: 12px; }
.finding { font-size: 12px; padding: 6px 10px; margin-bottom: 6px; background: #fdf6ec; border-left: 3px solid #e6a23c; }
.finding.high { background: #fef0f0; border-left-color: #f56c6c; }
.finding.low { background: #f4f4f5; border-left-color: #909399; }
</style>
