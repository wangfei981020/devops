<template>
  <div class="page">
    <div class="page-head">
      <span class="page-title">集群体检</span>
      <span class="muted" style="margin-left:10px">
        「这个集群有什么问题」的统一入口——异常按等级排好、每条带处置建议，不用自己去翻列表拼。
      </span>
      <el-select v-model="cid" size="small" style="width:230px;margin-left:auto" @change="reload">
        <el-option v-for="c in clusters" :key="c.id"
          :label="(c.display_name || c.name) + ' · ' + c.environment" :value="c.id" />
      </el-select>
      <el-button size="small" :icon="Refresh" :loading="loading" style="margin-left:8px" @click="reload">刷新</el-button>
    </div>

    <!-- 采集新鲜度放最前面：这份数据能不能信，决定下面所有结论能不能信。
         观测组件挂了的时候，依赖日志/指标的结论会静默失真，必须先看见这一点。 -->
    <el-alert v-if="fresh && !fresh.trustworthy" type="warning" :closable="false" show-icon style="margin-bottom:12px">
      <template #title>数据可信度存疑，以下结论请谨慎采信</template>
      {{ fresh.advice }}
    </el-alert>

    <el-card shadow="never">
      <el-tabs v-model="tab" @tab-change="onTab">
        <el-tab-pane :label="`体检总览${sumLabel}`" name="health">
          <LoadError :error="errHealth" title="体检未完成" @retry="loadHealth" />
          <div class="sev-bar" v-if="summary && !errHealth">
            <span class="chip critical">critical {{ summary.critical || 0 }}</span>
            <span class="chip warning">warning {{ summary.warning || 0 }}</span>
            <span class="chip info">info {{ summary.info || 0 }}</span>
          </div>
          <el-table :data="findings" size="small" v-loading="loading" max-height="560">
            <el-table-column label="等级" width="100"><template #default="{ row }">
              <el-tag size="small" :type="sevType(row.severity)">{{ row.severity }}</el-tag>
            </template></el-table-column>
            <el-table-column prop="category" label="类别" width="100" />
            <el-table-column prop="title" label="问题" min-width="230" show-overflow-tooltip />
            <el-table-column prop="count" label="数量" width="80" align="right" />
            <el-table-column prop="detail" label="说明" min-width="240" show-overflow-tooltip />
            <el-table-column prop="action" label="怎么查/怎么处置" min-width="240" show-overflow-tooltip />
            <el-table-column label="" width="80" fixed="right"><template #default="{ row }">
              <el-button v-if="row.key" link type="primary" size="small" @click="drill(row)">查看</el-button>
            </template></el-table-column>
          </el-table>
          <!-- 「未发现异常」只在体检真的跑完时才能说 -->
          <el-empty v-if="!loading && !errHealth && !findings.length" description="本次体检未发现异常" :image-size="60" />
        </el-tab-pane>

        <el-tab-pane label="配置审计" name="config">
          <!-- capability 必须显示：ConfigMap 是确定性判定，Secret 取决于该集群有没有开名录，
               两者可信度不同。不写出来的话「没报问题」会被误读成「没有问题」。 -->
          <LoadError :error="errConfig" title="配置审计未完成" @retry="loadConfig" />
          <div v-if="cfgCap && !errConfig" class="cap">
            <div><b>ConfigMap</b>：{{ cfgCap.configmap }}</div>
            <div><b>Secret</b>：{{ cfgCap.secret }}</div>
          </div>
          <el-table :data="cfgFindings" size="small" v-loading="loading" max-height="520">
            <el-table-column label="等级" width="90"><template #default="{ row }">
              <el-tag size="small" :type="sevType(row.severity)">{{ row.severity }}</el-tag>
            </template></el-table-column>
            <el-table-column prop="namespace" label="命名空间" width="150" show-overflow-tooltip />
            <el-table-column label="缺失对象" min-width="200"><template #default="{ row }">
              <el-tag size="small" type="info">{{ row.ref_kind }}</el-tag>
              <span style="margin-left:6px">{{ row.ref_name }}</span>
              <span v-if="row.ref_key" class="muted">/{{ row.ref_key }}</span>
            </template></el-table-column>
            <el-table-column prop="source" label="引用方式" width="130" />
            <el-table-column prop="pod_count" label="影响 Pod" width="100" align="right" />
            <el-table-column prop="basis" label="判定依据" min-width="220" show-overflow-tooltip />
            <el-table-column prop="action" label="处置建议" min-width="220" show-overflow-tooltip />
          </el-table>
          <el-empty v-if="!loading && !errConfig && !cfgFindings.length" :image-size="60"
            description="没有发现缺失的配置引用（注意上方 Secret 判定能力说明）" />
        </el-tab-pane>

        <el-tab-pane label="安全审计" name="security">
          <LoadError :error="errSecurity" title="安全审计未完成" @retry="loadSecurity" />
          <div class="filters">
            <el-checkbox v-model="includePlatform" size="small" @change="loadSecurity">
              包含平台组件（CNI/CSI/监控等，特权是设计使然）
            </el-checkbox>
            <span v-if="secHidden" class="muted">已隐藏 {{ secHidden }} 个平台组件</span>
            <span v-if="secSummary && !errSecurity" class="muted">
              critical {{ secSummary.critical || 0 }} · high {{ secSummary.high || 0 }} ·
              medium {{ secSummary.medium || 0 }} · info {{ secSummary.info || 0 }}
            </span>
          </div>
          <el-table :data="secFindings" size="small" v-loading="loading" max-height="520">
            <el-table-column label="等级" width="90"><template #default="{ row }">
              <el-tag size="small" :type="sevType(row.severity)">{{ row.severity }}</el-tag>
            </template></el-table-column>
            <el-table-column prop="namespace" label="命名空间" width="150" show-overflow-tooltip />
            <el-table-column prop="workload" label="工作负载" min-width="180" show-overflow-tooltip>
              <template #default="{ row }">{{ row.workload || row.pod }}</template>
            </el-table-column>
            <el-table-column label="风险" min-width="360"><template #default="{ row }">
              <div v-for="(r, i) in row.risks" :key="i" class="risk-line">{{ r }}</div>
            </template></el-table-column>
            <el-table-column label="平台组件" width="100"><template #default="{ row }">
              <el-tag v-if="row.platform_component" size="small" type="info">是</el-tag>
              <span v-else class="muted">—</span>
            </template></el-table-column>
          </el-table>
          <el-empty v-if="!loading && !errSecurity && !secFindings.length" :image-size="60"
            description="未发现有风险的安全上下文配置" />
        </el-tab-pane>
      </el-tabs>
    </el-card>

    <!-- 下钻抽屉：光有「56 个」没法处置，得能看到是哪 56 个。
         列名由后端给出（与 SELECT 顺序对应），前端不写死——加新体检项时前端不用改。 -->
    <el-drawer v-model="dw" :title="dwTitle" size="70%" :close-on-click-modal="false">
      <el-alert v-if="dwUnsupported" type="info" :closable="false" show-icon style="margin-bottom:10px">
        <template #title>这一项没有明细清单</template>
        {{ dwUnsupported }}
      </el-alert>
      <div v-if="dwNote" class="cap" style="margin-bottom:10px">{{ dwNote }}</div>
      <el-alert v-if="dwTruncated" type="warning" :closable="false" show-icon style="margin-bottom:10px"
        :title="dwTruncated" />
      <el-table v-if="dwRows.length" :data="dwRows" size="small" v-loading="dwLoading" max-height="calc(100vh - 260px)">
        <el-table-column v-for="(c, i) in dwColumns" :key="i" :label="c" :min-width="colWidth(c)"
          show-overflow-tooltip>
          <template #default="{ row }">{{ row[i] ?? '—' }}</template>
        </el-table-column>
      </el-table>
      <el-empty v-else-if="!dwLoading && !dwUnsupported" description="没有明细数据" :image-size="60" />
    </el-drawer>
  </div>
</template>

<script setup>
import { ref, computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { Refresh } from '@element-plus/icons-vue'
import { listK8sClusters, clusterHealth, configAudit, securityAudit, k8sSyncState, healthDetail } from '../api/cmdb'
import { normalizeError } from '../api/http'
import LoadError from '../components/LoadError.vue'

const route = useRoute()
const router = useRouter()
const valid = ['health', 'config', 'security']
const tab = ref(valid.includes(route.query.tab) ? route.query.tab : 'health')

const loading = ref(false)
// 三个 tab 各自记自己的失败：配置审计挂了不该让体检总览也显示报错，反之亦然
const errHealth = ref(''); const errConfig = ref(''); const errSecurity = ref('')
const clusters = ref([]); const cid = ref(null)
const findings = ref([]); const summary = ref(null); const fresh = ref(null)
const cfgFindings = ref([]); const cfgCap = ref(null)
const secFindings = ref([]); const secSummary = ref(null); const secHidden = ref(0)
const includePlatform = ref(false)
const done = { health: false, config: false, security: false }

// 下钻抽屉
const dw = ref(false); const dwLoading = ref(false); const dwTitle = ref('')
const dwColumns = ref([]); const dwRows = ref([]); const dwNote = ref('')
const dwUnsupported = ref(''); const dwTruncated = ref('')

async function drill(row) {
  dwTitle.value = `${row.title}（${row.count} 项）`
  dw.value = true
  dwLoading.value = true
  dwColumns.value = []; dwRows.value = []; dwNote.value = ''
  dwUnsupported.value = ''; dwTruncated.value = ''
  try {
    const r = await healthDetail({ cluster_id: cid.value, key: row.key })
    dwColumns.value = r.columns || []
    dwRows.value = r.rows || []
    dwNote.value = r.note || ''
    dwUnsupported.value = r.unsupported || ''
    dwTruncated.value = r.truncated || ''
  } catch (e) { ElMessage.error('取明细失败') } finally { dwLoading.value = false }
}

// 列宽按语义给：名字类的长、数字类的短，避免长列被挤成一团
function colWidth(c) {
  if (/Pod|名称|镜像|错误|Conditions/.test(c)) return 220
  if (/命名空间|节点|工作负载|PVC|HPA|storageClass|OS|资源类型/.test(c)) return 150
  return 100
}

const sumLabel = computed(() => {
  const s = summary.value
  if (!s) return ''
  const c = (s.critical || 0) + (s.warning || 0)
  return c ? ` (${c})` : ''
})

function sevType(s) {
  return { critical: 'danger', high: 'danger', warning: 'warning', medium: 'warning' }[s] || 'info'
}

async function loadClusters() {
  try {
    clusters.value = await listK8sClusters()
  } catch (e) {
    // 集群列表取不到 => 后面每个 tab 都没法体检，直接把错误摊开说
    const msg = normalizeError(e).message
    errHealth.value = errConfig.value = errSecurity.value = '加载集群列表失败：' + msg
    clusters.value = []
    return
  }
  if (!cid.value && clusters.value.length) {
    const q = Number(route.query.cluster_id)
    cid.value = clusters.value.some((c) => c.id === q) ? q : clusters.value[0].id
  }
}

async function loadHealth() {
  // 没有集群可选（多半是集群列表就没加载出来）时，不能什么都不做——
  // 那样页面会停在「本次体检未发现异常」的空态上，等于报告"没问题"（CMDB-013）
  if (!cid.value) {
    findings.value = []; summary.value = null
    if (!errHealth.value) errHealth.value = '没有可体检的集群：集群列表为空或未加载成功'
    return
  }
  loading.value = true
  errHealth.value = ''
  try {
    const r = await clusterHealth({ cluster_id: cid.value })
    findings.value = r.findings || []
    summary.value = r.summary || null
    done.health = true
    // 顺带取一次新鲜度：结论可信度的前提，放在页面最上方
    try {
      const s = await k8sSyncState({ cluster_id: cid.value })
      fresh.value = (s.clusters || []).find((x) => x.cluster_id === cid.value) || null
    } catch (e) { fresh.value = null }
  } catch (e) {
    // 体检失败必须清空结论：留着上一次的 findings 会让人以为这就是本次结果
    errHealth.value = normalizeError(e).message
    findings.value = []; summary.value = null
  } finally { loading.value = false }
}

async function loadConfig() {
  if (!cid.value) {
    cfgFindings.value = []; cfgCap.value = null
    if (!errConfig.value) errConfig.value = '没有可审计的集群：集群列表为空或未加载成功'
    return
  }
  loading.value = true
  errConfig.value = ''
  try {
    const r = await configAudit({ cluster_id: cid.value })
    cfgFindings.value = r.findings || []
    cfgCap.value = r.capability || null
    done.config = true
  } catch (e) {
    errConfig.value = normalizeError(e).message
    cfgFindings.value = []; cfgCap.value = null
  } finally { loading.value = false }
}

async function loadSecurity() {
  if (!cid.value) {
    secFindings.value = []; secSummary.value = null
    if (!errSecurity.value) errSecurity.value = '没有可审计的集群：集群列表为空或未加载成功'
    return
  }
  loading.value = true
  errSecurity.value = ''
  try {
    const r = await securityAudit({
      cluster_id: cid.value,
      include_platform: includePlatform.value ? '1' : undefined,
    })
    secFindings.value = r.findings || []
    secSummary.value = r.summary || null
    secHidden.value = r.platform_hidden || 0
    done.security = true
  } catch (e) {
    errSecurity.value = normalizeError(e).message
    secFindings.value = []; secSummary.value = null
  } finally { loading.value = false }
}

function loadTab(name) {
  if (name === 'health') loadHealth()
  if (name === 'config') loadConfig()
  if (name === 'security') loadSecurity()
}

function onTab(name) {
  router.replace({ query: { ...route.query, tab: name } })
  if (!done[name]) loadTab(name)
}

async function reload() {
  done.health = done.config = done.security = false
  router.replace({ query: { ...route.query, cluster_id: cid.value } })
  loadTab(tab.value)
}

;(async () => {
  await loadClusters()
  loadTab(tab.value)
})()
</script>

<style scoped>
.page-head { display: flex; align-items: center; }
.filters { display: flex; gap: 12px; align-items: center; margin-bottom: 10px; flex-wrap: wrap; }
.muted { color: #909399; font-size: 12px; }
.sev-bar { margin-bottom: 10px; display: flex; gap: 8px; }
.chip { font-size: 12px; padding: 2px 10px; border-radius: 10px; }
.chip.critical { background: #fef0f0; color: #f56c6c; }
.chip.warning { background: #fdf6ec; color: #e6a23c; }
.chip.info { background: #f4f4f5; color: #909399; }
.cap { font-size: 12px; color: #606266; background: #f4f4f5; border-left: 3px solid #909399; padding: 8px 12px; margin-bottom: 10px; line-height: 1.8; }
.risk-line { font-size: 12px; line-height: 1.6; }
</style>
