<template>
  <div class="page">
    <div class="page-head"><span class="page-title">模型管理</span></div>
    <el-card shadow="never">
      <el-table :data="tPaged" size="small">
        <el-table-column prop="code" label="类型 code" width="180" />
        <el-table-column prop="name" label="名称" width="160" />
        <el-table-column label="实例数" width="120"><template #default="{ row }">{{ counts[row.code] || 0 }}</template></el-table-column>
        <el-table-column label="说明" min-width="280"><template #default="{ row }">{{ desc[row.code] || 'CI 类型' }}</template></el-table-column>
      </el-table>
      <el-pagination v-if="types.length > tSize" v-model:current-page="tPage" v-model:page-size="tSize" :page-sizes="[10,20,50,100]"
        :total="types.length" layout="total, sizes, prev, pager, next" style="margin-top:12px; justify-content:flex-end" />
      <div class="muted" style="margin-top:12px">第一期预置「域名 / 证书」两类 CI；后续可扩展主机 / 应用 / 数据库 / 负载均衡等。</div>
    </el-card>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { listCITypes, dashboard } from '../api/cmdb'
import { usePaged } from '../composables/usePaged'

const types = ref([])
const { page: tPage, size: tSize, paged: tPaged } = usePaged(types)
const counts = ref({})
const desc = {
  domain: '域名资产：注册商 / 到期 / 解析状态',
  certificate: '证书资产：CA / 绑定域名 / 到期 / 自动续期',
}
onMounted(async () => {
  types.value = await listCITypes()
  try { const d = await dashboard(); counts.value = { domain: d.domain_total, certificate: d.cert_total } } catch (e) {}
})
</script>
