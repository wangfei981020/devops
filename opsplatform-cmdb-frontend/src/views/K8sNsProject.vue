<template>
  <div class="page">
    <div class="page-head">
      <span class="page-title">命名空间 → 项目归属</span>
      <span class="muted" style="margin-left:10px">多项目共享集群时，按此把命名空间成本拆到项目（一个项目可挂多个命名空间）</span>
    </div>
    <el-card shadow="never">
      <LoadError :error="error" @retry="load" />
      <div class="bar">
        <el-select v-model="clusterId" placeholder="选集群" style="width:220px" @change="load">
          <el-option v-for="c in clusters" :key="c.id" :label="(c.display_name||c.name)+' · '+c.environment" :value="c.id" />
        </el-select>
        <el-button :icon="MagicStick" @click="autoFill">按名称智能填充</el-button>
        <span class="muted" style="margin-left:auto">
          <!-- 「已全部归属」同样是断言，取不到数据时不能说（CMDB-013） -->
          <template v-if="error">共 — 命名空间 · <b style="color:#f56c6c">数据未加载，归属情况未知</b></template>
          <template v-else>共 {{ rows.length }} 命名空间 · <b v-if="unassigned" style="color:#e6a23c">未分配 {{ unassigned }} ⚠</b><span v-else>已全部归属</span></template>
        </span>
      </div>
      <el-table :data="paged" size="small" v-loading="loading">
        <el-table-column prop="name" label="命名空间" min-width="220" />
        <el-table-column label="建议" width="160"><template #default="{ row }">
          <span v-if="suggest(row.name)" class="muted">{{ suggest(row.name) }}</span><span v-else class="muted">—</span>
        </template></el-table-column>
        <el-table-column label="项目归属" min-width="240"><template #default="{ row }">
          <el-select v-model="row.project" filterable clearable placeholder="选项目（空=未分配）" style="width:220px" @change="save(row)">
            <el-option v-for="p in projects" :key="p.id||p.name" :label="p.name" :value="p.name" />
          </el-select>
        </template></el-table-column>
        <el-table-column label="状态" width="100"><template #default="{ row }">
          <el-tag size="small" :type="row.project?'success':'warning'">{{ row.project ? '已分配' : '未分配' }}</el-tag>
        </template></el-table-column>
      </el-table>
      <Pager :total="rows.length" v-model:page="page" v-model:page-size="pageSize" />
      <el-empty v-if="!loading && !error && !rows.length" description="该集群还没同步命名空间，先去集群管理点「同步」" />
    </el-card>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { MagicStick } from '@element-plus/icons-vue'
import { listK8sClusters, listK8sNsProjects, setK8sNsProject, listProjects } from '../api/cmdb'
import { usePager } from '../composables/usePager'
import { useLoadState } from '../composables/useLoadState'
import Pager from '../components/Pager.vue'
import LoadError from '../components/LoadError.vue'

const { loading, error, run } = useLoadState()
const clusters = ref([]); const projects = ref([]); const rows = ref([])
const { page, pageSize, paged } = usePager(rows)
const clusterId = ref(null)

const unassigned = computed(() => rows.value.filter(r => !r.project).length)

// 建议：命名空间名包含某项目名(或反之) → 建议该项目
function suggest(nsName) {
  const n = (nsName || '').toLowerCase()
  const hit = projects.value.find(p => { const pn = (p.name || '').toLowerCase(); return pn && (n.includes(pn) || pn.includes(n)) })
  return hit ? hit.name : ''
}

async function load() {
  if (!clusterId.value) return
  await run(async () => { rows.value = await listK8sNsProjects(clusterId.value) })
  if (error.value) rows.value = []
}

async function save(row) {
  try {
    await setK8sNsProject({ cluster_id: clusterId.value, namespace: row.name, project: row.project || '' })
    ElMessage.success('已保存')
  } catch (e) { ElMessage.error('保存失败') }
}

async function autoFill() {
  let n = 0
  for (const row of rows.value) {
    if (!row.project) { const s = suggest(row.name); if (s) { row.project = s; await save(row); n++ } }
  }
  ElMessage.success(n ? `已智能填充 ${n} 个` : '没有可自动匹配的命名空间')
}

onMounted(async () => {
  const ok = await run(async () => {
    clusters.value = await listK8sClusters()
    projects.value = await listProjects()
    return true
  })
  if (!ok) return
  if (clusters.value.length) { clusterId.value = clusters.value[0].id; load() }
})
</script>

<style scoped>
.page-head { margin-bottom: 14px; }
.page-title { font-size: 18px; font-weight: 600; }
.muted { color: #909399; font-size: 12px; }
.bar { display: flex; gap: 10px; align-items: center; margin-bottom: 12px; }
</style>
