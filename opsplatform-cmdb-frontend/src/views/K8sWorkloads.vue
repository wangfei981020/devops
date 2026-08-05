<template>
  <div class="page">
    <div class="page-head">
      <span class="page-title">K8s 工作负载</span>
      <span class="muted" style="margin-left:10px">Deployment / StatefulSet / DaemonSet / CronJob</span>
    </div>
    <el-card shadow="never">
      <div class="bar">
        <el-select v-model="clusterId" placeholder="选集群" style="width:200px" @change="onCluster">
          <el-option v-for="c in clusters" :key="c.id" :label="(c.display_name||c.name)+' · '+c.environment" :value="c.id" />
        </el-select>
        <el-select v-model="ns" clearable filterable placeholder="命名空间" style="width:180px" @change="load">
          <el-option v-for="n in namespaces" :key="n.name" :label="n.name" :value="n.name" />
        </el-select>
        <el-select v-model="kind" clearable placeholder="类型" style="width:150px" @change="load">
          <el-option v-for="k in kinds" :key="k" :label="k" :value="k" />
        </el-select>
        <el-input v-model="q" clearable placeholder="搜名称/镜像" style="width:200px" @keyup.enter="load" @clear="load" />
        <el-button :icon="Search" @click="load">查询</el-button>
        <el-switch v-model="onlyBad" active-text="只看异常" style="margin-left:6px" @change="page=1" />
        <span class="muted" style="margin-left:auto">
          <!-- 失败时不能报「共 0」：0 是"这个集群没有工作负载"的断言（CMDB-013） -->
          <template v-if="error">共 — · <b style="color:#f56c6c">数据未加载</b></template>
          <!-- 计数必须和表格里实际显示的一致。原来「共 N」用的是未筛选的 rows，
               而表格用的是筛选后的 display——勾上「只看异常」且没有异常时，
               就会出现「共 207」和「暂无数据」同框（CMDB-043）。
               筛选生效时把两个数都给出来，别让人怀疑是不是数据丢了。 -->
          <template v-else>
            共 {{ display.length }}
            <span v-if="display.length !== rows.length" class="muted">/ {{ rows.length }}（已筛选）</span>
            <b v-if="badCount" style="color:#f56c6c;margin-left:6px">异常 {{ badCount }}</b>
          </template>
        </span>
      </div>
      <LoadError :error="error" title="工作负载未加载" @retry="load" />
      <el-table :data="paged" size="small" v-loading="loading">
        <el-table-column prop="namespace" label="命名空间" width="150" />
        <el-table-column prop="kind" label="类型" width="120" />
        <el-table-column prop="name" label="名称" min-width="220" />
        <el-table-column label="副本" width="90"><template #default="{ row }">
          <span :class="{bad: row.status==='degraded'}">{{ row.replicas_ready }}/{{ row.replicas_desired }}</span>
        </template></el-table-column>
        <el-table-column label="镜像" min-width="260"><template #default="{ row }">
          <span class="muted">{{ row.image }}</span><b style="margin-left:4px">:{{ row.image_tag }}</b>
          <el-tag v-if="row.image_tag==='latest'" size="small" type="warning" style="margin-left:6px">latest</el-tag>
        </template></el-table-column>
        <el-table-column label="状态" width="100"><template #default="{ row }">
          <el-tag size="small" :type="row.status==='healthy'?'success':(row.status==='degraded'?'danger':'info')">{{ statusText(row.status) }}</el-tag>
        </template></el-table-column>
        <el-table-column label="操作" width="130" fixed="right"><template #default="{ row }">
          <el-button link type="primary" size="small" @click="openPods(row)">Pod</el-button>
          <el-button link type="primary" size="small" @click="openChanges(row)">变更</el-button>
        </template></el-table-column>
      </el-table>
      <Pager :total="display.length" v-model:page="page" v-model:page-size="pageSize" />

      <el-dialog :close-on-click-modal="false" v-model="podDlg" :title="`Pod · ${podWl?.namespace}/${podWl?.name}`" width="820px" top="6vh">
        <el-table :data="wlPods" size="small" v-loading="podLoading" max-height="480">
          <el-table-column prop="name" label="Pod" min-width="240" />
          <el-table-column prop="node_name" label="节点" min-width="150" />
          <el-table-column label="状态" width="100"><template #default="{ row }">
            <el-tag size="small" :type="row.phase==='Running'||row.phase==='Succeeded'?'success':(row.phase==='Failed'?'danger':'warning')">{{ row.phase }}</el-tag>
          </template></el-table-column>
          <el-table-column label="失败原因" min-width="140"><template #default="{ row }"><span v-if="row.reason" style="color:#f56c6c">{{ row.reason }}</span><span v-else class="muted">—</span></template></el-table-column>
          <el-table-column prop="restarts" label="重启" width="70" />
        </el-table>
        <el-empty v-if="!podLoading && !error && !wlPods.length" description="无 Pod" :image-size="50" />
      </el-dialog>

      <el-dialog :close-on-click-modal="false" v-model="chDlg" :title="`变更记录 · ${chWl?.namespace}/${chWl?.name}`" width="640px">
        <el-table :data="changes" size="small" v-loading="chLoading" max-height="420">
          <el-table-column prop="changed_at" label="时间" width="160" />
          <el-table-column prop="field" label="字段" width="90"><template #default="{ row }">{{ row.field==='image'?'镜像':'副本' }}</template></el-table-column>
          <el-table-column label="变化" min-width="280"><template #default="{ row }">
            <span class="muted">{{ row.old_value }}</span> → <b>{{ row.new_value }}</b>
          </template></el-table-column>
        </el-table>
        <el-empty v-if="!chLoading && !error && !changes.length" description="暂无变更（首次纳管后、镜像/副本变化才会记录）" />
      </el-dialog>
      <!-- 空态也要分两种：真没数据 vs 筛掉了。文案写死"先去同步"会让人
           对着一个筛选器造成的空列表去点同步。 -->
      <el-empty v-if="!loading && !error && !rows.length" description="无数据，先去集群管理点「同步」" />
      <el-empty v-else-if="!loading && !error && !display.length"
                :description="`当前筛选条件下没有匹配项（共 ${rows.length} 个工作负载）`" :image-size="50">
        <el-button v-if="onlyBad" link type="primary" @click="onlyBad = false">显示全部</el-button>
      </el-empty>
    </el-card>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Search } from '@element-plus/icons-vue'
import { listK8sClusters, listK8sWorkloads, listK8sNamespaces, listK8sChanges, listK8sPods } from '../api/cmdb'
import { pickDefaultCluster } from '../composables/useClusterPick'
import { useLoadState } from '../composables/useLoadState'
import { normalizeError } from '../api/http'
import LoadError from '../components/LoadError.vue'
import { usePager } from '../composables/usePager'
import Pager from '../components/Pager.vue'

const kinds = ['Deployment', 'StatefulSet', 'DaemonSet', 'CronJob']
const clusters = ref([]); const namespaces = ref([]); const rows = ref([]); const onlyBad = ref(false)
const display = computed(() => onlyBad.value ? rows.value.filter(r => r.status === 'degraded') : rows.value)
const badCount = computed(() => rows.value.filter(r => r.status === 'degraded').length)
const { page, pageSize, paged } = usePager(display)
const clusterId = ref(null); const ns = ref(''); const kind = ref(''); const q = ref('')
const { loading, error, run } = useLoadState()
const chDlg = ref(false); const chWl = ref(null); const changes = ref([]); const chLoading = ref(false)
const podDlg = ref(false); const podWl = ref(null); const wlPods = ref([]); const podLoading = ref(false)
async function openPods(row) {
  podWl.value = row; podDlg.value = true; wlPods.value = []; podLoading.value = true
  try { wlPods.value = await listK8sPods({ cluster_id: clusterId.value, namespace: row.namespace, workload: row.name }) }
  catch (e) { /* 静默 */ } finally { podLoading.value = false }
}

function statusText(s) { return { healthy: '正常', degraded: '副本不足', 'scaled-0': '已缩容' }[s] || s }

async function openChanges(row) {
  chWl.value = row; chDlg.value = true; chLoading.value = true; changes.value = []
  try {
    changes.value = await listK8sChanges({ cluster_id: row.cluster_id, namespace: row.namespace, kind: row.kind, name: row.name })
  } catch (e) { /* ignore */ } finally { chLoading.value = false }
}

async function onCluster() { ns.value = ''; namespaces.value = await listK8sNamespaces({ cluster_id: clusterId.value }); load() }

async function load() {
  if (!clusterId.value) return
  // 失败落到页面 error（红条 + 计数变 —），不是一闪而过的 toast：
  // toast 消失后表格照常显示「无数据，先去集群管理点同步」，把接口故障说成了"这个集群是空的"。
  const r = await run(() => {
    const p = { cluster_id: clusterId.value }
    if (ns.value) p.namespace = ns.value
    if (kind.value) p.kind = kind.value
    if (q.value) p.q = q.value
    return listK8sWorkloads(p)
  })
  rows.value = error.value ? [] : (r || [])
}

onMounted(async () => {
  try {
    clusters.value = await listK8sClusters()
    if (clusters.value.length) { clusterId.value = pickDefaultCluster(clusters.value); onCluster() }
  } catch (e) { ElMessage.error('加载集群失败') }
})
</script>

<style scoped>
.page-head { margin-bottom: 14px; }
.page-title { font-size: 18px; font-weight: 600; }
.muted { color: #909399; font-size: 12px; }
.bad { color: #f56c6c; font-weight: 600; }
.bar { display: flex; gap: 10px; align-items: center; margin-bottom: 12px; }
</style>
