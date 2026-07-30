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
          <!-- 横向节点流拓扑：节点框 + 带状态色的连线 + 分支。
               上一版是纵向清单，读起来是「一行行的字」而不是拓扑；这版每跳一个框、
               框间连线上色（绿=正常/黄=需确认/红=断），分支从入口横向展开。
               链路可能十来跳，容器横向滚动而不是压缩节点。
               视觉沿用 cmdb-domain-quality-topo 那套：浅纸底 + 状态点光晕 + 连线着色。 -->
          <div ref="topoEl" class="topo2">
            <div class="tp-row">
              <template v-for="(n, i) in flowNodes" :key="'n' + i">
                <i v-if="i" class="tp-link" :class="n.link || 'ok'" />
                <div v-if="n.divider" class="tp-divider"><span>集群边界</span></div>
                <div v-else class="tp-node" :class="n.status" :data-entry="n.lab === '入口' || undefined">
                  <div class="tp-lab">{{ n.lab }}</div>
                  <div class="tp-title">
                    <span class="dot" :class="n.status" />{{ n.title }}
                  </div>
                  <div v-for="(s, si) in n.sub || []" :key="si" class="tp-sub" :class="s.cls">
                    {{ s.text }}
                  </div>
                  <el-tooltip v-if="n.why" :content="n.why" placement="bottom">
                    <span class="tp-why">依据 / 例外</span>
                  </el-tooltip>
                </div>
              </template>
            </div>

            <!-- 后端分支：一个 Service 一支，从「入口」节点正下方分出去。
                 marginLeft 不能写死——链路前段（CNAME 跳数、有没有网关）长度不定，
                 「入口」在整行里的实际横坐标会变，写死的偏移量只是碰巧在某些情况下对得上。
                 改成量「入口」节点渲染后的真实 offsetLeft，分支永远从它正下方分出。 -->
            <div v-for="(b, bi) in branches" :key="'b' + bi" class="tp-row branch" :style="{ marginLeft: entryOffset + 'px' }">
              <div class="tp-fork" :class="{ bad: !b.pods }" />
              <div class="tp-node" :class="b.pods ? 'ok' : 'bad'">
                <div class="tp-lab">Service</div>
                <div class="tp-title"><span class="dot" :class="b.pods ? 'ok' : 'bad'" />{{ b.service }}</div>
              </div>
              <i class="tp-link" :class="b.pods ? 'ok' : 'bad'" />
              <div class="tp-node" :class="b.workloads.length ? 'ok' : 'bad'">
                <div class="tp-lab">工作负载</div>
                <div class="tp-title">
                  <span class="dot" :class="b.workloads.length ? 'ok' : 'bad'" />
                  {{ b.workloads.join(', ') || '无' }}
                </div>
              </div>
              <i class="tp-link" :class="b.pods ? 'ok' : 'bad'" />
              <div class="tp-node" :class="b.pods ? 'ok' : 'bad'">
                <div class="tp-lab">Pod</div>
                <div class="tp-title">
                  <span class="dot" :class="b.pods ? 'ok' : 'bad'" />
                  <el-tooltip v-if="b.pods" placement="bottom">
                    <template #content>
                      <div v-for="(p, pi) in b.podList" :key="pi">{{ p.pod }} @ {{ p.node }}</div>
                    </template>
                    <span>{{ b.pods }} 个</span>
                  </el-tooltip>
                  <span v-else>0 个</span>
                </div>
                <div v-if="!b.pods" class="tp-sub bad">入口指向没有实例，访问会 5xx</div>
              </div>
              <i v-if="b.nodes" class="tp-link ok" />
              <div v-if="b.nodes" class="tp-node ok">
                <div class="tp-lab">节点</div>
                <div class="tp-title">
                  <span class="dot ok" />
                  <el-tooltip placement="bottom">
                    <template #content><div v-for="(nd, ni) in b.nodeList" :key="ni">{{ nd }}</div></template>
                    <span>{{ b.nodes }} 个</span>
                  </el-tooltip>
                </div>
              </div>
            </div>
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
import { ref, computed, onMounted, onUnmounted, watch, nextTick } from 'vue'
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
// 把整条链路拍平成节点数组：一处生成，模板只负责画。
// 状态语义固定为三档——ok 通、warn 需人工确认、bad 断——连线颜色跟节点走，
// 这样一眼能看出「链在哪一跳变黄/变红」。
const flowNodes = computed(() => {
  const f = fwd.value
  if (!f) return []
  const out = []
  const di = f.domain_info
  out.push({
    lab: '域名', title: f.domain, status: 'ok',
    sub: [{ text: di
      ? `${di.project || '未关联项目'} · ${di.env || '未标环境'} · ${di.module || '未关联模块'}`
      : '域名台账里没有这条记录（不影响链路查询）' }],
  })

  // CNAME 逐跳。命中 CDN 的那跳单独标出来
  for (const hp of hops.value.slice(1)) {
    const isMatch = hp === trace.value?.match_hop
    out.push({
      lab: 'CNAME', title: hp, status: 'ok', link: 'ok',
      sub: isMatch ? [{ text: trace.value.managed ? '我方纳管的 CDN 站点' : '第三方 CDN 站点' }] : [],
    })
  }

  const tr = trace.value
  const viaCDN = tr?.via_cdn || cdnRecs.value.length
  const cert = f.cdn_cert
  const cdnSub = []
  if (tr?.ips?.length) cdnSub.push({ text: `用户解析到边缘 IP ${tr.ips.slice(0, 3).join(', ')}` })
  if (cert) {
    cdnSub.push({
      text: `边缘证书 ${cert.type} · ${cert.issuer} · ${cert.days_left != null ? cert.days_left + ' 天' : '到期未知'}`,
      cls: cert.severity === 'high' ? 'bad' : cert.severity ? 'warn' : '',
    })
  } else if (viaCDN) {
    cdnSub.push({ text: '未找到覆盖该域名的边缘证书记录', cls: 'warn' })
  }
  out.push({
    lab: 'CDN',
    title: viaCDN ? (tr?.managed ? '我方 Cloudflare 站点' : 'Cloudflare（非纳管账号）') : '未经纳管 CDN',
    status: viaCDN ? 'ok' : 'idle',
    link: viaCDN ? 'ok' : 'dash',
    sub: cdnSub,
    why: tr?.basis || f.cdn?.reason,
  })

  if (originIPs.value.length) {
    out.push({
      lab: '回源', title: originIPs.value.join(', '), status: 'ok', link: 'ok',
      sub: [{ text: 'CDN 往源站打的地址——与上面的边缘 IP 是两回事' }],
    })
  }

  // 网关与入口只画第一条 chain 的（多 chain 极少见，明细表里能看全）
  const ch = (f.chains || [])[0]
  if (ch) {
    // 集群边界：域名/CDN/回源在集群外，网关(Gateway)/入口(VirtualService)虽然
    // 网络路径上离外部最近，但本身已经是集群内的 Istio CRD——不画出这条界线，
    // 两段很容易被看成是同一层，或者被以为「网关是集群外的东西」。
    out.push({ divider: true })
    for (const gw of ch.gateways || []) {
      const sub = []
      if (gw.lb_ip) sub.push({ text: `${gw.lb_scope} ${gw.lb_ip}` })
      if (gw.listeners) sub.push({ text: gw.listeners })
      for (const ct of gw.tls || []) {
        const st = ct.exists === false ? 'bad' : ct.exists === true ? '' : 'warn'
        sub.push({
          text: `证书 Secret ${ct.secret}：` +
            (ct.exists === true ? '存在' : ct.exists === false ? '不存在' : '存在性未知'),
          cls: st,
        })
      }
      const bad = gw.missing || (gw.tls || []).some((c) => c.exists === false)
      // cdn_origin_match 只在链路里真有 CDN 回源地址时才有值。不走 CDN 的域名压根
      // 没有回源可比，undefined 不等于「对不上」——以前一律判 warn，等于给每个
      // 直连域名挂一个既没依据也没说明的橙灯。
      out.push({
        lab: '网关', title: gw.ref,
        status: bad ? 'bad' : gw.cdn_origin_match === false ? 'warn' : 'ok',
        link: bad ? 'bad' : 'ok',
        sub: gw.missing ? [{ text: gw.missing, cls: 'bad' }] : sub,
        why: gw.cdn_origin_note || gw.service_missing,
      })
    }
    out.push({
      lab: '入口', title: `${ch.entry_kind} ${ch.namespace}/${ch.entry_name}`, status: 'ok', link: 'ok',
      sub: [{ text: `集群 ${ch.cluster?.display_name || ch.cluster?.name} · ${ch.cluster?.environment}` }],
    })
  }
  return out
})

// 后端分支：一个 Service 一支
const branches = computed(() => {
  const ch = (fwd.value?.chains || [])[0]
  if (!ch) return []
  return (ch.services || []).map((sv) => ({
    service: sv.service,
    workloads: sv.workloads || [],
    pods: (sv.pods || []).length,
    podList: sv.pods || [],
    nodes: (sv.nodes || []).length,
    nodeList: sv.nodes || [],
  }))
})

// 「入口」节点的真实横坐标，供下面的 Service 分支行对齐——见上面模板里的注释。
// 每次重算都从容器里现查 DOM，而不是缓存节点的 ref：换域名重查时整行节点会被
// 重建，缓存下来的那个已经脱离文档，offsetLeft 读出来是 0，分支就跑到最左边去了。
const topoEl = ref(null)
const entryOffset = ref(0)
async function recalcEntryOffset() {
  await nextTick()
  const el = topoEl.value?.querySelector('[data-entry]')
  entryOffset.value = el ? el.offsetLeft : 0
}
watch(flowNodes, recalcEntryOffset, { flush: 'post' })
onMounted(() => window.addEventListener('resize', recalcEntryOffset))
onUnmounted(() => window.removeEventListener('resize', recalcEntryOffset))

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

/* 横向节点流拓扑。视觉沿用 cmdb-domain-quality-topo：状态点带光晕、连线按状态着色。
   节点不压缩、容器横向滚动——链路十来跳时压缩会让每格都读不清。 */
/* padding-top 要容下「集群边界」那个上浮的标签(top:-18px)，否则它会被 overflow 裁掉 */
.topo2 { overflow-x: auto; padding: 22px 2px 2px; position: relative; }
.tp-row { display: flex; align-items: stretch; min-width: max-content; margin-bottom: 10px; }
/* Service 分支行的 margin-left 由 JS 量「入口」节点的 offsetLeft 动态给出，见组件脚本 */

/* 集群边界：域名/CDN/回源在这条线左边（集群外），网关/入口/Service/…在右边（K8s 集群内）。
   之前两段挤在一起看不出物理边界，容易把网关当成"集群外"的东西。 */
.tp-divider {
  align-self: stretch; flex: none; width: 0; margin: 0 16px; position: relative;
  border-left: 2px dashed #c3c7cf;
}
.tp-divider span {
  position: absolute; top: -18px; left: 50%; transform: translateX(-50%);
  font-size: 10.5px; color: #8a909a; white-space: nowrap; background: #fff; padding: 0 4px;
}
.tp-node {
  min-width: 150px; max-width: 260px; background: #fff;
  border: 1px solid #e7e9e2; border-left: 3px solid #1f9d63; border-radius: 8px;
  padding: 7px 11px;
}
.tp-node.warn { border-left-color: #c98a12; background: #fefbf4; }
.tp-node.bad { border-left-color: #d1483a; background: #fdf5f4; }
.tp-node.idle { border-left-color: #8a909a; background: #fafaf8; }
.tp-lab { font-size: 10.5px; color: #8a909a; letter-spacing: .3px; margin-bottom: 2px; }
.tp-title {
  font-size: 13px; font-weight: 600; color: #14161a; display: flex; align-items: center; gap: 6px;
  word-break: break-all;
}
.tp-title .dot {
  width: 8px; height: 8px; border-radius: 50%; flex: 0 0 8px; background: #1f9d63;
  box-shadow: 0 0 0 3px #e4f3ea;
}
.tp-title .dot.warn { background: #c98a12; box-shadow: 0 0 0 3px #faf0da; }
.tp-title .dot.bad { background: #d1483a; box-shadow: 0 0 0 3px #fbe7e4; }
.tp-title .dot.idle { background: #8a909a; box-shadow: 0 0 0 3px #e7e9e2; }
.tp-sub { font-size: 11px; color: #565b64; line-height: 1.6; margin-top: 2px; word-break: break-all; }
.tp-sub.warn { color: #c98a12; }
.tp-sub.bad { color: #d1483a; font-weight: 600; }
.tp-why {
  display: inline-block; margin-top: 3px; font-size: 11px; color: #3538cd;
  cursor: help; text-decoration: underline dotted;
}
/* 连线：横线 + 箭头，颜色跟随两端状态 */
.tp-link { align-self: center; width: 26px; height: 3px; background: #1f9d63; position: relative; flex: none; }
.tp-link.warn { background: #c98a12; }
.tp-link.bad { background: #d1483a; }
.tp-link.dash {
  height: 2px;
  background: repeating-linear-gradient(90deg, #8a909a 0 5px, transparent 5px 10px);
}
.tp-link::after {
  content: ''; position: absolute; right: 0; top: -3px;
  border-left: 6px solid currentColor; border-top: 4.5px solid transparent; border-bottom: 4.5px solid transparent;
  color: inherit;
}
.tp-link { color: #1f9d63; }
.tp-link.warn { color: #c98a12; }
.tp-link.bad { color: #d1483a; }
.tp-link.dash { color: #8a909a; }
/* 分支从上一行底部拐出来 */
.tp-fork {
  align-self: center; width: 26px; height: 3px; background: #1f9d63; position: relative; flex: none;
  color: #1f9d63;
}
.tp-fork.bad { background: #d1483a; color: #d1483a; }
.tp-fork::before {
  content: ''; position: absolute; left: 0; bottom: 0; width: 3px; height: 22px;
  background: currentColor; transform: translateY(-100%);
}
.tp-fork::after {
  content: ''; position: absolute; right: 0; top: -3px;
  border-left: 6px solid currentColor; border-top: 4.5px solid transparent; border-bottom: 4.5px solid transparent;
}
</style>
