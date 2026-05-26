<template>
  <div v-loading="loading">
    <el-page-header @back="$router.back()" title="返回">
      <template #content>集群详情</template>
    </el-page-header>
    <el-descriptions v-if="cluster" :title="cluster.name" :column="2" border style="margin-top:16px">
      <el-descriptions-item label="项目">{{ cluster.project_id }}</el-descriptions-item>
      <el-descriptions-item label="区域">{{ cluster.location }}</el-descriptions-item>
      <el-descriptions-item label="当前版本">{{ snap?.current_version || '-' }}</el-descriptions-item>
      <el-descriptions-item label="可升级最大">{{ snap?.max_upgradable_version || '-' }}</el-descriptions-item>
      <el-descriptions-item label="GKE 最新">{{ snap?.latest_available_version || '-' }}</el-descriptions-item>
      <el-descriptions-item label="落后(可升级)">
        <VersionDiffBadge v-if="snap" :behind="snap.current_to_max_versions_behind" :diff="snap.current_to_max_version_diff" />
      </el-descriptions-item>
      <el-descriptions-item label="落后(最新)">
        <VersionDiffBadge v-if="snap" :behind="snap.current_to_latest_versions_behind" :diff="snap.current_to_latest_version_diff" />
      </el-descriptions-item>
      <el-descriptions-item label="标准EOL"><EolCell :date="snap?.std_support_end" /></el-descriptions-item>
      <el-descriptions-item label="扩展EOL"><EolCell :date="snap?.ext_support_end" /></el-descriptions-item>
      <el-descriptions-item label="最后刷新">{{ formatTime(snap?.last_refreshed_at) }}</el-descriptions-item>
    </el-descriptions>
    <h3 style="margin-top:24px;">节点池</h3>
    <el-table :data="snap?.nodepools || []" stripe>
      <el-table-column prop="name" label="节点池" min-width="220" />
      <el-table-column prop="current_version" label="当前版本" min-width="160" />
      <el-table-column prop="max_upgradable_version" label="可升级最大" min-width="160" />
      <el-table-column prop="latest_available_version" label="GKE 最新" min-width="160" />
      <el-table-column label="落后(可升级)" min-width="160">
        <template #default="{ row }">
          <VersionDiffBadge :behind="row.current_to_max_versions_behind" :diff="row.current_to_max_version_diff" />
        </template>
      </el-table-column>
      <el-table-column label="落后(最新)" min-width="160">
        <template #default="{ row }">
          <VersionDiffBadge :behind="row.current_to_latest_versions_behind" :diff="row.current_to_latest_version_diff" />
        </template>
      </el-table-column>
      <el-table-column label="标准EOL" min-width="110">
        <template #default="{ row }"><EolCell :date="row.std_support_end" /></template>
      </el-table-column>
      <el-table-column label="扩展EOL" min-width="110">
        <template #default="{ row }"><EolCell :date="row.ext_support_end" /></template>
      </el-table-column>
    </el-table>
  </div>
</template>
<script setup>
import { ref, onMounted, computed } from 'vue'
import { useRoute } from 'vue-router'
import { listClusters } from '../api/clusters'
import VersionDiffBadge from '../components/VersionDiffBadge.vue'
import EolCell from '../components/EolCell.vue'

const route = useRoute()
const loading = ref(false)
const cluster = ref(null)
const snap = computed(() => cluster.value?.snapshot)

function formatTime(s) {
  if (!s) return '-'
  return new Date(s).toLocaleString('zh-CN')
}

onMounted(async () => {
  loading.value = true
  try {
    const all = await listClusters()
    cluster.value = all.find(c => String(c.id) === String(route.params.id))
  } finally {
    loading.value = false
  }
})
</script>
