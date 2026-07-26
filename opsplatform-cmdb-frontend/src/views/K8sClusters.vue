<template>
  <div class="page">
    <div class="page-head">
      <span class="page-title">K8s 集群管理</span>
      <span class="muted" style="margin-left:10px">多集群只读纳管（GKE / IDC 自管）· 只连 apiserver、不连节点</span>
      <el-button type="primary" size="small" style="float:right" @click="openAdd">+ 添加集群</el-button>
      <el-button size="small" style="float:right;margin-right:8px" @click="openDiscover">从云账号发现 GKE</el-button>
    </div>

    <el-card shadow="never">
      <el-table :data="clusters" size="small" v-loading="loading">
        <el-table-column label="集群" min-width="180">
          <template #default="{ row }">
            <b>{{ row.display_name || row.name }}</b>
            <div class="muted" v-if="row.display_name">{{ row.name }}</div>
          </template>
        </el-table-column>
        <el-table-column prop="environment" label="环境" width="90" />
        <el-table-column prop="provider" label="接入" width="100" />
        <el-table-column label="apiserver" min-width="180"><template #default="{ row }">{{ row.endpoint || '—' }}</template></el-table-column>
        <el-table-column label="云账号/项目" min-width="140"><template #default="{ row }">
          <span v-if="row.cloud_account">{{ row.cloud_account }}</span>
          <span v-else-if="row.project_id">{{ row.project_id }}</span>
          <span v-else>—</span>
        </template></el-table-column>
        <el-table-column label="凭据" width="90"><template #default="{ row }">
          <el-tag size="small" :type="row.has_kubeconfig || row.provider==='in-cluster' ? 'success' : 'info'">
            {{ row.provider==='in-cluster' ? '本集群' : (row.has_kubeconfig ? '已配' : '未配') }}
          </el-tag>
        </template></el-table-column>
        <el-table-column label="状态" width="80"><template #default="{ row }">
          <el-tag size="small" :type="row.enabled ? 'success' : 'info'">{{ row.enabled ? '启用' : '停用' }}</el-tag>
        </template></el-table-column>
        <el-table-column label="操作" width="200" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" size="small" :loading="testing[row.id]" @click="doTest(row)">测连通</el-button>
            <el-button link type="success" size="small" :loading="syncing[row.id]" @click="doSync(row)">同步</el-button>
            <el-button link type="primary" size="small" @click="openEdit(row)">编辑</el-button>
            <el-button link type="danger" size="small" @click="delCluster(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
      <el-empty v-if="!loading && !clusters.length" description="还没有纳管集群，点右上「添加集群」" />
    </el-card>

    <!-- 添加/编辑弹窗 -->
    <el-dialog :close-on-click-modal="false" :close-on-press-escape="false" v-model="dlg"
      :title="editing ? '编辑集群' : '添加集群'" width="620px">
      <el-form :model="form" label-width="110px">
        <el-form-item label="集群名">
          <el-input v-model="form.name" placeholder="GKE 集群名或自定义标识" />
        </el-form-item>
        <el-form-item label="展示名">
          <el-input v-model="form.display_name" placeholder="可选，列表展示用" />
        </el-form-item>
        <el-form-item label="环境">
          <el-select v-model="form.environment" style="width:160px">
            <el-option v-for="e in envs" :key="e" :label="e" :value="e" />
          </el-select>
        </el-form-item>
        <el-form-item label="接入方式">
          <el-select v-model="form.provider" style="width:200px">
            <el-option label="gke（GKE 托管）" value="gke" />
            <el-option label="generic（IDC 自管 / 通用）" value="generic" />
            <el-option label="in-cluster（本集群 SA）" value="in-cluster" />
          </el-select>
          <span class="muted" style="margin-left:8px">in-cluster 用本 Pod 只读 SA，无需 kubeconfig</span>
        </el-form-item>
        <el-form-item label="云账号" v-if="form.provider==='gke'">
          <el-select v-model="form.cloud_account_id" style="width:280px" clearable placeholder="选主机模块已配的 GCP 云账号（复用凭据）">
            <el-option v-for="a in cloudAccounts" :key="a.id" :label="a.name" :value="a.id" />
          </el-select>
          <span class="muted" style="margin-left:8px">GCP SA key 在 <b>主机 → 云账号</b> 配一次即可，这里选用；没有就先去那配</span>
        </el-form-item>
        <el-form-item label="云项目" v-if="form.provider==='gke'">
          <el-input v-model="form.project_id" placeholder="GCP project id（可选，接主机/成本）" />
        </el-form-item>
        <el-form-item label="region/zone">
          <el-input v-model="form.location" placeholder="可选" style="width:260px" />
        </el-form-item>
        <el-form-item label="apiserver">
          <el-input v-model="form.endpoint" placeholder="可选，展示用" />
        </el-form-item>
        <el-form-item label="节点池标签" v-if="form.provider!=='in-cluster'">
          <el-input v-model="form.nodepool_label" placeholder="GKE 默认 cloud.google.com/gke-nodepool；IDC 填自定义 label" style="width:360px" />
          <span class="muted" style="margin-left:8px">留空=按角色/default 兜底分组</span>
        </el-form-item>
        <el-form-item label="计费模式">
          <el-select v-model="form.cost_mode" clearable placeholder="自动（按接入方式推断）" style="width:260px">
            <el-option label="cloud（真实云支出）" value="cloud" />
            <el-option label="idc（迁云估算，IT管实际）" value="idc" />
            <el-option label="none（不计费，如本地）" value="none" />
          </el-select>
          <span class="muted" style="margin-left:8px">空=自动：gke→cloud / in-cluster→none / generic→idc</span>
        </el-form-item>
        <el-form-item label="kubeconfig" v-if="form.provider!=='in-cluster'">
          <el-input v-model="form.kubeconfig" type="textarea" :rows="6"
            :placeholder="editing ? '留空=保留原值；填了=覆盖' : '粘贴只读 view SA 的 token 版 kubeconfig（AES 加密存，不回显）'" />
        </el-form-item>
        <el-form-item label="启用">
          <el-switch v-model="form.enabled" :active-value="1" :inactive-value="0" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dlg=false">取消</el-button>
        <el-button type="primary" @click="save">保存</el-button>
      </template>
    </el-dialog>

    <!-- 从云账号发现 GKE -->
    <el-dialog :close-on-click-modal="false" v-model="disDlg" title="从云账号发现 GKE 集群" width="720px">
      <div class="bar" style="display:flex;gap:10px;align-items:center;margin-bottom:12px">
        <el-select v-model="disAcct" placeholder="云账号" style="width:180px" @change="disProj=''">
          <el-option v-for="a in cloudAccounts" :key="a.id" :label="a.name" :value="a.id" />
        </el-select>
        <el-select v-model="disProj" placeholder="GCP 项目" style="width:220px" filterable>
          <el-option v-for="p in disProjects" :key="p.project_id" :label="(p.name?p.name+' · ':'')+p.project_id" :value="p.project_id" />
        </el-select>
        <el-button type="primary" :loading="disLoading" @click="doDiscover">发现集群</el-button>
        <span class="muted" style="margin-left:auto">用该项目 SA key 调 GKE API（公网，不受集群授权网络限制）</span>
      </div>
      <el-table :data="disList" size="small" @selection-change="s=>disSel=s" max-height="360">
        <el-table-column type="selection" width="44" />
        <el-table-column prop="name" label="集群" min-width="160" />
        <el-table-column prop="location" label="区域" width="130" />
        <el-table-column prop="version" label="版本" width="120" />
        <el-table-column prop="node_count" label="节点" width="70" />
        <el-table-column prop="status" label="状态" width="100" />
      </el-table>
      <el-empty v-if="!disLoading && disList.length===0 && disTried" description="没发现集群（或 SA key 无权限/项目无 GKE）" :image-size="60" />
      <template #footer>
        <el-button @click="disDlg=false">取消</el-button>
        <el-button type="primary" :disabled="!disSel.length" @click="importSel">导入选中 ({{ disSel.length }})</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { computed } from 'vue'
import { listK8sClusters, createK8sCluster, updateK8sCluster, deleteK8sCluster, testK8sCluster, syncK8sCluster, listCloudAccounts, discoverGKE } from '../api/cmdb'
import { useAppStore } from '../stores/app'

const app = useAppStore()
const envs = ['PROD', 'UAT', 'TEST', 'DEV']
const clusters = ref([])
const cloudAccounts = ref([])
const loading = ref(false)
const testing = reactive({})
const syncing = reactive({})
const dlg = ref(false)
const editing = ref(false)
const blank = () => ({ id: 0, name: '', display_name: '', environment: 'DEV', provider: 'gke', project_id: '', cloud_account_id: null, location: '', endpoint: '', nodepool_label: '', cost_mode: '', kubeconfig: '', enabled: 1 })
const form = reactive(blank())

async function load() {
  loading.value = true
  try {
    clusters.value = await listK8sClusters()
    try { cloudAccounts.value = await listCloudAccounts() } catch (e) { /* 无云账号不阻塞 */ }
  } catch (e) { ElMessage.error('加载失败') } finally { loading.value = false }
}

function openAdd() {
  editing.value = false
  Object.assign(form, blank())
  dlg.value = true
}
function openEdit(row) {
  editing.value = true
  Object.assign(form, { ...blank(), ...row, cloud_account_id: row.cloud_account_id || null, kubeconfig: '' }) // 凭据不回显，留空=保留
  dlg.value = true
}

async function save() {
  if (!form.name) { ElMessage.warning('集群名必填'); return }
  try {
    if (editing.value) await updateK8sCluster(form.id, form)
    else await createK8sCluster(form)
    ElMessage.success('已保存'); dlg.value = false; load()
  } catch (e) { ElMessage.error(e.response?.data?.error || '保存失败') }
}

async function delCluster(row) {
  try {
    await app.showConfirm(`删除集群「${row.display_name || row.name}」？仅从 CMDB 移除纳管，不影响集群本身。`)
    await deleteK8sCluster(row.id); ElMessage.success('已删除'); load()
  } catch (e) { if (e !== 'cancel') ElMessage.error('删除失败') }
}

async function doTest(row) {
  testing[row.id] = true
  try {
    const r = await testK8sCluster(row.id)
    if (r.ok) ElMessage.success(`连通成功 · 版本 ${r.version} · 节点 ${r.nodes} 个`)
    else ElMessage.error('连通失败：' + (r.error || '未知'))
  } catch (e) { ElMessage.error(e.response?.data?.error || '测试失败') } finally { testing[row.id] = false }
}

async function doSync(row) {
  syncing[row.id] = true
  try {
    const r = await syncK8sCluster(row.id)
    const s = r.summary || {}
    if (r.ok) ElMessage.success(`同步完成 · 节点${s.nodes ?? 0} 工作负载${s.workloads ?? 0} Pod${s.pods ?? 0} Service${s.services ?? 0} Ingress${s.ingresses ?? 0}`)
    else ElMessage.warning('同步部分失败，详见执行结果：' + JSON.stringify(s))
  } catch (e) { ElMessage.error(e.response?.data?.error || '同步失败') } finally { syncing[row.id] = false }
}

// ── 从云账号发现 GKE ──
const disDlg = ref(false); const disAcct = ref(null); const disProj = ref('')
const disList = ref([]); const disSel = ref([]); const disLoading = ref(false); const disTried = ref(false)
const disProjects = computed(() => (cloudAccounts.value.find(a => a.id === disAcct.value)?.projects) || [])
function openDiscover() { disDlg.value = true; disList.value = []; disSel.value = []; disTried.value = false }
async function doDiscover() {
  if (!disAcct.value || !disProj.value) { ElMessage.warning('选云账号和项目'); return }
  disLoading.value = true; disTried.value = true; disList.value = []
  try {
    const r = await discoverGKE({ cloud_account_id: disAcct.value, project_id: disProj.value })
    if (r.ok) disList.value = r.clusters || []
    else ElMessage.error('发现失败：' + (r.error || ''))
  } catch (e) { ElMessage.error('发现失败') } finally { disLoading.value = false }
}
async function importSel() {
  let n = 0
  for (const c of disSel.value) {
    try {
      await createK8sCluster({ name: c.name, display_name: c.name, environment: 'UAT', provider: 'gke',
        cloud_account_id: disAcct.value, project_id: disProj.value, location: c.location, endpoint: c.endpoint,
        ca_data: c.ca, cost_mode: 'cloud', enabled: 1 })
      n++
    } catch (e) { /* 跳过重复 */ }
  }
  ElMessage.success(`已导入 ${n} 个集群`); disDlg.value = false; load()
}

onMounted(load)
</script>

<style scoped>
.page-head { margin-bottom: 14px; }
.page-title { font-size: 18px; font-weight: 600; }
.muted { color: #909399; font-size: 12px; }
</style>
