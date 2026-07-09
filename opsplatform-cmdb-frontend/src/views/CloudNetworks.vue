<template>
  <div class="page">
    <div class="page-head">
      <span class="page-title">VPC 网络</span>
      <el-button :icon="Refresh" @click="load">刷新</el-button>
    </div>
    <div class="muted" style="margin-bottom:12px">云上 VPC 网络与子网，只读同步。展开看子网 CIDR。</div>

    <el-card shadow="never">
      <div class="filters">
        <el-select v-model="fp" clearable placeholder="厂商" style="width:130px"><el-option v-for="p in providers" :key="p" :label="plabel(p)" :value="p" /></el-select>
        <span class="grow" />
        <span class="muted">共 {{ filtered.length }} 个 VPC · {{ subnets.length }} 个子网</span>
      </div>
      <el-table :data="paged" size="small" v-loading="loading" row-key="key">
        <el-table-column type="expand">
          <template #default="{ row }">
            <div style="padding:6px 20px 12px">
              <el-table :data="subnetsOf(row)" size="small">
                <el-table-column prop="name" label="子网" min-width="150" />
                <el-table-column label="区域" width="140"><template #default="{ row: s }"><el-tag :style="regionStyle(s.region)" size="small">{{ s.region }}</el-tag></template></el-table-column>
                <el-table-column label="CIDR" width="160"><template #default="{ row: s }"><span class="mono">{{ s.cidr }}</span></template></el-table-column>
                <el-table-column label="网关" width="140"><template #default="{ row: s }"><span class="mono">{{ s.gateway || '—' }}</span></template></el-table-column>
              </el-table>
              <el-empty v-if="!subnetsOf(row).length" description="无子网" :image-size="40" />
            </div>
          </template>
        </el-table-column>
        <el-table-column label="厂商" width="90"><template #default="{ row }"><el-tag :style="providerStyle(row.provider)" size="small">{{ plabel(row.provider) }}</el-tag></template></el-table-column>
        <el-table-column label="项目" min-width="100"><template #default="{ row }"><el-tag v-if="row.project" :style="projectStyle(row.project)" size="small">{{ row.project }}</el-tag><span v-else class="muted">—</span></template></el-table-column>
        <el-table-column label="VPC 名" min-width="160"><template #default="{ row }">{{ row.name }}</template></el-table-column>
        <el-table-column label="模式" width="100"><template #default="{ row }"><el-tag size="small" :style="typeStyle(row.mode)">{{ row.mode }}</el-tag></template></el-table-column>
        <el-table-column label="子网数" width="80" align="right"><template #default="{ row }">{{ row.subnet_count }}</template></el-table-column>
        <el-table-column label="防火墙规则" width="100" align="right"><template #default="{ row }">{{ row.firewall_count }}</template></el-table-column>
      </el-table>
      <el-pagination v-model:current-page="page" v-model:page-size="size" :page-sizes="[10,20,50,100]"
        :total="filtered.length" layout="total, sizes, prev, pager, next" style="margin-top:12px; justify-content:flex-end" />
    </el-card>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Refresh } from '@element-plus/icons-vue'
import { listCloudNetworks, listCloudSubnets } from '../api/cmdb'
import { providerLabel as plabel, providerStyle, projectStyle, regionStyle, typeStyle } from '../utils/cloud'
import { usePaged } from '../composables/usePaged'

const rows = ref([]), subnets = ref([]), loading = ref(false), fp = ref(null)

const providers = computed(() => [...new Set(rows.value.map((r) => r.provider))].filter(Boolean))
const filtered = computed(() => rows.value.filter((r) => !fp.value || r.provider === fp.value)
  .map((r) => ({ ...r, key: r.provider + '/' + r.project_id + '/' + r.name })))
const { page, size, paged } = usePaged(filtered)
function subnetsOf(row) { return subnets.value.filter((s) => s.network === row.name && s.project === row.project) }

async function load() {
  loading.value = true
  try { rows.value = await listCloudNetworks(); subnets.value = await listCloudSubnets() }
  catch (e) { ElMessage.error('加载失败') } finally { loading.value = false }
}
onMounted(load)
</script>

<style scoped>
.filters { display:flex; gap:10px; align-items:center; margin-bottom:12px; }
.grow { flex:1; }
.mono { font-family: ui-monospace, Menlo, Consolas, monospace; font-size:12px; }
.muted { color:#909399; }
</style>
