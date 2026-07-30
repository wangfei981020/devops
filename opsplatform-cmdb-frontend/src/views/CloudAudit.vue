<template>
  <div class="page">
    <div class="page-head">
      <span class="page-title">云平台审计</span>
      <span class="muted" style="margin-left:10px">
        GCP 侧的权限与解析。与「DNS 记录」不同——那里是我方注册商台账，这里是 GCP 托管的 Cloud DNS。
      </span>
      <el-button size="small" :icon="Refresh" :loading="loading" style="margin-left:auto" @click="reload">刷新</el-button>
    </div>

    <el-card shadow="never">
      <el-tabs v-model="tab" @tab-change="onTab">
      <el-tab-pane label="IAM 权限" name="iam">
      <!-- 空结果必须区分「确实没风险」和「压根没采到」：任何 GCP 项目都至少有一条绑定，
           所以一条都没有只可能是没采成功。这条提示由后端给出，原样透出。 -->
      <el-alert v-if="emptyHint" type="warning" :closable="false" show-icon style="margin-bottom:12px">
        <template #title>没有 IAM 数据 — 这不代表没有风险</template>
        {{ emptyHint }}
      </el-alert>

      <div class="filters">
        <el-input v-model="project" clearable placeholder="项目 ID（可选）" size="small" style="width:220px" @change="load" />
        <el-checkbox v-model="onlyIssues" size="small" @change="load">只看有风险的</el-checkbox>
        <span v-if="summary && Object.keys(summary).length" class="muted">
          <b class="crit">critical {{ summary.critical || 0 }}</b> ·
          <b class="high">high {{ summary.high || 0 }}</b> ·
          <b class="med">medium {{ summary.medium || 0 }}</b>
        </span>
        <span class="muted">共 {{ total }} 条绑定</span>
      </div>

      <el-table :data="items" size="small" v-loading="loading" max-height="600">
        <el-table-column label="风险" width="100"><template #default="{ row }">
          <el-tag v-if="row.severity" size="small" :type="sevType(row.severity)">{{ row.severity }}</el-tag>
          <span v-else class="muted">—</span>
        </template></el-table-column>
        <el-table-column prop="project" label="项目" width="170" show-overflow-tooltip />
        <el-table-column prop="role" label="角色" min-width="230" show-overflow-tooltip />
        <el-table-column label="主体" min-width="280"><template #default="{ row }">
          <el-tag size="small" :type="memberType(row.member_type)">{{ row.member_type }}</el-tag>
          <span style="margin-left:6px">{{ row.member || '（所有人）' }}</span>
        </template></el-table-column>
        <el-table-column prop="issue" label="问题说明" min-width="300" show-overflow-tooltip />
      </el-table>
      <el-empty v-if="!loading && !items.length && !emptyHint" :image-size="60"
        description="没有匹配的 IAM 绑定" />
      </el-tab-pane>

      <el-tab-pane label="Cloud DNS" name="dns">
        <!-- 一致性校验放前面：两边都配了解析且目标不一致时，只有 NS 指向的那边生效，
             另一边改了完全没效果——这是「改了没生效」最常见的原因。 -->
        <el-alert v-if="cmp && cmp.not_comparable" type="warning" :closable="false" show-icon style="margin-bottom:10px">
          <template #title>本次比对不成立</template>
          {{ cmp.not_comparable }}
        </el-alert>
        <div v-else-if="cmp" class="cmp">
          GCP Cloud DNS <b>{{ cmp.gcp_fqdn_count }}</b> 个域名 ·
          Cloudflare <b>{{ cmp.cloudflare_fqdn_count }}</b> 个 ·
          <span :class="cmp.conflict_count ? 'bad' : 'ok'">冲突 {{ cmp.conflict_count }}</span>
        </div>
        <div v-if="conflicts.length" class="conflicts">
          <div v-for="(c, i) in conflicts" :key="i" class="conflict">
            <b>{{ c.fqdn }}</b>
            <div class="muted">GCP：{{ c.gcp }}</div>
            <div class="muted">Cloudflare：{{ c.cloudflare }}</div>
            <div class="muted">{{ c.action }}</div>
          </div>
        </div>

        <el-alert v-if="dnsHint" type="info" :closable="false" show-icon style="margin:10px 0">
          {{ dnsHint }}
        </el-alert>

        <div class="filters">
          <el-input v-model="dnsProject" clearable placeholder="项目 ID（可选）" size="small" style="width:200px" @change="loadDns" />
          <el-input v-model="dnsQ" clearable placeholder="域名或解析目标关键词" size="small" style="width:230px" @change="loadDns" />
          <span class="muted">托管区 {{ zones.length }} 个 · 记录 {{ records.length }} 条</span>
        </div>
        <el-table v-if="zones.length" :data="zones" size="small" style="margin-bottom:12px">
          <el-table-column prop="project" label="项目" width="170" show-overflow-tooltip />
          <el-table-column prop="zone_name" label="托管区" width="180" show-overflow-tooltip />
          <el-table-column prop="dns_name" label="根域名" min-width="180" />
          <el-table-column label="可见性" width="100"><template #default="{ row }">
            <el-tag size="small" :type="row.visibility === 'public' ? 'warning' : 'info'">{{ row.visibility || '—' }}</el-tag>
          </template></el-table-column>
          <el-table-column prop="record_count" label="记录数" width="90" align="right" />
          <el-table-column prop="name_servers" label="NS" min-width="220" show-overflow-tooltip />
        </el-table>
        <el-table :data="records" size="small" v-loading="loading" max-height="440">
          <el-table-column prop="name" label="记录名" min-width="240" show-overflow-tooltip />
          <el-table-column prop="type" label="类型" width="80" />
          <el-table-column prop="targets" label="解析目标" min-width="240" show-overflow-tooltip />
          <el-table-column prop="ttl" label="TTL" width="80" align="right" />
          <el-table-column prop="zone" label="托管区" width="160" show-overflow-tooltip />
        </el-table>
        <el-empty v-if="!loading && !records.length && !dnsHint" description="没有解析记录" :image-size="60" />
      </el-tab-pane>
      </el-tabs>
    </el-card>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { Refresh } from '@element-plus/icons-vue'
import { cloudIamAudit, listCloudDns, dnsConsistency } from '../api/cmdb'

const route = useRoute(); const router = useRouter()
const valid = ['iam', 'dns']
const tab = ref(valid.includes(route.query.tab) ? route.query.tab : 'iam')

const loading = ref(false)
const items = ref([]); const summary = ref(null); const total = ref(0); const emptyHint = ref('')
const project = ref(''); const onlyIssues = ref(true)
const zones = ref([]); const records = ref([]); const dnsHint = ref('')
const cmp = ref(null); const conflicts = ref([])
const dnsProject = ref(''); const dnsQ = ref('')
const done = { iam: false, dns: false }

function sevType(s) {
  return { critical: 'danger', high: 'danger', medium: 'warning' }[s] || 'info'
}
// allUsers / allAuthenticatedUsers 标红：它们意味着互联网上任何人，是最危险的两种主体
function memberType(t) {
  return ['allUsers', 'allAuthenticatedUsers'].includes(t) ? 'danger'
    : t === 'serviceAccount' ? 'warning' : 'info'
}

async function load() {
  loading.value = true
  emptyHint.value = ''
  try {
    const r = await cloudIamAudit({
      project: project.value || undefined,
      only: onlyIssues.value ? 'issues' : undefined,
    })
    items.value = r.items || []
    summary.value = r.summary || null
    total.value = r.total || 0
    if (r.empty_hint) emptyHint.value = r.empty_hint
  } catch (e) { ElMessage.error('加载失败') } finally { loading.value = false }
}

async function loadDns() {
  loading.value = true
  dnsHint.value = ''
  try {
    const r = await listCloudDns({ project: dnsProject.value || undefined, q: dnsQ.value || undefined })
    zones.value = r.zones || []
    records.value = r.records || []
    if (r.empty_hint) dnsHint.value = r.empty_hint
    // 一致性校验是全量比对，不吃筛选条件
    const c = await dnsConsistency()
    cmp.value = c
    conflicts.value = c.conflicts || []
    done.dns = true
  } catch (e) { ElMessage.error('加载 Cloud DNS 失败') } finally { loading.value = false }
}

function onTab(name) {
  router.replace({ query: { ...route.query, tab: name } })
  if (done[name]) return
  if (name === 'iam') load()
  if (name === 'dns') loadDns()
}

function reload() {
  done.iam = done.dns = false
  if (tab.value === 'iam') load(); else loadDns()
}

if (tab.value === 'iam') { load(); done.iam = true } else { loadDns() }
</script>

<style scoped>
.page-head { display: flex; align-items: center; }
.filters { display: flex; gap: 12px; align-items: center; margin-bottom: 10px; flex-wrap: wrap; }
.muted { color: #909399; font-size: 12px; }
.crit { color: #f56c6c; }
.high { color: #f56c6c; }
.med { color: #e6a23c; }
.bad { color: #f56c6c; }
.ok { color: #67c23a; }
.cmp { font-size: 12px; color: #606266; background: #f4f4f5; border-left: 3px solid #909399; padding: 7px 12px; margin-bottom: 10px; }
.conflicts { margin-bottom: 10px; }
.conflict { font-size: 12px; padding: 7px 10px; margin-bottom: 6px; background: #fdf6ec; border-left: 3px solid #e6a23c; line-height: 1.7; }
</style>
