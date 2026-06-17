<template>
  <div class="page">
    <div class="page-head"><span class="page-title">总览</span></div>
    <el-row :gutter="14">
      <el-col :span="6" v-for="c in cards" :key="c.label">
        <el-card shadow="never"><div class="stat"><div class="num" :style="{color:c.color}">{{ c.num }}</div><div class="lbl">{{ c.label }}</div></div></el-card>
      </el-col>
    </el-row>

    <el-card shadow="never" style="margin-top:14px">
      <template #header><span style="font-weight:600">⚠ 30 天内到期（点进处理）</span></template>
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
      <el-empty v-if="!expiring.length" description="30 天内无到期项" :image-size="60" />
    </el-card>
  </div>
</template>

<script setup>
import { ref, onMounted, computed } from 'vue'
import { dashboard } from '../api/cmdb'

const data = ref({})
const expiring = computed(() => data.value.expiring || [])
const cards = computed(() => [
  { label: '配置项', num: data.value.ci_total || 0, color: '#1f2430' },
  { label: '域名', num: data.value.domain_total || 0, color: '#3b82f6' },
  { label: '证书', num: data.value.cert_total || 0, color: '#10b981' },
  { label: '30天内到期', num: (data.value.expiring || []).length, color: '#e6a23c' },
])
onMounted(async () => { try { data.value = await dashboard() } catch (e) {} })
</script>

<style scoped>
.stat { text-align: center; padding: 8px; }
.num { font-size: 28px; font-weight: 700; }
.lbl { color: #909399; font-size: 13px; margin-top: 4px; }
</style>
