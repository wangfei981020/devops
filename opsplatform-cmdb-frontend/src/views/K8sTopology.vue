<template>
  <div class="page">
    <div class="page-head">
      <span class="page-title">K8s 全链路关系</span>
      <span class="muted" style="margin-left:10px">正向：域名→CDN→证书→入口→Service→工作负载→Pod→节点 · 反向：节点→受影响域名</span>
    </div>

    <el-card shadow="never">
      <el-radio-group v-model="mode" style="margin-bottom:14px">
        <el-radio-button value="fwd">正向 · 按域名查链路</el-radio-button>
        <el-radio-button value="rev">反向 · 按节点查影响</el-radio-button>
      </el-radio-group>

      <!-- 正向 -->
      <div v-if="mode==='fwd'">
        <div class="bar">
          <el-input v-model="domain" clearable placeholder="输入域名，如 api.slileisure.com" style="width:320px" @keyup.enter="runFwd" />
          <el-button type="primary" :icon="Search" :loading="loading" @click="runFwd">查链路</el-button>
        </div>
        <div v-if="fwd">
          <div class="stage-row">
            <div class="stage"><div class="st-h">CDN</div><div class="st-b"><el-tag v-for="c in fwd.cdns" :key="c" size="small">{{ c }}</el-tag><span v-if="!fwd.cdns.length" class="muted">—</span></div></div>
            <span class="arrow">→</span>
            <div class="stage"><div class="st-h">域名</div><div class="st-b"><b>{{ fwd.domain }}</b></div></div>
            <span class="arrow">→</span>
            <div class="stage"><div class="st-h">证书</div><div class="st-b"><span v-if="fwd.cert">到期 {{ fwd.cert.cert_expiry }}</span><span v-else class="muted">—</span></div></div>
          </div>
          <el-alert v-if="!fwd.matched" type="info" :closable="false" style="margin-top:12px"
            title="没匹配到 Ingress/HTTPRoute" description="该域名在已纳管集群里没有对应入口（或集群没装 Gateway API / 该域名未走 K8s 入口）" />
          <div v-for="(ch,i) in fwd.chains" :key="i" class="chain-card">
            <div class="chain-top">
              <el-tag type="warning" size="small">{{ ch.entry_kind }}</el-tag>
              <b style="margin-left:6px">{{ ch.namespace }}/{{ ch.entry_name }}</b>
              <span class="muted" style="margin-left:10px">集群 {{ ch.cluster?.display_name || ch.cluster?.name }} · {{ ch.cluster?.environment }}</span>
              <el-tag v-if="ch.tls_secret" size="small" style="margin-left:8px">TLS: {{ ch.tls_secret }}</el-tag>
            </div>
            <el-table :data="ch.services" size="small" style="margin-top:8px">
              <el-table-column prop="service" label="Service" min-width="180" />
              <el-table-column label="工作负载" min-width="200"><template #default="{ row }">
                <el-tag v-for="w in row.workloads" :key="w" size="small" type="success" style="margin:2px">{{ w }}</el-tag>
                <span v-if="!row.workloads.length" class="muted">—</span>
              </template></el-table-column>
              <el-table-column label="节点" min-width="180"><template #default="{ row }">
                <el-tag v-for="n in row.nodes" :key="n" size="small" style="margin:2px">{{ n }}</el-tag>
                <span v-if="!row.nodes.length" class="muted">—</span>
              </template></el-table-column>
              <el-table-column label="Pod 数" width="80"><template #default="{ row }">{{ row.pods.length }}</template></el-table-column>
            </el-table>
          </div>
        </div>
      </div>

      <!-- 反向 -->
      <div v-else>
        <div class="bar">
          <el-select v-model="clusterId" placeholder="选集群" style="width:220px" @change="onClusterRev">
            <el-option v-for="c in clusters" :key="c.id" :label="(c.display_name||c.name)+' · '+c.environment" :value="c.id" />
          </el-select>
          <el-select v-model="node" filterable placeholder="选节点" style="width:260px">
            <el-option v-for="n in nodes" :key="n.name" :label="n.name+(Number(n.stuck)?' (卡死)':'')" :value="n.name" />
          </el-select>
          <el-button type="primary" :icon="Search" :loading="loading" @click="runRev">查影响</el-button>
        </div>
        <div v-if="rev">
          <el-alert type="warning" :closable="false" style="margin-bottom:12px"
            :title="`节点 ${rev.node} 一旦下线/卡死，将影响：工作负载 ${rev.workloads.length} · Service ${rev.services.length} · 域名 ${rev.domains.length}`" />
          <el-descriptions :column="1" border size="small">
            <el-descriptions-item label="受影响域名">
              <el-tag v-for="d in rev.domains" :key="d" type="danger" size="small" style="margin:2px">{{ d }}</el-tag>
              <span v-if="!rev.domains.length" class="muted">无（该节点上的服务没有对外 Ingress 域名）</span>
            </el-descriptions-item>
            <el-descriptions-item label="受影响 Ingress">
              <span v-if="!rev.ingresses.length" class="muted">无</span>
              <div v-for="ig in rev.ingresses" :key="ig.namespace+ig.name">{{ ig.namespace }}/{{ ig.name }} — {{ ig.hosts }}</div>
            </el-descriptions-item>
            <el-descriptions-item label="工作负载">
              <el-tag v-for="w in rev.workloads" :key="w" type="success" size="small" style="margin:2px">{{ w }}</el-tag>
            </el-descriptions-item>
            <el-descriptions-item label="Service">
              <el-tag v-for="s in rev.services" :key="s" size="small" style="margin:2px">{{ s }}</el-tag>
            </el-descriptions-item>
          </el-descriptions>
          <div class="mini-t" style="margin-top:14px">节点上的 Pod（{{ rev.pods.length }}）</div>
          <el-table :data="revPaged" size="small" max-height="320">
            <el-table-column prop="namespace" label="命名空间" width="150" />
            <el-table-column prop="pod" label="Pod" min-width="240" />
            <el-table-column prop="workload" label="工作负载" min-width="160" />
            <el-table-column prop="phase" label="状态" width="100" />
          </el-table>
          <Pager :total="rev.pods.length" v-model:page="revPage" v-model:page-size="revSize" />
        </div>
      </div>
    </el-card>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Search } from '@element-plus/icons-vue'
import { k8sTopology, k8sImpact, listK8sClusters, listK8sNodes } from '../api/cmdb'
import { usePager } from '../composables/usePager'
import Pager from '../components/Pager.vue'

const mode = ref('fwd'); const loading = ref(false)
const domain = ref(''); const fwd = ref(null)
const clusters = ref([]); const nodes = ref([]); const clusterId = ref(null); const node = ref(''); const rev = ref(null)
const revPods = computed(() => rev.value?.pods || [])
const { page: revPage, pageSize: revSize, paged: revPaged } = usePager(revPods)

async function runFwd() {
  if (!domain.value) { ElMessage.warning('输入域名'); return }
  loading.value = true
  try { fwd.value = await k8sTopology(domain.value.trim()) } catch (e) { ElMessage.error('查询失败') } finally { loading.value = false }
}

async function onClusterRev() {
  node.value = ''
  nodes.value = await listK8sNodes({ cluster_id: clusterId.value })
}
async function runRev() {
  if (!clusterId.value || !node.value) { ElMessage.warning('选集群和节点'); return }
  loading.value = true
  try { rev.value = await k8sImpact(clusterId.value, node.value) } catch (e) { ElMessage.error('查询失败') } finally { loading.value = false }
}

onMounted(async () => {
  try {
    clusters.value = await listK8sClusters()
    if (clusters.value.length) { clusterId.value = clusters.value[0].id; onClusterRev() }
  } catch (e) { /* ignore */ }
})
</script>

<style scoped>
.page-head { margin-bottom: 14px; }
.page-title { font-size: 18px; font-weight: 600; }
.muted { color: #909399; font-size: 12px; }
.bar { display: flex; gap: 10px; align-items: center; margin-bottom: 14px; }
.stage-row { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.stage { border: 1px solid #e7e9e2; border-radius: 8px; padding: 8px 14px; min-width: 120px; }
.st-h { font-size: 11px; color: #909399; text-transform: uppercase; letter-spacing: .05em; }
.st-b { margin-top: 4px; font-size: 13px; }
.arrow { color: #c0c4cc; font-weight: 700; }
.chain-card { border: 1px solid #e7e9e2; border-radius: 8px; padding: 12px 14px; margin-top: 12px; }
.chain-top { display: flex; align-items: center; flex-wrap: wrap; }
.mini-t { font-size: 13px; font-weight: 600; }
</style>
