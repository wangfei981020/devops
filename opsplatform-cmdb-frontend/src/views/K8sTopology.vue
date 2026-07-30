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
          <!-- 完整拓扑：从域名起，把 CNAME 链 → CDN → 回源 → 网关 → 入口 → Service
               → 工作负载 → Pod → 节点 串成一条竖着走的链。
               以前是「CDN→域名→证书」一行 + 「入口→…→节点」另一行，两段断开、顺序还反了
               （域名才是起点）。竖排是因为这条链有十来跳，横排既放不下也读不顺。 -->
          <div class="flow">
            <div class="fl-step">
              <div class="fl-lab">域名</div>
              <div class="fl-box origin">
                <b>{{ fwd.domain }}</b>
                <div v-if="fwd.domain_info" class="muted">
                  {{ fwd.domain_info.project || '未关联项目' }} ·
                  {{ fwd.domain_info.env || '未标环境' }} ·
                  {{ fwd.domain_info.module || '未关联模块' }}
                </div>
                <div v-else class="muted">域名台账里没有这条记录（不影响链路查询）</div>
              </div>
            </div>

            <div v-for="(hp, i) in hops.slice(1)" :key="'h' + i" class="fl-step">
              <div class="fl-lab">CNAME</div>
              <div class="fl-box" :class="{ cdn: hp === trace?.match_hop }">
                {{ hp }}
                <el-tag v-if="hp === trace?.match_hop" size="small" type="warning" style="margin-left:6px">
                  {{ trace.managed ? 'CDN 站点' : 'CDN' }}
                </el-tag>
              </div>
            </div>

            <div class="fl-step">
              <div class="fl-lab">CDN</div>
              <div v-if="trace?.via_cdn || cdnRecs.length" class="fl-box cdn">
                <b>{{ trace?.managed ? '我方 Cloudflare 站点' : 'Cloudflare（非纳管账号）' }}</b>
                <div v-if="trace?.ips?.length" class="muted">
                  用户解析到边缘 IP {{ trace.ips.slice(0, 3).join(', ') }}
                </div>
                <div class="basis">{{ trace?.basis || fwd.cdn?.reason }}</div>
                <!-- 边缘证书与源站证书是两张，用户先撞到的是这张 -->
                <div v-if="fwd.cdn_cert" class="cert-line">
                  证书 {{ fwd.cdn_cert.type }} · {{ fwd.cdn_cert.issuer }} ·
                  <el-tag size="small" :type="certTagType(fwd.cdn_cert.days_left)">
                    {{ fwd.cdn_cert.days_left != null ? fwd.cdn_cert.days_left + ' 天' : '到期未知' }}
                  </el-tag>
                  <span class="muted">{{ fwd.cdn_cert.hosts }}</span>
                  <div v-if="fwd.cdn_cert.issue" class="basis bad">{{ fwd.cdn_cert.issue }}</div>
                </div>
                <div v-else class="basis">CDN 上没找到覆盖该域名的边缘证书记录（可能 token 缺 SSL and Certificates·Read）</div>
              </div>
              <div v-else class="fl-box dim">
                未经纳管 CDN
                <div class="basis">{{ trace?.basis || fwd.cdn?.reason || '无 CDN 数据' }}</div>
              </div>
            </div>

            <div class="fl-step" v-if="originIPs.length">
              <div class="fl-lab">回源</div>
              <div class="fl-box">
                源站 <b>{{ originIPs.join(', ') }}</b>
                <div class="muted">CDN 在这里配的回源地址——与上面的边缘 IP 是两回事</div>
              </div>
            </div>

            <template v-for="(ch, ci) in fwd.chains" :key="'c' + ci">
              <div v-for="(gw, gi) in (ch.gateways || [])" :key="'g' + ci + '-' + gi" class="fl-step">
                <div class="fl-lab">网关</div>
                <div class="fl-box" :class="{ warn: gw.missing, ok: gw.cdn_origin_match }">
                  <b>{{ gw.ref }}</b>
                  <el-tag v-if="gw.lb_ip" size="small" :type="gw.lb_scope === '内网' ? 'info' : 'danger'"
                    style="margin-left:6px">{{ gw.lb_scope }} {{ gw.lb_ip }}</el-tag>
                  <el-tag v-if="gw.cdn_origin_match" size="small" type="success" style="margin-left:6px">
                    回源已对上
                  </el-tag>
                  <div v-if="gw.listeners" class="muted">{{ gw.listeners }}</div>
                  <!-- 源站侧证书：Gateway 引用的 TLS Secret。存在性判定取决于该集群
                       有没有开 Secret 名录，未知与不存在必须分开显示。 -->
                  <div v-for="(ct, ti) in (gw.tls || [])" :key="'t' + ti" class="cert-line">
                    证书 Secret <b>{{ ct.secret }}</b>
                    <el-tag v-if="ct.exists === true" size="small" type="success">存在</el-tag>
                    <el-tag v-else-if="ct.exists === false" size="small" type="danger">不存在</el-tag>
                    <el-tag v-else size="small" type="info">存在性未知</el-tag>
                    <div v-if="ct.note" class="basis" :class="{ bad: ct.exists === false }">{{ ct.note }}</div>
                  </div>
                  <div v-if="gw.tls_note" class="basis">{{ gw.tls_note }}</div>
                  <div v-if="gw.missing" class="basis bad">{{ gw.missing }}</div>
                  <div v-else-if="gw.cdn_origin_note" class="basis">{{ gw.cdn_origin_note }}</div>
                  <div v-if="gw.service_missing" class="basis bad">{{ gw.service_missing }}</div>
                </div>
              </div>

              <div class="fl-step">
                <div class="fl-lab">入口</div>
                <div class="fl-box entry">
                  <el-tag size="small" type="warning">{{ ch.entry_kind }}</el-tag>
                  <b style="margin-left:6px">{{ ch.namespace }}/{{ ch.entry_name }}</b>
                  <el-tag v-if="ch.tls_secret" size="small" style="margin-left:6px">TLS: {{ ch.tls_secret }}</el-tag>
                  <div class="muted">
                    集群 {{ ch.cluster?.display_name || ch.cluster?.name }} · {{ ch.cluster?.environment }}
                  </div>
                </div>
              </div>

              <div v-for="(sv, si) in ch.services" :key="'s' + ci + '-' + si" class="fl-step">
                <div class="fl-lab">后端</div>
                <div class="fl-box" :class="{ bad: !sv.pods?.length }">
                  <div class="svc-line">
                    <span class="pill">Service {{ sv.service }}</span>
                    <i class="mini-arrow" />
                    <span v-if="!sv.workloads?.length" class="bad">无工作负载</span>
                    <span v-for="(w, wi) in sv.workloads" :key="wi" class="pill">{{ w }}</span>
                    <i class="mini-arrow" />
                    <template v-if="sv.pods?.length">
                      <el-tooltip placement="right">
                        <template #content>
                          <div v-for="(pd, pi) in sv.pods" :key="pi">{{ pd.pod }} @ {{ pd.node }}</div>
                        </template>
                        <span class="pill">{{ sv.pods.length }} Pod</span>
                      </el-tooltip>
                      <i class="mini-arrow" />
                      <el-tooltip placement="right">
                        <template #content><div v-for="(nd, ni) in sv.nodes" :key="ni">{{ nd }}</div></template>
                        <span class="pill">{{ sv.nodes.length }} 节点</span>
                      </el-tooltip>
                    </template>
                    <span v-else class="bad">0 Pod —— 入口指向没有实例，访问会 5xx</span>
                  </div>
                </div>
              </div>
            </template>
          </div>

          <el-alert v-if="!fwd.matched" type="info" :closable="false" style="margin-top:12px"
            title="没匹配到 K8s 入口"
            description="该域名在已纳管集群里没有对应 Ingress / HTTPRoute / Istio VirtualService（或该域名未走 K8s 入口）" />

          <!-- 拓扑图看关系，明细表看完整字段，两者都留 -->
          <el-collapse v-if="fwd.matched" style="margin-top:12px">
            <el-collapse-item title="后端明细（完整字段）" name="d">
              <div v-for="(ch, i) in fwd.chains" :key="i" style="margin-bottom:10px">
                <div class="muted">{{ ch.entry_kind }} · {{ ch.namespace }}/{{ ch.entry_name }}</div>
                <el-table :data="ch.services" size="small">
                  <el-table-column prop="service" label="Service" min-width="180" />
                  <el-table-column label="工作负载" min-width="180"><template #default="{ row }">
                    {{ (row.workloads || []).join(', ') || '—' }}
                  </template></el-table-column>
                  <el-table-column label="Pod" min-width="240"><template #default="{ row }">
                    <div v-for="(pd, pi) in row.pods" :key="pi" class="muted">{{ pd.pod }}</div>
                    <span v-if="!row.pods?.length" class="bad">无</span>
                  </template></el-table-column>
                  <el-table-column label="节点" min-width="240"><template #default="{ row }">
                    <div v-for="(nd, ni) in row.nodes" :key="ni" class="muted">{{ nd }}</div>
                  </template></el-table-column>
                </el-table>
              </div>
            </el-collapse-item>
          </el-collapse>
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
const trace = computed(() => fwd.value?.cdn?.trace || null)
const hops = computed(() => trace.value?.hops || [])
// 回源地址与边缘 IP 必须分开：前者是 CDN 往源站打的，后者是用户解析到的 CDN 节点。
// 混在一起看会得出「回源打到 Cloudflare 自己」这种荒谬结论。
const originIPs = computed(() => {
  const fromTrace = trace.value?.origin_ips || []
  const fromRecs = cdnRecs.value.filter((r) => r.type === 'A' || r.type === 'AAAA').map((r) => r.content)
  return [...new Set([...fromTrace, ...fromRecs])]
})
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
function certTagType(d) {
  if (d == null) return 'info'
  return d < 0 ? 'danger' : d <= 14 ? 'warning' : d <= 30 ? 'warning' : 'success'
}
</script>

<style scoped>
.page-head { margin-bottom: 14px; }
.page-title { font-size: 18px; font-weight: 600; }
.muted { color: #909399; font-size: 12px; }
.bar { display: flex; gap: 10px; align-items: center; margin-bottom: 14px; }
.mini-t { font-size: 13px; font-weight: 600; }

/* 图形化链路：横向分层 + 连线。不引图表库——这里只要层级清晰，CSS 够用 */
.pill { display: inline-block; padding: 0 6px; margin: 1px 2px 1px 0; border-radius: 3px; background: #f0f2f5; font-size: 12px; }
.hops { word-break: break-all; line-height: 1.5; margin-top: 3px; }
.why { margin-left: 6px; font-size: 12px; color: #409eff; cursor: help; text-decoration: underline dotted; }
.warn-note { font-size: 11px; color: #e6a23c; margin-top: 3px; line-height: 1.5; }
.bad { color: #f56c6c; }

/* 纵向拓扑：每跳一格，左侧层名右侧内容，格与格之间用竖线+箭头连起来 */
.flow { padding: 4px 0; }
.fl-step { display: flex; align-items: stretch; position: relative; padding-bottom: 18px; }
.fl-step:last-child { padding-bottom: 0; }
/* 竖线画在层名列上，最后一格不画 */
.fl-step:not(:last-child)::before {
  content: ''; position: absolute; left: 47px; top: 26px; bottom: 0; width: 1px; background: #dcdfe6;
}
.fl-step:not(:last-child)::after {
  content: ''; position: absolute; left: 44px; bottom: 2px;
  border-top: 5px solid #c0c4cc; border-left: 4px solid transparent; border-right: 4px solid transparent;
}
.fl-lab {
  flex: none; width: 56px; font-size: 11px; color: #909399; text-align: right;
  padding-right: 14px; padding-top: 6px;
}
.fl-box {
  flex: 1; min-width: 0; border: 1px solid #dcdfe6; border-radius: 4px;
  padding: 6px 10px; background: #fff; font-size: 13px; word-break: break-all;
}
.fl-box.origin { background: #ecf5ff; border-color: #b3d8ff; }
.fl-box.cdn { background: #fdf6ec; border-color: #f3d19e; }
.fl-box.entry { background: #fdf6ec; border-color: #f3d19e; }
.fl-box.ok { border-color: #b3e19d; }
.fl-box.warn { background: #fdf6ec; border-color: #e6a23c; }
.fl-box.bad { background: #fef0f0; border-color: #fbc4c4; }
.fl-box.dim { background: #f4f4f5; color: #909399; }
.fl-box .basis { font-size: 11px; color: #909399; margin-top: 3px; line-height: 1.6; }
.fl-box .basis.bad { color: #f56c6c; }
.cert-line { font-size: 12px; margin-top: 4px; line-height: 1.7; }
.svc-line { display: flex; align-items: center; flex-wrap: wrap; gap: 2px; }
.mini-arrow {
  display: inline-block; width: 16px; height: 1px; background: #c0c4cc; position: relative; margin: 0 2px;
}
.mini-arrow::after {
  content: ''; position: absolute; right: 0; top: -3px;
  border-left: 4px solid #c0c4cc; border-top: 3px solid transparent; border-bottom: 3px solid transparent;
}
</style>
