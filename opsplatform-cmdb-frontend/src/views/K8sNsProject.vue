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
        <el-button v-if="canManage" :icon="MagicStick" @click="autoFill">按名称智能填充</el-button>
        <span class="muted" style="margin-left:auto">
          <!-- 「已全部归属」同样是断言，取不到数据时不能说（CMDB-013）。
               0 个命名空间也不能说"已全部归属"——那是"没东西可归属"，不是"归属都做完了"。 -->
          <template v-if="error">共 — 命名空间 · <b style="color:#f56c6c">数据未加载，归属情况未知</b></template>
          <template v-else-if="!rows.length">共 0 命名空间 · <span style="color:#e6a23c">该集群没有命名空间数据，无从判断归属</span></template>
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

    <el-dialog v-model="auto.show" title="按名称自动归属（预览）" width="760px" :close-on-click-modal="false">
      <div v-loading="auto.loading">
        <el-alert type="info" :closable="false" show-icon style="margin-bottom:12px">
          <template #title>
            将填充 <b>{{ auto.willApply }}</b> 个命名空间的归属；
            已有归属的不会被覆盖，平台组件和无法判断的保持为空由你手工处理。
          </template>
        </el-alert>
        <el-table :data="auto.items" size="small" max-height="420" border>
          <el-table-column prop="namespace" label="命名空间" min-width="200" show-overflow-tooltip />
          <el-table-column label="将归属到" width="130">
            <template #default="{ row }">
              <b v-if="row.project" class="ok">{{ row.project }}</b>
              <span v-else-if="row.current" class="muted">{{ row.current }}</span>
              <span v-else class="muted">—</span>
            </template>
          </el-table-column>
          <el-table-column label="依据" width="110">
            <template #default="{ row }">
              <el-tag size="small" effect="plain" :type="ruleType(row.rule)">{{ ruleLabel(row.rule) }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="reason" label="为什么" min-width="260" show-overflow-tooltip />
        </el-table>
      </div>
      <template #footer>
        <el-button @click="auto.show = false">取消</el-button>
        <el-button type="primary" :disabled="!auto.willApply" :loading="auto.applying" @click="applyAuto">
          确认填充 {{ auto.willApply }} 个
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { useAuthStore } from '../stores/auth'
import { MagicStick } from '@element-plus/icons-vue'
import { listK8sClusters, listK8sNsProjects, setK8sNsProject, listProjects, autoNsProjects } from '../api/cmdb'
import { pickDefaultCluster } from '../composables/useClusterPick'
import { usePager } from '../composables/usePager'
import { useLoadState } from '../composables/useLoadState'
import Pager from '../components/Pager.vue'
import LoadError from '../components/LoadError.vue'

const auth = useAuthStore()
const canManage = computed(() => auth.hasButton('manage_ns_project'))
const { loading, error, run } = useLoadState()
const clusters = ref([]); const projects = ref([]); const rows = ref([])
const { page, pageSize, paged } = usePager(rows)
const clusterId = ref(null)

const unassigned = computed(() => rows.value.filter(r => !r.project).length)

// 建议：命名空间名包含某项目名(或反之) → 建议该项目
// ⚠️ 匹配规则已移到后端（handlers/ns_project_auto.go），前端不再自己判。
//
//	原来这里用的是**双向 includes**：`n.includes(pn) || pn.includes(n)`。
//	那个判据太松——项目 `g32` 会吞掉 `g32x-foo` 这种毫不相干的命名空间，
//	而归错的代价是成本报表把开销算到别人头上，比"未分配"更糟
//	（未分配至少是诚实的）。
//	后端那套是：精确同名 > `项目名-` 前缀（必须带分隔符）> 平台组件不归 > 留空。
//	规则只有一份，前后端不会打架。

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

// 先预览再落库：批量写映射会直接改变成本报表的归属结果，
// 让人看清楚哪些会被填、按什么理由填，再决定要不要执行。
const auto = reactive({ show: false, loading: false, applying: false, items: [], stat: {}, willApply: 0 })

async function autoFill() {
  if (!clusterId.value) { ElMessage.warning('请先选择集群'); return }
  auto.loading = true
  auto.show = true
  try {
    const r = await autoNsProjects(clusterId.value, true)
    if (r.error) { ElMessage.warning(r.error + (r.hint ? '——' + r.hint : '')); auto.show = false; return }
    auto.items = r.items || []
    auto.stat = r.stat || {}
    auto.willApply = r.will_apply || 0
  } catch (e) {
    ElMessage.error(e?.raw?.response?.data?.error || e?.message || '预览失败')
    auto.show = false
  } finally { auto.loading = false }
}

async function applyAuto() {
  auto.applying = true
  try {
    const r = await autoNsProjects(clusterId.value, false)
    ElMessage.success(r.msg || `已归属 ${r.applied} 个`)
    auto.show = false
    load()
  } catch (e) {
    ElMessage.error(e?.raw?.response?.data?.error || e?.message || '执行失败')
  } finally { auto.applying = false }
}

const ruleLabel = (r) => ({ exact: '同名', prefix: '前缀匹配', platform: '平台组件', none: '无法判断', keep: '已有归属' }[r] || r)
const ruleType = (r) => ({ exact: 'success', prefix: 'success', platform: 'info', none: 'warning', keep: '' }[r] || '')

onMounted(async () => {
  const ok = await run(async () => {
    clusters.value = await listK8sClusters()
    projects.value = await listProjects()
    return true
  })
  if (!ok) return
  if (clusters.value.length) { clusterId.value = pickDefaultCluster(clusters.value); load() }
})
</script>

<style scoped>
.page-head { margin-bottom: 14px; }
.page-title { font-size: 18px; font-weight: 600; }
.muted { color: #909399; font-size: 12px; }
.bar { display: flex; gap: 10px; align-items: center; margin-bottom: 12px; }
</style>
