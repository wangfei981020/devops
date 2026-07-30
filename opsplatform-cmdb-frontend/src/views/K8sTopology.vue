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
          <el-select v-model="fProject" clearable placeholder="项目(筛)" style="width:150px" @change="fEnv='';domain=''">
            <el-option v-for="p in projects" :key="p" :label="p" :value="p" />
          </el-select>
          <el-select v-model="fEnv" clearable placeholder="环境(筛)" style="width:130px" @change="domain=''">
            <el-option v-for="e in envs" :key="e" :label="e" :value="e" />
          </el-select>
          <el-select v-model="domain" filterable clearable allow-create default-first-option placeholder="选/搜域名(可自输)" style="width:340px">
            <el-option v-for="d in domainOpts" :key="d.name" :label="d.name + (d.module?' · '+d.module:'')" :value="d.name" />
          </el-select>
          <el-button type="primary" :icon="Search" :loading="loading" @click="runFwd">查链路</el-button>
        </div>
        <div v-if="fwd">
          <div class="stage-row">
            <div class="stage" :class="{ dim: !cdnRecs.length && !fwd.edge?.cdn }">
              <div class="st-h">CDN / 源站</div><div class="st-b">
              <!-- 优先用 Cloudflare 接入数据（cdn_dns_records）；旧域名台账里很多域名没登记，
                   以前只查那张表所以这一格长期是空的。 -->
              <template v-if="cdnRecs.length">
                <el-tag size="small" :type="fwd.cdn?.via_cdn ? 'warning' : 'info'">
                  {{ fwd.cdn?.via_cdn ? 'Cloudflare 代理' : '已解析·未代理' }}
                </el-tag>
                <div v-for="(r, i) in cdnRecs" :key="i" class="muted" style="margin-top:3px">
                  {{ r.type }} → {{ r.content }}
                </div>
                <div v-if="fwd.cdn?.note" class="warn-note">{{ fwd.cdn.note }}</div>
              </template>
              <template v-else-if="fwd.edge && fwd.edge.cdn">
                <el-tag size="small" type="warning">{{ fwd.edge.cdn }}</el-tag>
                <div v-if="fwd.edge.cname" class="muted" style="margin-top:3px">回源 {{ fwd.edge.cname }}</div>
              </template>
              <template v-else-if="fwd.edge && fwd.edge.origin_ip">
                <el-tag size="small" type="info">直连</el-tag>
                <div style="margin-top:3px">源站 {{ fwd.edge.origin_ip }}</div>
              </template>
              <template v-else>
                <span class="muted">未走纳管 CDN</span>
                <!-- 「为什么是空的」比一个「—」有用：不写清楚会被当成采集坏了 -->
                <el-tooltip v-if="fwd.cdn?.reason" :content="fwd.cdn.reason" placement="bottom">
                  <span class="why">为什么？</span>
                </el-tooltip>
              </template>
            </div></div>
            <span class="arrow">→</span>
            <div class="stage"><div class="st-h">域名</div><div class="st-b">
              <b>{{ fwd.domain }}</b>
              <div v-if="fwd.domain_info" class="muted" style="margin-top:3px">{{ fwd.domain_info.project }} · {{ fwd.domain_info.env }} · {{ fwd.domain_info.module || '未关联模块' }}</div>
            </div></div>
            <span class="arrow">→</span>
            <div class="stage"><div class="st-h">证书</div><div class="st-b"><span v-if="fwd.edge && fwd.edge.cert_expiry">到期 {{ String(fwd.edge.cert_expiry).slice(0,10) }}</span><span v-else class="muted">—</span></div></div>
          </div>
          <el-alert v-if="!fwd.matched" type="info" :closable="false" style="margin-top:12px"
            title="没匹配到 K8s 入口" description="该域名在已纳管集群里没有对应 Ingress / HTTPRoute / Istio VirtualService（或该域名未走 K8s 入口）" />
          <div v-for="(ch,i) in fwd.chains" :key="i" class="chain-card">
            <div class="chain-top">
              <el-tag type="warning" size="small">{{ ch.entry_kind }}</el-tag>
              <b style="margin-left:6px">{{ ch.namespace }}/{{ ch.entry_name }}</b>
              <span class="muted" style="margin-left:10px">集群 {{ ch.cluster?.display_name || ch.cluster?.name }} · {{ ch.cluster?.environment }}</span>
              <el-tag v-if="ch.tls_secret" size="small" style="margin-left:8px">TLS: {{ ch.tls_secret }}</el-tag>
            </div>
            <!-- 图形化链路：一个 Service 一行，把「入口→Service→工作负载→Pod→节点」摆成一条，
                 层级关系一眼能看出来。下面的表格保留，用于看完整字段。 -->
            <div class="topo">
              <div v-for="(sv, si) in ch.services" :key="si" class="topo-row">
                <div class="node entry">
                  <div class="n-t">{{ ch.entry_kind }}</div>
                  <div class="n-v">{{ ch.entry_name }}</div>
                </div>
                <i class="link" />
                <div class="node">
                  <div class="n-t">Service</div>
                  <div class="n-v">{{ sv.service }}</div>
                </div>
                <i class="link" />
                <div class="node">
                  <div class="n-t">工作负载</div>
                  <div class="n-v">
                    <span v-if="!sv.workloads?.length" class="bad">无</span>
                    <span v-for="(w, wi) in sv.workloads" :key="wi" class="pill">{{ w }}</span>
                  </div>
                </div>
                <i class="link" />
                <div class="node" :class="{ bad: !sv.pods?.length }">
                  <div class="n-t">Pod</div>
                  <div class="n-v">
                    <span v-if="!sv.pods?.length" class="bad">0 个（入口指向没有实例，访问会 5xx）</span>
                    <el-tooltip v-else placement="bottom">
                      <template #content>
                        <div v-for="(p, pi) in sv.pods" :key="pi">{{ p.pod }} @ {{ p.node }}</div>
                      </template>
                      <span class="pill">{{ sv.pods.length }} 个</span>
                    </el-tooltip>
                  </div>
                </div>
                <i class="link" />
                <div class="node">
                  <div class="n-t">节点</div>
                  <div class="n-v">
                    <el-tooltip v-if="sv.nodes?.length" placement="bottom">
                      <template #content><div v-for="(n, ni) in sv.nodes" :key="ni">{{ n }}</div></template>
                      <span class="pill">{{ sv.nodes.length }} 个</span>
                    </el-tooltip>
                    <span v-else class="muted">—</span>
                  </div>
                </div>
              </div>
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
import { k8sTopology, k8sImpact, listK8sClusters, listK8sNodes, topoDomains } from '../api/cmdb'
import { usePager } from '../composables/usePager'
import Pager from '../components/Pager.vue'

const mode = ref('fwd'); const loading = ref(false)
const domain = ref(''); const fwd = ref(null)
const domainsAll = ref([]); const fProject = ref(''); const fEnv = ref('')
const cdnRecs = computed(() => fwd.value?.cdn?.records || [])
const projects = computed(() => [...new Set(domainsAll.value.map(d => d.project).filter(Boolean))].sort())
const envs = computed(() => {
  const src = fProject.value ? domainsAll.value.filter(d => d.project === fProject.value) : domainsAll.value
  return [...new Set(src.map(d => d.env).filter(Boolean))].sort()
})
const domainOpts = computed(() => domainsAll.value.filter(d =>
  (!fProject.value || d.project === fProject.value) && (!fEnv.value || d.env === fEnv.value)))
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
  try { domainsAll.value = await topoDomains() } catch (e) { domainsAll.value = [] }
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

/* 图形化链路：横向分层 + 连线。不引图表库——这里只要层级清晰，CSS 够用 */
.topo { margin-top: 10px; overflow-x: auto; }
.topo-row { display: flex; align-items: stretch; gap: 0; margin-bottom: 8px; min-width: max-content; }
.topo-row .node {
  min-width: 130px; max-width: 220px; padding: 6px 10px;
  border: 1px solid #dcdfe6; border-radius: 4px; background: #fff;
}
.topo-row .node.entry { background: #fdf6ec; border-color: #f3d19e; }
.topo-row .node.bad { background: #fef0f0; border-color: #fbc4c4; }
.topo-row .n-t { font-size: 11px; color: #909399; margin-bottom: 2px; }
.topo-row .n-v { font-size: 12px; color: #303133; word-break: break-all; line-height: 1.5; }
.topo-row .link {
  align-self: center; width: 22px; height: 1px; background: #c0c4cc; position: relative; flex: none;
}
.topo-row .link::after {
  content: ''; position: absolute; right: 0; top: -3px;
  border-left: 5px solid #c0c4cc; border-top: 3.5px solid transparent; border-bottom: 3.5px solid transparent;
}
.pill { display: inline-block; padding: 0 6px; margin: 1px 2px 1px 0; border-radius: 3px; background: #f0f2f5; font-size: 12px; }
.stage.dim { opacity: .65; }
.why { margin-left: 6px; font-size: 12px; color: #409eff; cursor: help; text-decoration: underline dotted; }
.warn-note { font-size: 11px; color: #e6a23c; margin-top: 3px; line-height: 1.5; }
.bad { color: #f56c6c; }
</style>
