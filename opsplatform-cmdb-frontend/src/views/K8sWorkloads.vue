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
        <span class="muted" style="margin-left:auto">共 {{ rows.length }}</span>
      </div>
      <el-table :data="rows" size="small" v-loading="loading">
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
        <el-table-column label="操作" width="90" fixed="right"><template #default="{ row }">
          <el-button link type="primary" size="small" @click="openChanges(row)">变更</el-button>
        </template></el-table-column>
      </el-table>

      <el-dialog :close-on-click-modal="false" v-model="chDlg" :title="`变更记录 · ${chWl?.namespace}/${chWl?.name}`" width="640px">
        <el-table :data="changes" size="small" v-loading="chLoading" max-height="420">
          <el-table-column prop="changed_at" label="时间" width="160" />
          <el-table-column prop="field" label="字段" width="90"><template #default="{ row }">{{ row.field==='image'?'镜像':'副本' }}</template></el-table-column>
          <el-table-column label="变化" min-width="280"><template #default="{ row }">
            <span class="muted">{{ row.old_value }}</span> → <b>{{ row.new_value }}</b>
          </template></el-table-column>
        </el-table>
        <el-empty v-if="!chLoading && !changes.length" description="暂无变更（首次纳管后、镜像/副本变化才会记录）" />
      </el-dialog>
      <el-empty v-if="!loading && !rows.length" description="无数据，先去集群管理点「同步」" />
    </el-card>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Search } from '@element-plus/icons-vue'
import { listK8sClusters, listK8sWorkloads, listK8sNamespaces, listK8sChanges } from '../api/cmdb'

const kinds = ['Deployment', 'StatefulSet', 'DaemonSet', 'CronJob']
const clusters = ref([]); const namespaces = ref([]); const rows = ref([])
const clusterId = ref(null); const ns = ref(''); const kind = ref(''); const q = ref(''); const loading = ref(false)
const chDlg = ref(false); const chWl = ref(null); const changes = ref([]); const chLoading = ref(false)

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
  loading.value = true
  try {
    const p = { cluster_id: clusterId.value }
    if (ns.value) p.namespace = ns.value
    if (kind.value) p.kind = kind.value
    if (q.value) p.q = q.value
    rows.value = await listK8sWorkloads(p)
  } catch (e) { ElMessage.error('加载失败') } finally { loading.value = false }
}

onMounted(async () => {
  try {
    clusters.value = await listK8sClusters()
    if (clusters.value.length) { clusterId.value = clusters.value[0].id; onCluster() }
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
