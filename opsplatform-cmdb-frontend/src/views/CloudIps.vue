<template>
  <div class="page">
    <div class="page-head">
      <span class="page-title">IP 地址</span>
      <el-button :icon="Refresh" @click="load">刷新</el-button>
    </div>
    <div class="muted" style="margin-bottom:12px">聚合各云资源的 IP（主机内外网 / 预留静态IP / 负载均衡 VIP），只读。<b style="color:#f56c6c">🔴闲置</b>=预留静态IP 没绑任何资源，仍在计费。</div>
    <LoadError :error="error" title="IP 列表未加载" @retry="load" />

    <el-card shadow="never">
      <div class="filters">
        <el-input v-model="kw" placeholder="搜索 IP / 归属" clearable :prefix-icon="Search" style="width:220px" />
        <el-select v-model="fp" clearable placeholder="厂商" style="width:130px"><el-option v-for="p in opts.provider" :key="p" :label="plabel(p)" :value="p" /></el-select>
        <el-select v-model="fk" clearable placeholder="类型" style="width:150px"><el-option v-for="k in opts.kind" :key="k" :label="k" :value="k" /></el-select>
        <el-checkbox v-model="onlyIdle" style="margin-left:6px">只看闲置静态IP</el-checkbox>
        <span class="grow" />
        <span class="muted">共 {{ filtered.length }} 个 · 🔴闲置 {{ idleCount }}</span>
      </div>
      <el-table :data="paged" size="small" v-loading="loading">
        <el-table-column label="厂商" width="90"><template #default="{ row }"><el-tag :style="providerStyle(row.provider)" size="small">{{ plabel(row.provider) }}</el-tag></template></el-table-column>
        <el-table-column label="项目" min-width="100"><template #default="{ row }"><el-tag v-if="row.project" :style="projectStyle(row.project)" size="small">{{ row.project }}</el-tag><span v-else class="muted">—</span></template></el-table-column>
        <el-table-column label="区域" width="130"><template #default="{ row }"><el-tag v-if="row.region" :style="regionStyle(row.region)" size="small">{{ row.region }}</el-tag><span v-else class="muted">—</span></template></el-table-column>
        <el-table-column label="IP 地址" min-width="150"><template #default="{ row }"><span class="mono">{{ row.ip }}</span></template></el-table-column>
        <el-table-column label="类型" width="120"><template #default="{ row }"><el-tag size="small" :style="typeStyle(row.kind)">{{ row.kind }}</el-tag></template></el-table-column>
        <el-table-column label="归属资源" min-width="180"><template #default="{ row }">
          <span :class="{ idle: row.idle }">{{ row.owner || '—' }}</span>
        </template></el-table-column>
        <el-table-column label="状态" width="110"><template #default="{ row }">
          <el-tag v-if="row.idle" type="danger" size="small">🔴 闲置·计费</el-tag>
          <el-tag v-else type="success" size="small" effect="plain">使用中</el-tag>
        </template></el-table-column>
      </el-table>
      <el-pagination v-model:current-page="page" v-model:page-size="size" :page-sizes="[10,20,50,100]"
        :total="filtered.length" layout="total, sizes, prev, pager, next" style="margin-top:12px; justify-content:flex-end" />
    </el-card>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { useLoadState } from '../composables/useLoadState'
import LoadError from '../components/LoadError.vue'
import { Refresh, Search } from '@element-plus/icons-vue'
import { listCloudIps } from '../api/cmdb'
import { providerLabel as plabel, providerStyle, projectStyle, regionStyle, typeStyle } from '../utils/cloud'
import { usePaged } from '../composables/usePaged'

const { loading, error, run } = useLoadState()
const rows = ref([])
const kw = ref(''), fp = ref(null), fk = ref(null), onlyIdle = ref(false)

const opts = computed(() => ({
  provider: [...new Set(rows.value.map((r) => r.provider))].filter(Boolean),
  kind: [...new Set(rows.value.map((r) => r.kind))].filter(Boolean),
}))
const filtered = computed(() => rows.value.filter((r) => {
  const k = kw.value.toLowerCase()
  return (!k || (r.ip || '').includes(k) || (r.owner || '').toLowerCase().includes(k)) &&
    (!fp.value || r.provider === fp.value) && (!fk.value || r.kind === fk.value) &&
    (!onlyIdle.value || r.idle)
}))
const { page, size, paged } = usePaged(filtered)
const idleCount = computed(() => rows.value.filter((r) => r.idle).length)
function kindType(k) { return k.includes('VIP') ? 'warning' : k.includes('外网') ? 'primary' : 'info' }

async function load() {
  // 失败必须留在页面上。原来是 catch 里弹一下 toast——3 秒后消失，
  // 表格显示"暂无数据"，看起来就像"云上真的没有 IP"。
  // 这类系统最危险的失效模式就是故障时告诉运维"没问题"（CMDB-013）。
  await run(async () => { rows.value = await listCloudIps() })
}
onMounted(load)
</script>

<style scoped>
.filters { display:flex; gap:10px; align-items:center; margin-bottom:12px; }
.grow { flex:1; }
.mono { font-family: ui-monospace, Menlo, Consolas, monospace; font-size:12px; }
.idle { color:#f56c6c; }
.muted { color:#909399; }
</style>
