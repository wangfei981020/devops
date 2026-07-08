<template>
  <div class="page">
    <div class="page-head">
      <span class="page-title">负载均衡</span>
      <el-button :icon="Refresh" @click="load">刷新</el-button>
    </div>
    <div class="muted" style="margin-bottom:12px">云负载均衡（转发规则），只读。VIP 可在「IP 地址」页反查，后端主机见主机页。</div>

    <el-card shadow="never">
      <div class="filters">
        <el-select v-model="fp" clearable placeholder="厂商" style="width:130px"><el-option v-for="p in opts.provider" :key="p" :label="plabel(p)" :value="p" /></el-select>
        <el-select v-model="fs" clearable placeholder="类型" style="width:150px"><el-option v-for="s in opts.scheme" :key="s" :label="s" :value="s" /></el-select>
        <span class="grow" />
        <span class="muted">共 {{ filtered.length }} 个</span>
      </div>
      <el-table :data="filtered" size="small" v-loading="loading">
        <el-table-column label="厂商" width="90"><template #default="{ row }"><el-tag :style="providerStyle(row.provider)" size="small">{{ plabel(row.provider) }}</el-tag></template></el-table-column>
        <el-table-column label="项目" min-width="100"><template #default="{ row }"><el-tag v-if="row.project" :style="projectStyle(row.project)" size="small">{{ row.project }}</el-tag><span v-else class="muted">—</span></template></el-table-column>
        <el-table-column label="区域" width="120"><template #default="{ row }"><el-tag v-if="row.region" :style="regionStyle(row.region)" size="small">{{ row.region }}</el-tag><span v-else class="muted">—</span></template></el-table-column>
        <el-table-column label="名称" min-width="170"><template #default="{ row }">{{ row.name }}</template></el-table-column>
        <el-table-column label="类型" width="120"><template #default="{ row }"><el-tag size="small" :style="typeStyle(row.scheme)">{{ schemeLabel(row.scheme) }}</el-tag></template></el-table-column>
        <el-table-column label="前端 VIP" min-width="150"><template #default="{ row }"><span class="mono">{{ row.vip || '—' }}</span></template></el-table-column>
        <el-table-column label="端口" width="110"><template #default="{ row }"><span class="mono">{{ row.port_range || '—' }}</span></template></el-table-column>
        <el-table-column label="协议" width="90"><template #default="{ row }">{{ row.protocol || '—' }}</template></el-table-column>
        <el-table-column label="目标" min-width="150"><template #default="{ row }">{{ row.target || '—' }}</template></el-table-column>
      </el-table>
    </el-card>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Refresh } from '@element-plus/icons-vue'
import { listCloudLoadBalancers } from '../api/cmdb'
import { providerLabel as plabel, providerStyle, projectStyle, regionStyle, typeStyle } from '../utils/cloud'

const rows = ref([]), loading = ref(false), fp = ref(null), fs = ref(null)

const opts = computed(() => ({
  provider: [...new Set(rows.value.map((r) => r.provider))].filter(Boolean),
  scheme: [...new Set(rows.value.map((r) => r.scheme))].filter(Boolean),
}))
const filtered = computed(() => rows.value.filter((r) => (!fp.value || r.provider === fp.value) && (!fs.value || r.scheme === fs.value)))
function schemeLabel(s) { return ({ EXTERNAL: '外部', EXTERNAL_MANAGED: '外部(托管)', INTERNAL: '内部', INTERNAL_MANAGED: '内部(托管)' }[s] || s || '—') }

async function load() {
  loading.value = true
  try { rows.value = await listCloudLoadBalancers() } catch (e) { ElMessage.error('加载失败') } finally { loading.value = false }
}
onMounted(load)
</script>

<style scoped>
.filters { display:flex; gap:10px; align-items:center; margin-bottom:12px; }
.grow { flex:1; }
.mono { font-family: ui-monospace, Menlo, Consolas, monospace; font-size:12px; }
.muted { color:#909399; }
</style>
