<template>
  <div class="page">
    <div class="page-head"><span class="page-title">总览</span></div>
    <LoadError :error="error" @retry="load" />
    <!-- 分组显示的原因见 CMDB-014：把「已登记证书 6」和「线上快到期 158」并排放，
         两个数看起来自相矛盾（158 > 6），实际它们统计的根本不是同一批对象。
         台账 = CMDB 里登记管理的；线上 = 对线上域名实测出来的。 -->
    <div v-loading="loading">
      <div v-for="g in cardGroups" :key="g.title" class="cgroup">
        <div class="cg-head">
          <span class="cg-title">{{ g.title }}</span>
          <span class="cg-tip">{{ g.tip }}</span>
        </div>
        <el-row :gutter="14">
          <el-col :span="6" v-for="c in g.cards" :key="c.label" style="margin-bottom:14px">
            <el-card shadow="never" :class="{clickable: c.to}" @click="c.to && $router.push(c.to)">
              <!-- 失败时显示 —，不显示 0：0 是"确实没有"的断言（CMDB-013） -->
              <div class="stat"><div class="num" :style="{color: error ? '#c0c4cc' : c.color}">{{ num(c.num) }}</div><div class="lbl">{{ c.label }}</div></div>
            </el-card>
          </el-col>
        </el-row>
      </div>
    </div>

    <el-card shadow="never" style="margin-top:14px" v-if="!error && (projStats.length || domStats.length)">
      <template #header><span style="font-weight:600">生命周期概览</span></template>
      <div class="stat-row">
        <span class="stat-title">项目：</span>
        <!-- 带上筛选条件跳转。原来点哪个标签都跳到裸 /domains，
             用户得在目标页重新找一遍刚才点的是什么——"跳过去了但没带着我点的东西"
             比不能点更让人困惑（CMDB-20260805-012）。 -->
        <el-tag v-for="s in projStats" :key="s.label" size="small" effect="plain" :style="chip(s.color)" style="margin-right:8px" class="clk"
                @click="$router.push({ path: '/domains', query: { pstatus: s.label } })">{{ s.label }} {{ s.n }}</el-tag>
        <el-tag v-if="projNone" size="small" type="info" style="margin-right:8px">未设置 {{ projNone }}</el-tag>
      </div>
      <div class="stat-row" style="margin-top:8px">
        <span class="stat-title">主域名：</span>
        <el-tag v-for="s in domStats" :key="s.label" size="small" effect="plain" :style="chip(s.color)" style="margin-right:8px" class="clk" @click="$router.push('/domains')">{{ s.label }} {{ s.n }}</el-tag>
        <el-tag v-if="domNone" size="small" type="info" style="margin-right:8px">未设置 {{ domNone }}</el-tag>
      </div>
    </el-card>

    <!-- 已过期单独成表：混在「30 天内到期」里既排不了序也提不起注意（CMDB-016） -->
    <el-card shadow="never" style="margin-top:14px" v-if="error || expired.length">
      <template #header><span style="font-weight:600; color:#f56c6c">🔴 已过期（需立即处理）</span></template>
      <el-table :data="expired" size="small">
        <el-table-column label="类型" width="90">
          <template #default="{ row }"><el-tag size="small" :type="row.type==='certificate'?'warning':'info'">{{ row.type==='certificate'?'证书':'域名' }}</el-tag></template>
        </el-table-column>
        <el-table-column prop="name" label="名称" min-width="220" />
        <el-table-column prop="expiry_at" label="到期日" width="130" />
        <el-table-column label="已过期" width="130">
          <!-- 显示「已过期 140 天」而不是「-140 天」：文案不能说谎 -->
          <template #default="{ row }"><span style="color:#f56c6c; font-weight:600">已过期 {{ -row.days }} 天</span></template>
        </el-table-column>
        <el-table-column label="操作" width="120">
          <template #default="{ row }">
            <el-button v-if="row.type==='certificate'" link type="primary" @click="$router.push('/certs')">去续期</el-button>
            <el-button v-else link type="primary" @click="$router.push('/domains')">查看</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <el-card shadow="never" style="margin-top:14px">
      <template #header><span style="font-weight:600">⚠ 30 天内到期（点进处理）</span></template>
      <LoadError :error="error" title="到期清单未加载" @retry="load" />
      <el-table :data="expiring" size="small">
        <el-table-column label="类型" width="90">
          <template #default="{ row }"><el-tag size="small" :type="row.type==='certificate'?'warning':'info'">{{ row.type==='certificate'?'证书':'域名' }}</el-tag></template>
        </el-table-column>
        <el-table-column prop="name" label="名称" min-width="220" />
        <el-table-column prop="expiry_at" label="到期日" width="130" />
        <el-table-column label="剩余" width="110">
          <template #default="{ row }"><span :style="{color: row.days<=7?'#f56c6c':'#e6a23c', fontWeight:600}">{{ row.days }} 天</span></template>
        </el-table-column>
        <el-table-column label="操作" width="120">
          <template #default="{ row }">
            <el-button v-if="row.type==='certificate'" link type="primary" @click="$router.push('/certs')">去续期</el-button>
            <el-button v-else link type="primary" @click="$router.push('/domains')">查看</el-button>
          </template>
        </el-table-column>
      </el-table>
      <!-- 「无到期项」是结论，取不到数据时不能这么说 -->
      <el-empty v-if="!error && !expiring.length" description="30 天内无到期项" :image-size="60" />
    </el-card>
  </div>
</template>

<script setup>
import { ref, onMounted, computed } from 'vue'
import { dashboard, listDomains } from '../api/cmdb'
import { useAppStore } from '../stores/app'
import { useLoadState } from '../composables/useLoadState'
import LoadError from '../components/LoadError.vue'

const { loading, error, run, num } = useLoadState()
const app = useAppStore()
const data = ref({})
const domains = ref([])
function chip(c) { return c ? { color: c, borderColor: c + '66', background: c + '14' } : {} }
const expiring = computed(() => data.value.expiring || [])
// 后端已经把已过期从 expiring 里分出来了（CMDB-016）
const expired = computed(() => data.value.expired || [])
// 项目状态统计（按字典顺序，只列有的）
const projStats = computed(() => app.projectStatuses.map((s) => ({ label: s.label, color: s.color, n: app.projects.filter((p) => p.status === s.label).length })).filter((x) => x.n > 0))
const projNone = computed(() => app.projects.filter((p) => !p.status).length)
const domStats = computed(() => app.domainStatuses.map((s) => ({ label: s.label, color: s.color, n: domains.value.filter((d) => d.life_status === s.label).length })).filter((x) => x.n > 0))
const domNone = computed(() => domains.value.filter((d) => !d.life_status && !d.ignored).length)
// 标签文案与展示台(/dashboard)保持逐字一致——同一个字段在两个页面叫两个名字，
// 就会被当成两个指标（CMDB-014）。
const cardGroups = computed(() => [
  {
    title: '台账（CMDB 登记管理的资产）',
    tip: '来自 CMDB 自己的登记数据',
    cards: [
      { label: '配置项', num: data.value.ci_total || 0, color: '#1f2430' },
      { label: '注册域名', num: data.value.domain_total || 0, color: '#3b82f6' },
      { label: '解析记录', num: data.value.record_total || 0, color: '#2dd4bf' },
      { label: '已登记证书', num: data.value.cert_total || 0, color: '#10b981' },
      { label: '已登记证书过期', num: data.value.cert_expired || 0, color: '#909399' },
    ],
  },
  {
    title: '线上探测（对线上域名实测 TLS 的结果）',
    tip: '与台账不是同一批对象，数量大于台账属正常',
    cards: [
      { label: '线上证书 30 天内到期', num: data.value.online_cert_expiring || 0, color: '#e6a23c', to: '/cert-inspect' },
      { label: '线上证书已过期', num: data.value.online_cert_expired || 0, color: '#f56c6c', to: '/cert-inspect' },
      { label: '线上证书检测失败', num: data.value.online_cert_failed || 0, color: '#909399', to: '/cert-inspect' },
    ],
  },
  {
    title: '到期（域名注册 + 已登记证书）',
    tip: '已过期与 30 天内到期互斥，不重复计数',
    cards: [
      { label: '已过期(域名/证书)', num: expired.value.length, color: '#f56c6c' },
      { label: '30天内到期(域名/证书)', num: expiring.value.length, color: '#e6a23c' },
    ],
  },
])
// 展示台数据是整页的地基，它失败就等于这一屏全部不可信；
// 域名列表只影响"生命周期概览"那一块，单独容错即可。
async function load() {
  await run(async () => { data.value = await dashboard() })
  if (error.value) { data.value = {}; domains.value = []; return }
  app.loadProjects(); app.loadStatuses()
  try { domains.value = await listDomains() } catch (e) { domains.value = [] }
}
onMounted(load)
</script>

<style scoped>
.cgroup { margin-bottom: 6px; }
.cg-head { display: flex; align-items: baseline; gap: 10px; margin-bottom: 8px; }
.cg-title { font-size: 14px; font-weight: 600; color: #303133; }
.cg-tip { font-size: 12px; color: #909399; }
.stat { text-align: center; padding: 8px; }
.clickable { cursor: pointer; transition: box-shadow .15s; }
.clickable:hover { box-shadow: 0 2px 12px rgba(0,0,0,.1); }
.num { font-size: 28px; font-weight: 700; }
.lbl { color: #909399; font-size: 13px; margin-top: 4px; }
.stat-row { display: flex; align-items: center; flex-wrap: wrap; }
.stat-title { color: #606266; font-size: 13px; margin-right: 8px; min-width: 56px; }
.clk { cursor: pointer; }
</style>
