<template>
  <div>
    <div style="margin-bottom:16px;display:flex;gap:8px;">
      <el-button type="primary" @click="$refs.dlg.open()">添加集群</el-button>
      <RefreshButton label="刷新选中" :cluster-ids="selected" @done="reload" />
      <RefreshButton label="刷新全部" all @done="reload" />
    </div>
    <el-table :data="store.items" v-loading="store.loading" @selection-change="onSelect" stripe>
      <el-table-column type="selection" width="40" />
      <el-table-column prop="project_id" label="项目" min-width="120" />
      <el-table-column prop="location" label="区域" min-width="100" />
      <el-table-column label="集群名" min-width="180">
        <template #default="{ row }">
          <router-link :to="`/clusters/${row.id}`">{{ row.name }}</router-link>
        </template>
      </el-table-column>
      <el-table-column label="SA Key" min-width="100">
        <template #default="{ row }">
          <el-tag v-if="row.has_sa_key" type="success" size="small">已配置</el-tag>
          <el-tag v-else type="danger" size="small">未配置</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="当前版本" min-width="160">
        <template #default="{ row }">{{ row.snapshot?.current_version || '-' }}</template>
      </el-table-column>
      <el-table-column label="可升级最大版本" min-width="160">
        <template #default="{ row }">{{ row.snapshot?.max_upgradable_version || '-' }}</template>
      </el-table-column>
      <el-table-column label="GKE 最新" min-width="160">
        <template #default="{ row }">{{ row.snapshot?.latest_available_version || '-' }}</template>
      </el-table-column>
      <el-table-column label="落后(可升级)" min-width="160">
        <template #default="{ row }">
          <VersionDiffBadge v-if="row.snapshot" :behind="row.snapshot.current_to_max_versions_behind" :diff="row.snapshot.current_to_max_version_diff" />
        </template>
      </el-table-column>
      <el-table-column label="落后(最新)" min-width="160">
        <template #default="{ row }">
          <VersionDiffBadge v-if="row.snapshot" :behind="row.snapshot.current_to_latest_versions_behind" :diff="row.snapshot.current_to_latest_version_diff" />
        </template>
      </el-table-column>
      <el-table-column label="标准EOL" min-width="110">
        <template #default="{ row }"><EolCell :date="row.snapshot?.std_support_end" /></template>
      </el-table-column>
      <el-table-column label="扩展EOL" min-width="110">
        <template #default="{ row }"><EolCell :date="row.snapshot?.ext_support_end" /></template>
      </el-table-column>
      <el-table-column label="最后刷新" min-width="160">
        <template #default="{ row }">{{ formatTime(row.snapshot?.last_refreshed_at) }}</template>
      </el-table-column>
      <el-table-column label="操作" width="240" fixed="right">
        <template #default="{ row }">
          <RefreshButton label="刷新" :cluster-ids="[row.id]" @done="reload" />
          <el-button size="small" @click="$refs.dlg.open(row)">编辑</el-button>
          <el-button size="small" type="danger" @click="onDelete(row)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>
    <ClusterFormDialog ref="dlg" @saved="reload" />
  </div>
</template>
<script setup>
import { onMounted, ref } from 'vue'
import { useClustersStore } from '../stores/clusters'
import { useAppStore } from '../stores/app'
import { deleteCluster } from '../api/clusters'
import { ElMessage } from 'element-plus'
import VersionDiffBadge from '../components/VersionDiffBadge.vue'
import EolCell from '../components/EolCell.vue'
import RefreshButton from '../components/RefreshButton.vue'
import ClusterFormDialog from '../components/ClusterFormDialog.vue'

const store = useClustersStore()
const app = useAppStore()
const selected = ref([])

function onSelect(rows) {
  selected.value = rows.map(r => r.id)
}
async function reload() {
  await new Promise(r => setTimeout(r, 1500))
  await store.load()
}
async function onDelete(row) {
  if (!await app.showConfirm(`确认删除监控配置 ${row.name}？（不会删除 GKE 集群本身）`)) return
  try {
    await deleteCluster(row.id)
    ElMessage.success('已删除')
    await store.load()
  } catch (e) {
    ElMessage.error('删除失败：' + (e.response?.data?.error || e.message))
  }
}
function formatTime(s) {
  if (!s) return '-'
  return new Date(s).toLocaleString('zh-CN')
}
onMounted(() => store.load())
</script>
