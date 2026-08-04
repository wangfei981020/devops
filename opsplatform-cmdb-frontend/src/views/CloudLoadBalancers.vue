<template>
  <div class="page">
    <div class="page-head">
      <span class="page-title">负载均衡</span>
      <el-button :icon="Refresh" @click="load">刷新</el-button>
    </div>
    <div class="muted" style="margin-bottom:12px">云负载均衡（转发规则），只读。点「详情」看前端转发规则与追溯到的后端实例。</div>
    <LoadError :error="error" title="负载均衡列表未加载" @retry="load" />

    <el-card shadow="never">
      <div class="filters">
        <el-select v-model="fp" clearable placeholder="厂商" style="width:130px"><el-option v-for="p in opts.provider" :key="p" :label="plabel(p)" :value="p" /></el-select>
        <el-select v-model="fs" clearable placeholder="类型" style="width:150px"><el-option v-for="s in opts.scheme" :key="s" :label="s" :value="s" /></el-select>
        <span class="grow" />
        <span class="muted">共 {{ filtered.length }} 个</span>
      </div>
      <el-table :data="paged" size="small" v-loading="loading">
        <el-table-column label="厂商" width="90"><template #default="{ row }"><el-tag :style="providerStyle(row.provider)" size="small">{{ plabel(row.provider) }}</el-tag></template></el-table-column>
        <el-table-column label="项目" min-width="100"><template #default="{ row }"><el-tag v-if="row.project" :style="projectStyle(row.project)" size="small">{{ row.project }}</el-tag><span v-else class="muted">—</span></template></el-table-column>
        <el-table-column label="区域" width="120"><template #default="{ row }"><el-tag v-if="row.region" :style="regionStyle(row.region)" size="small">{{ row.region }}</el-tag><span v-else class="muted">—</span></template></el-table-column>
        <el-table-column label="名称" min-width="170"><template #default="{ row }">{{ row.name }}</template></el-table-column>
        <el-table-column label="类型" width="120"><template #default="{ row }"><el-tag size="small" :style="typeStyle(row.scheme)">{{ schemeLabel(row.scheme) }}</el-tag></template></el-table-column>
        <el-table-column label="前端 VIP" min-width="150"><template #default="{ row }"><span class="mono">{{ row.vip || '—' }}</span></template></el-table-column>
        <el-table-column label="端口" width="100"><template #default="{ row }"><span class="mono">{{ row.port_range || '—' }}</span></template></el-table-column>
        <el-table-column label="协议" width="80"><template #default="{ row }">{{ row.protocol || '—' }}</template></el-table-column>
        <el-table-column label="后端" width="70" align="right"><template #default="{ row }">{{ (row.backends || []).length }}</template></el-table-column>
        <el-table-column label="操作" width="80"><template #default="{ row }">
          <el-button link type="primary" :icon="View" @click="openDetail(row)">详情</el-button>
        </template></el-table-column>
      </el-table>
      <el-pagination v-model:current-page="page" v-model:page-size="size" :page-sizes="[10,20,50,100]"
        :total="filtered.length" layout="total, sizes, prev, pager, next" style="margin-top:12px; justify-content:flex-end" />
    </el-card>

    <!-- 详情 -->
    <el-dialog :close-on-click-modal="false" :close-on-press-escape="false" v-model="dlg" :title="detail ? '负载均衡详情 · ' + detail.name : '负载均衡详情'" width="640px">
      <template v-if="detail">
        <div class="sec-title">前端（转发规则）</div>
        <el-descriptions :column="2" size="small" border>
          <el-descriptions-item label="厂商">{{ plabel(detail.provider) }}</el-descriptions-item>
          <el-descriptions-item label="项目">{{ detail.project || '—' }}</el-descriptions-item>
          <el-descriptions-item label="类型">{{ schemeLabel(detail.scheme) }}</el-descriptions-item>
          <el-descriptions-item label="区域">{{ detail.region || '—' }}</el-descriptions-item>
          <el-descriptions-item label="前端 VIP"><span class="mono">{{ detail.vip || '—' }}</span></el-descriptions-item>
          <el-descriptions-item label="端口"><span class="mono">{{ detail.port_range || '—' }}</span></el-descriptions-item>
          <el-descriptions-item label="协议">{{ detail.protocol || '—' }}</el-descriptions-item>
          <el-descriptions-item label="目标">{{ detail.target || '—' }}</el-descriptions-item>
          <el-descriptions-item label="self_link" :span="2">
            <span class="mono" style="word-break:break-all">{{ detail.self_link || '—' }}</span>
          </el-descriptions-item>
        </el-descriptions>

        <div class="sec-title" style="margin-top:16px">后端实例（{{ (detail.backends || []).length }}）</div>
        <el-table v-if="(detail.backends || []).length" :data="detail.backends" size="small" border>
          <el-table-column label="主机名" min-width="180"><template #default="{ row }"><span class="mono">{{ row.instance }}</span></template></el-table-column>
          <el-table-column label="内网 IP" width="150"><template #default="{ row }"><span class="mono">{{ row.internal_ip || '—' }}</span></template></el-table-column>
          <el-table-column label="实例组" min-width="150"><template #default="{ row }">{{ row.group || '—' }}</template></el-table-column>
          <el-table-column label="zone" width="130"><template #default="{ row }">{{ row.zone || '—' }}</template></el-table-column>
        </el-table>
        <el-empty v-else description="未追溯到后端实例（可能是无实例后端 / 服务型后端，或需重新同步）" :image-size="50" />
      </template>
      <template #footer><el-button @click="dlg=false">关闭</el-button></template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { useLoadState } from '../composables/useLoadState'
import LoadError from '../components/LoadError.vue'
import { Refresh, View } from '@element-plus/icons-vue'
import { listCloudLoadBalancers } from '../api/cmdb'
import { providerLabel as plabel, providerStyle, projectStyle, regionStyle, typeStyle } from '../utils/cloud'
import { usePaged } from '../composables/usePaged'

const { loading, error, run } = useLoadState()
const rows = ref([]), fp = ref(null), fs = ref(null)
const dlg = ref(false), detail = ref(null)

const opts = computed(() => ({
  provider: [...new Set(rows.value.map((r) => r.provider))].filter(Boolean),
  scheme: [...new Set(rows.value.map((r) => r.scheme))].filter(Boolean),
}))
const filtered = computed(() => rows.value.filter((r) => (!fp.value || r.provider === fp.value) && (!fs.value || r.scheme === fs.value)))
const { page, size, paged } = usePaged(filtered)
function schemeLabel(s) { return ({ EXTERNAL: '外部', EXTERNAL_MANAGED: '外部(托管)', INTERNAL: '内部', INTERNAL_MANAGED: '内部(托管)' }[s] || s || '—') }
function openDetail(row) { detail.value = row; dlg.value = true }

async function load() {
  // 同 CloudIps：失败不能退化成"暂无数据"（CMDB-013）
  await run(async () => { rows.value = await listCloudLoadBalancers() })
}
onMounted(load)
</script>

<style scoped>
.filters { display:flex; gap:10px; align-items:center; margin-bottom:12px; }
.grow { flex:1; }
.mono { font-family: ui-monospace, Menlo, Consolas, monospace; font-size:12px; }
.muted { color:#909399; }
.sec-title { font-weight:600; font-size:13px; margin-bottom:8px; }
</style>
