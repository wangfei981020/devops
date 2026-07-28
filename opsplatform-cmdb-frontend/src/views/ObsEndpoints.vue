<template>
  <div :class="{ page: !embedded }">
    <div v-if="!embedded" class="page-head">
      <span class="page-title">数据源接入</span>
      <span class="muted" style="margin-left:10px">Prometheus/VM · Loki · KubeSphere 只读地址，支持多条（按环境/集群区分）。本地不存历史，实时查这些源</span>
      <el-button type="primary" size="small" style="float:right" @click="openAdd">+ 添加数据源</el-button>
    </div>
    <!-- 嵌进「接入管理」的 tab 时不重复显示页标题，但按钮和说明要留着 -->
    <div v-else style="margin-bottom:10px">
      <el-button type="primary" size="small" @click="openAdd">+ 添加数据源</el-button>
      <span class="muted" style="margin-left:10px">Prometheus/VM · Loki · KubeSphere 只读地址，支持多条（按环境/集群区分）。本地不存历史，实时查这些源</span>
    </div>
    <el-card shadow="never" :body-style="embedded ? { padding: '0' } : {}">
      <el-table :data="paged" size="small" v-loading="loading">
        <el-table-column prop="name" label="名称" min-width="150" />
        <el-table-column prop="type" label="类型" width="130"><template #default="{row}">
          <el-tag size="small">{{ typeText(row.type) }}</el-tag>
        </template></el-table-column>
        <el-table-column prop="url" label="地址" min-width="280" />
        <el-table-column label="适用" width="140"><template #default="{row}">
          {{ row.env || '通用' }}<span v-if="row.cluster_id"> / 集群{{ row.cluster_id }}</span>
        </template></el-table-column>
        <el-table-column label="多集群" width="110"><template #default="{row}">
          <el-tag v-if="row.cluster_label" size="small" type="warning">{{ row.cluster_label }}</el-tag>
          <span v-else class="muted">单集群</span>
        </template></el-table-column>
        <el-table-column label="Token" width="80"><template #default="{row}">
          <el-tag size="small" :type="row.has_token?'success':'info'">{{ row.has_token?'已配':'无' }}</el-tag>
        </template></el-table-column>
        <el-table-column label="状态" width="80"><template #default="{row}">
          <el-tag size="small" :type="row.enabled?'success':'info'">{{ row.enabled?'启用':'停用' }}</el-tag>
        </template></el-table-column>
        <el-table-column label="操作" width="200" fixed="right"><template #default="{row}">
          <el-button link type="primary" size="small" :loading="testing[row.id]" @click="doTest(row)">测连通</el-button>
          <el-button link type="primary" size="small" @click="openEdit(row)">编辑</el-button>
          <el-button link type="danger" size="small" @click="del(row)">删除</el-button>
        </template></el-table-column>
      </el-table>
      <Pager :total="rows.length" v-model:page="page" v-model:page-size="pageSize" />
      <el-empty v-if="!loading && !rows.length" description="还没配数据源，点右上添加" />
    </el-card>

    <el-dialog :close-on-click-modal="false" v-model="dlg" :title="editing?'编辑数据源':'添加数据源'" width="560px">
      <el-form :model="form" label-width="100px">
        <el-form-item label="名称"><el-input v-model="form.name" placeholder="如 prod-vmselect" /></el-form-item>
        <el-form-item label="类型">
          <el-select v-model="form.type" style="width:240px">
            <el-option label="Prometheus / VictoriaMetrics" value="prometheus" />
            <el-option label="Loki" value="loki" />
            <el-option label="KubeSphere" value="kubesphere" />
          </el-select>
        </el-form-item>
        <el-form-item label="地址"><el-input v-model="form.url" placeholder="如 http://vmselect.monitoring:8481/select/0/prometheus" /></el-form-item>
        <el-form-item label="适用环境">
          <el-select v-model="form.env" clearable placeholder="通用（不限环境）" style="width:200px">
            <el-option v-for="e in envs" :key="e" :label="e" :value="e" />
          </el-select>
        </el-form-item>
        <el-form-item label="适用集群">
          <el-select v-model="form.cluster_id" clearable placeholder="通用（不限集群）" style="width:240px">
            <el-option v-for="c in clusters" :key="c.id" :label="c.display_name||c.name" :value="c.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="集群隔离标签">
          <el-input v-model="form.cluster_label" placeholder="留空=该源只有一个集群的数据" style="width:240px" />
          <div class="muted" style="font-size:12px;line-height:1.6;margin-top:4px">
            数据源同时采多个集群时必填，填区分集群的标签名（通常是 <code>cluster</code>）。<br>
            查询会自动加上 <code>标签名="集群名"</code>，否则会把其它集群的同名 Pod 一起算进来。<br>
            只采单个集群的数据源留空。
          </div>
        </el-form-item>
        <el-form-item label="Token">
          <el-input v-model="form.token" type="password" show-password :placeholder="editing?'留空=保留原值':'可选，Bearer token'" />
        </el-form-item>
        <el-form-item label="启用"><el-switch v-model="form.enabled" :active-value="1" :inactive-value="0" /></el-form-item>
      </el-form>
      <template #footer><el-button @click="dlg=false">取消</el-button><el-button type="primary" @click="save">保存</el-button></template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { listObsEndpoints, createObsEndpoint, updateObsEndpoint, deleteObsEndpoint, testObsEndpoint, listK8sClusters } from '../api/cmdb'
import { useAppStore } from '../stores/app'
import { usePager } from '../composables/usePager'
import Pager from '../components/Pager.vue'

// embedded=true 时作为「接入管理」的一个 tab 渲染：不显示页级标题，去掉外层留白
defineProps({ embedded: { type: Boolean, default: false } })

const app = useAppStore()
const envs = ['PROD', 'UAT', 'TEST', 'DEV']
const rows = ref([]); const clusters = ref([]); const loading = ref(false); const testing = reactive({})
const { page, pageSize, paged } = usePager(rows)
const dlg = ref(false); const editing = ref(false)
const blank = () => ({ id: 0, name: '', type: 'prometheus', url: '', env: '', cluster_id: null, cluster_label: '', token: '', enabled: 1 })
const form = reactive(blank())

function typeText(t) { return { prometheus: 'Prometheus/VM', loki: 'Loki', kubesphere: 'KubeSphere' }[t] || t }

async function load() {
  loading.value = true
  try { rows.value = await listObsEndpoints(); clusters.value = await listK8sClusters() }
  catch (e) { ElMessage.error('加载失败') } finally { loading.value = false }
}
function openAdd() { editing.value = false; Object.assign(form, blank()); dlg.value = true }
function openEdit(row) { editing.value = true; Object.assign(form, { ...blank(), ...row, cluster_id: row.cluster_id || null, token: '' }); dlg.value = true }
async function save() {
  if (!form.name || !form.url) { ElMessage.warning('名称/地址必填'); return }
  try {
    const body = { ...form, cluster_id: form.cluster_id || 0 }
    if (editing.value) await updateObsEndpoint(form.id, body); else await createObsEndpoint(body)
    ElMessage.success('已保存'); dlg.value = false; load()
  } catch (e) { ElMessage.error(e.response?.data?.error || '保存失败') }
}
async function del(row) {
  try { await app.showConfirm(`删除数据源「${row.name}」？`); await deleteObsEndpoint(row.id); ElMessage.success('已删除'); load() }
  catch (e) { if (e !== 'cancel') ElMessage.error('失败') }
}
async function doTest(row) {
  testing[row.id] = true
  try {
    const r = await testObsEndpoint(row.id)
    if (r.ok) {
      ElMessage.success(`连通成功（HTTP ${r.status} ${r.path || ''}）`)
      return
    }
    // 失败时把每条探测路径的结果都列出来——只说"HTTP 404"看不出是地址错了还是服务没起来。
    const detail = (r.tried || []).map(t => `${t.path} → ${t.status || t.error}`).join('\n')
    ElMessage({
      type: 'error', duration: 8000, showClose: true,
      message: (r.error || '连通失败') + (detail ? `\n探测明细：\n${detail}` : ''),
      customClass: 'obs-test-msg',
    })
  } catch (e) { ElMessage.error('测试失败') } finally { testing[row.id] = false }
}
onMounted(load)
</script>

<style scoped>
.page-head { margin-bottom: 14px; }
.page-title { font-size: 18px; font-weight: 600; }
.muted { color: #909399; font-size: 12px; }
</style>

<!-- ElMessage 挂在 body 上，scoped 管不到，探测明细要靠这个才能换行显示 -->
<style>
.obs-test-msg { max-width: 620px; }
.obs-test-msg .el-message__content { white-space: pre-line; line-height: 1.7; }
</style>
