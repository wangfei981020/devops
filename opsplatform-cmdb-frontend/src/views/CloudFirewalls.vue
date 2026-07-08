<template>
  <div class="page">
    <div class="page-head">
      <span class="page-title">防火墙</span>
      <el-button :icon="Refresh" @click="load">刷新</el-button>
    </div>
    <div class="muted" style="margin-bottom:12px">云防火墙规则，只读。<b style="color:#f56c6c">🔴高危</b>=入站对 <span class="mono">0.0.0.0/0</span> 放行敏感端口(22/3389/3306/6379…) 或全端口。</div>

    <el-card shadow="never">
      <div class="filters">
        <el-select v-model="fp" clearable placeholder="厂商" style="width:130px"><el-option v-for="p in opts.provider" :key="p" :label="plabel(p)" :value="p" /></el-select>
        <el-select v-model="fnet" clearable placeholder="VPC" style="width:150px"><el-option v-for="n in opts.network" :key="n" :label="n" :value="n" /></el-select>
        <el-select v-model="fdir" clearable placeholder="方向" style="width:120px"><el-option label="入站" value="INGRESS" /><el-option label="出站" value="EGRESS" /></el-select>
        <el-checkbox v-model="onlyRisk" style="margin-left:6px">只看高危</el-checkbox>
        <span class="grow" />
        <span class="muted">共 {{ filtered.length }} 条 · <span style="color:#f56c6c">🔴高危 {{ riskCount }}</span></span>
      </div>
      <el-table :data="filtered" size="small" v-loading="loading" :row-class-name="rowCls">
        <el-table-column label="厂商" width="80"><template #default="{ row }"><el-tag :style="providerStyle(row.provider)" size="small">{{ plabel(row.provider) }}</el-tag></template></el-table-column>
        <el-table-column label="项目" min-width="100"><template #default="{ row }"><el-tag v-if="row.project" :style="projectStyle(row.project)" size="small">{{ row.project }}</el-tag><span v-else class="muted">—</span></template></el-table-column>
        <el-table-column label="规则名" min-width="160"><template #default="{ row }">
          {{ row.name }}
          <el-tag v-if="row.high_risk" type="danger" size="small" style="margin-left:6px">高危</el-tag>
          <el-tag v-if="row.disabled" type="info" size="small" effect="plain" style="margin-left:4px">已停用</el-tag>
        </template></el-table-column>
        <el-table-column prop="network" label="VPC" width="120" />
        <el-table-column label="方向" width="80"><template #default="{ row }"><el-tag :style="typeStyle(row.direction)" size="small">{{ row.direction==='EGRESS' ? '出站' : '入站' }}</el-tag></template></el-table-column>
        <el-table-column prop="priority" label="优先级" width="80" align="right" />
        <el-table-column label="协议:端口" min-width="150"><template #default="{ row }"><span class="mono">{{ row.protocols || '—' }}</span></template></el-table-column>
        <el-table-column label="源" min-width="140"><template #default="{ row }"><span class="mono" :class="{ risk: (row.source_ranges||'').includes('0.0.0.0/0') }">{{ row.source_ranges || '—' }}</span></template></el-table-column>
        <el-table-column label="目标标签" min-width="120"><template #default="{ row }">{{ row.target_tags || '—' }}</template></el-table-column>
        <el-table-column label="动作" width="80"><template #default="{ row }"><el-tag :type="row.action==='allow' ? 'success' : 'danger'" size="small" effect="plain">{{ row.action==='allow' ? '允许' : '拒绝' }}</el-tag></template></el-table-column>
      </el-table>
    </el-card>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Refresh } from '@element-plus/icons-vue'
import { listCloudFirewalls } from '../api/cmdb'
import { providerLabel as plabel, providerStyle, projectStyle, typeStyle } from '../utils/cloud'

const rows = ref([]), loading = ref(false)
const fp = ref(null), fnet = ref(null), fdir = ref(null), onlyRisk = ref(false)

const opts = computed(() => ({
  provider: [...new Set(rows.value.map((r) => r.provider))].filter(Boolean),
  network: [...new Set(rows.value.map((r) => r.network))].filter(Boolean),
}))
const filtered = computed(() => rows.value.filter((r) =>
  (!fp.value || r.provider === fp.value) && (!fnet.value || r.network === fnet.value) &&
  (!fdir.value || r.direction === fdir.value) && (!onlyRisk.value || r.high_risk)))
const riskCount = computed(() => rows.value.filter((r) => r.high_risk).length)
function rowCls({ row }) { return row.high_risk ? 'risk-row' : '' }

async function load() {
  loading.value = true
  try { rows.value = await listCloudFirewalls() } catch (e) { ElMessage.error('加载失败') } finally { loading.value = false }
}
onMounted(load)
</script>

<style scoped>
.filters { display:flex; gap:10px; align-items:center; margin-bottom:12px; }
.grow { flex:1; }
.mono { font-family: ui-monospace, Menlo, Consolas, monospace; font-size:12px; }
.risk { color:#f56c6c; font-weight:600; }
.muted { color:#909399; }
:deep(.risk-row) { background:#fef0f0; }
</style>
