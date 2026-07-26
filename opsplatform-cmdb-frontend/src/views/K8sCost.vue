<template>
  <div class="page">
    <div class="page-head">
      <span class="page-title">云成本</span>
      <span class="muted" style="margin-left:10px">估算口径（机型费率 × 请求分摊）· cloud=真实支出 / idc=迁云估算 / 本地不计费 · USD/月</span>
    </div>

    <el-tabs v-model="tab">
      <!-- 总览 -->
      <el-tab-pane label="总览" name="ov">
        <div class="kpis">
          <div class="kpi"><div class="lab">云支出（真实/月）</div><div class="num">${{ ov.cloud_total || 0 }}</div></div>
          <div class="kpi idc"><div class="lab">IDC 迁云估算/月</div><div class="num">${{ ov.idc_estimate || 0 }}</div></div>
          <div class="kpi"><div class="lab">K8s 计算</div><div class="num sm">${{ ov.by_type?.k8s_compute || 0 }}</div></div>
          <div class="kpi"><div class="lab">K8s 存储</div><div class="num sm">${{ ov.by_type?.k8s_storage || 0 }}</div></div>
          <div class="kpi"><div class="lab">传统主机</div><div class="num sm">${{ ov.by_type?.traditional || 0 }}</div></div>
        </div>
        <el-card shadow="never">
          <template #header>
            <div class="cardhd">
              <span>维度：</span>
              <el-radio-group v-model="dim" size="small" @change="loadOv">
                <el-radio-button value="biz_project">业务项目</el-radio-button>
                <el-radio-button value="gcp_project">GCP 项目</el-radio-button>
                <el-radio-button value="cluster">集群</el-radio-button>
                <el-radio-button value="env">环境</el-radio-button>
                <el-radio-button value="type">类型</el-radio-button>
              </el-radio-group>
              <el-radio-group v-model="ovMode" size="small" style="margin-left:16px" @change="loadOv">
                <el-radio-button value="cloud">真实</el-radio-button>
                <el-radio-button value="idc">迁云估算</el-radio-button>
                <el-radio-button value="">全部</el-radio-button>
              </el-radio-group>
              <b style="margin-left:auto">合计 ${{ ov.group_total || 0 }}</b>
            </div>
          </template>
          <div v-for="g in ov.groups" :key="g.name" class="brow">
            <span class="bl">{{ g.name }}</span>
            <div class="tk"><div class="fl" :style="{width: pct(g.cost)+'%'}"></div></div>
            <b class="bn">${{ g.cost }}</b>
          </div>
          <el-empty v-if="!ov.groups?.length" description="无数据（费率未配或本地集群不计费）" />
        </el-card>
        <div class="muted" style="margin-top:8px">{{ ov.note }}</div>
      </el-tab-pane>

      <!-- 明细 -->
      <el-tab-pane label="明细" name="detail">
        <div class="bar">
          <el-select v-model="f.biz_project" clearable placeholder="业务项目" style="width:160px" @change="loadDetail">
            <el-option v-for="p in projOpts" :key="p" :label="p" :value="p" />
          </el-select>
          <el-select v-model="f.gcp_project" clearable placeholder="GCP 项目" style="width:180px" @change="loadDetail">
            <el-option v-for="p in gcpOpts" :key="p" :label="p" :value="p" />
          </el-select>
          <el-select v-model="f.env" clearable placeholder="环境" style="width:120px" @change="loadDetail">
            <el-option v-for="e in envOpts" :key="e" :label="e" :value="e" />
          </el-select>
          <el-select v-model="f.mode" clearable placeholder="计费模式" style="width:140px" @change="loadDetail">
            <el-option label="真实(cloud)" value="cloud" /><el-option label="迁云估算(idc)" value="idc" />
          </el-select>
          <b style="margin-left:auto">合计 ${{ det.total || 0 }} · {{ det.count || 0 }} 项</b>
        </div>
        <el-table :data="detPaged" size="small" max-height="520">
          <el-table-column prop="type" label="类型" width="110"><template #default="{row}">{{ typeText(row.type) }}</template></el-table-column>
          <el-table-column prop="cluster" label="集群" width="130" />
          <el-table-column prop="env" label="环境" width="80" />
          <el-table-column prop="gcp_project" label="GCP项目" width="140" />
          <el-table-column prop="biz_project" label="业务项目" width="120" />
          <el-table-column prop="namespace" label="命名空间" width="120" />
          <el-table-column prop="name" label="资源" min-width="180" />
          <el-table-column label="模式" width="90"><template #default="{row}">
            <el-tag size="small" :type="row.mode==='cloud'?'success':'warning'">{{ row.mode==='cloud'?'真实':'迁云估' }}</el-tag>
          </template></el-table-column>
          <el-table-column label="月费(估)" width="100"><template #default="{row}">${{ row.cost }}</template></el-table-column>
        </el-table>
        <Pager :total="detItems.length" v-model:page="detPage" v-model:page-size="detSize" />
      </el-tab-pane>

      <!-- 报告 -->
      <el-tab-pane label="报告/环比" name="report">
        <div class="bar">
          <el-radio-group v-model="rp.period" size="small" @change="loadReport">
            <el-radio-button value="month">月度</el-radio-button>
            <el-radio-button value="quarter">季度</el-radio-button>
            <el-radio-button value="year">年度</el-radio-button>
          </el-radio-group>
          <el-select v-model="rp.anchor" placeholder="选月份" style="width:130px" @change="loadReport">
            <el-option v-for="m in months" :key="m" :label="m" :value="m" />
          </el-select>
          <el-button type="primary" size="small" :icon="Camera" @click="snapshot">立即打本月快照</el-button>
          <span class="muted" style="margin-left:auto">每 6h 自动刷新当月快照，跨月自动定格上月</span>
        </div>
        <div class="kpis" style="grid-template-columns:repeat(3,1fr)">
          <div class="kpi"><div class="lab">周期总费用</div><div class="num">${{ rep.total || 0 }}</div></div>
          <div class="kpi"><div class="lab">上一周期</div><div class="num sm">${{ rep.prev_total || 0 }}</div></div>
          <div class="kpi" :class="{up:(rep.delta||0)>0}"><div class="lab">环比</div>
            <div class="num sm" :style="{color:(rep.delta||0)>0?'#f56c6c':'#1f9d63'}">{{ (rep.delta||0)>0?'▲ +':'▼ ' }}${{ Math.abs(rep.delta||0) }}</div>
          </div>
        </div>
        <el-card shadow="never" style="margin-bottom:14px">
          <template #header><b>近 12 月趋势</b></template>
          <div class="trend">
            <div v-for="t in rep.trend" :key="t.month" class="tcol">
              <div class="tbar" :style="{height: tHeight(t.cost)+'px'}" :title="'$'+t.cost"></div>
              <div class="tm">{{ t.month.slice(5) }}</div>
            </div>
          </div>
        </el-card>
        <el-card shadow="never">
          <template #header><b>环比归因（本月 vs 上月，按变化额排序）</b><span class="muted" style="margin-left:8px">哪些资源导致费用变化</span></template>
          <el-table :data="attr.movers" size="small" max-height="380">
            <el-table-column prop="resource" label="资源" min-width="240" />
            <el-table-column prop="type" label="类型" width="110"><template #default="{row}">{{ typeText(row.type) }}</template></el-table-column>
            <el-table-column prop="cluster" label="集群" width="130" />
            <el-table-column label="上月→本月" width="150"><template #default="{row}">${{ row.old }} → ${{ row.new }}</template></el-table-column>
            <el-table-column label="变化" width="110"><template #default="{row}">
              <b :style="{color: row.delta>0?'#f56c6c':'#1f9d63'}">{{ row.delta>0?'+':'' }}${{ row.delta }}</b>
            </template></el-table-column>
            <el-table-column prop="reason" label="原因" min-width="200" />
          </el-table>
          <el-empty v-if="!attr.movers?.length" description="无变化（需至少两个月快照）" />
        </el-card>
      </el-tab-pane>

      <!-- 节点成本 -->
      <el-tab-pane label="节点成本" name="nodes">
        <div class="muted" style="margin-bottom:10px">GKE 走机型费率；IDC 标"迁云估算"；本地不计费。无机型/IDC 可手填月成本</div>
        <el-table :data="nodes" size="small">
          <el-table-column prop="cluster" label="集群" width="150" />
          <el-table-column prop="name" label="节点" min-width="200" />
          <el-table-column label="模式" width="90"><template #default="{row}">
            <el-tag size="small" :type="row.mode==='cloud'?'success':(row.mode==='idc'?'warning':'info')">{{ row.mode }}</el-tag>
          </template></el-table-column>
          <el-table-column label="月成本(估)" width="110"><template #default="{row}">${{ row.monthly }}</template></el-table-column>
          <el-table-column prop="source" label="来源" min-width="160" />
          <el-table-column label="手动覆盖" width="200"><template #default="{row}">
            <el-input v-model.number="row._ov" size="small" style="width:100px" placeholder="月$" />
            <el-button link type="primary" size="small" @click="saveOv(row)">保存</el-button>
          </template></el-table-column>
        </el-table>
      </el-tab-pane>
    </el-tabs>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Camera } from '@element-plus/icons-vue'
import { costOverview, costDetail, costNodes, setNodeCostOverride, costSnapshot, costMonths, costReport, costAttribution } from '../api/cmdb'
import { usePager } from '../composables/usePager'
import Pager from '../components/Pager.vue'

const tab = ref('ov')
const ov = ref({}); const det = ref({}); const nodes = ref([])
const detItems = computed(() => det.value.items || [])
const { page: detPage, pageSize: detSize, paged: detPaged } = usePager(detItems)
const dim = ref('biz_project'); const ovMode = ref('cloud')
const f = ref({ biz_project: '', gcp_project: '', env: '', mode: '' })
const months = ref([]); const rp = ref({ period: 'month', anchor: '' }); const rep = ref({}); const attr = ref({})

function tHeight(c) { const max = Math.max(...(rep.value.trend || []).map(x => x.cost), 1); return Math.round(c / max * 90) + 2 }

async function loadReport() {
  try {
    rep.value = await costReport({ period: rp.value.period, anchor: rp.value.anchor, dim: 'type' })
    if (rp.value.period === 'month') attr.value = await costAttribution(rp.value.anchor)
    else attr.value = { movers: [] }
  } catch (e) { ElMessage.error('加载失败') }
}
async function snapshot() {
  try { const r = await costSnapshot(); ElMessage.success(`已打快照 ${r.month}（${r.rows} 项）`); await loadMonths(); loadReport() }
  catch (e) { ElMessage.error('打快照失败') }
}
async function loadMonths() {
  months.value = await costMonths()
  if (!rp.value.anchor && months.value.length) rp.value.anchor = months.value[0]
}

function typeText(t) { return { k8s_compute: 'K8s计算', k8s_storage: 'K8s存储', traditional: '传统主机' }[t] || t }
function pct(c) { const max = Math.max(...(ov.value.groups || []).map(x => x.cost), 1); return Math.round(c / max * 100) }

const projOpts = computed(() => [...new Set((det.value.items || []).map(i => i.biz_project))])
const gcpOpts = computed(() => [...new Set((det.value.items || []).map(i => i.gcp_project))])
const envOpts = computed(() => [...new Set((det.value.items || []).map(i => i.env))])

async function loadOv() { try { ov.value = await costOverview({ dim: dim.value, mode: ovMode.value }) } catch (e) { ElMessage.error('加载失败') } }
async function loadDetail() { try { det.value = await costDetail({ ...f.value }) } catch (e) { ElMessage.error('加载失败') } }
async function loadNodes() { try { nodes.value = (await costNodes()).map(n => ({ ...n, _ov: '' })) } catch (e) { ElMessage.error('加载失败') } }

async function saveOv(row) {
  try { await setNodeCostOverride({ cluster_id: row.cluster_id, name: row.name, monthly: Number(row._ov) || 0 }); ElMessage.success('已保存'); loadNodes() }
  catch (e) { ElMessage.error('保存失败') }
}

onMounted(async () => { loadOv(); loadDetail(); loadNodes(); await loadMonths(); loadReport() })
</script>

<style scoped>
.page-head { margin-bottom: 14px; }
.page-title { font-size: 18px; font-weight: 600; }
.muted { color: #909399; font-size: 12px; }
.bar { display: flex; gap: 10px; align-items: center; margin-bottom: 12px; }
.cardhd { display: flex; align-items: center; gap: 8px; }
.kpis { display: grid; grid-template-columns: repeat(5,1fr); gap: 12px; margin-bottom: 16px; }
.kpi { background: #fff; border: 1px solid #e7e9e2; border-radius: 10px; padding: 14px 16px; }
.kpi.idc { border-color: #f0d9a8; background: #fffdf5; }
.kpi .lab { font-size: 12px; color: #606266; }
.kpi .num { font-size: 24px; font-weight: 600; margin-top: 4px; font-family: 'Fira Code', monospace; color: #1e40af; }
.kpi .num.sm { font-size: 18px; color: #303133; }
.brow { display: flex; align-items: center; gap: 10px; margin: 8px 0; font-size: 13px; }
.bl { width: 170px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.tk { flex: 1; height: 10px; background: #f0f2f5; border-radius: 5px; overflow: hidden; }
.fl { height: 100%; background: #3b82f6; border-radius: 5px; }
.bn { width: 90px; text-align: right; font-family: 'Fira Code', monospace; }
.trend { display: flex; align-items: flex-end; gap: 8px; height: 110px; }
.tcol { flex: 1; display: flex; flex-direction: column; align-items: center; justify-content: flex-end; }
.tbar { width: 60%; background: #3b82f6; border-radius: 3px 3px 0 0; min-height: 2px; }
.tm { font-size: 10px; color: #909399; margin-top: 4px; font-family: 'Fira Code', monospace; }
</style>
