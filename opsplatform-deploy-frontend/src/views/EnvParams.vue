<template>
  <div class="ep-page">
    <div class="page-head">
      <div>
        <h2>项目参数</h2>
        <p class="sub">一个模板跨项目复用；这里配每个项目环境的专属值，新增模块时按目标环境自动带出</p>
      </div>
    </div>

    <el-table :data="envs" border stripe v-loading="loading">
      <el-table-column label="项目环境" width="200">
        <template #default="{ row }">
          <span class="env-name">{{ row.name }}</span>
          <el-tag size="small" :type="row.env_type === 'prod' ? 'danger' : 'success'" style="margin-left:8px">{{ (row.env_type || '').toUpperCase() }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="ingress 网关名" min-width="320">
        <template #default="{ row }">
          <code v-if="row.ingress_gateway">{{ row.ingress_gateway }}</code>
          <span v-else class="unset">未配置</span>
        </template>
      </el-table-column>
      <el-table-column label="Harbor 项目" min-width="160">
        <template #default="{ row }">
          <code v-if="row.harbor_project">{{ row.harbor_project }}</code>
          <span v-else class="unset">未配置（默认用 {{ projName(row) }}）</span>
        </template>
      </el-table-column>
      <el-table-column label="域名后缀" min-width="170">
        <template #default="{ row }">
          <code v-if="row.domain_suffix">{{ row.domain_suffix }}</code>
          <span v-else class="unset">未配置（域名留空）</span>
        </template>
      </el-table-column>
      <el-table-column label="namespace（可配多个）" min-width="200">
        <template #default="{ row }">
          <span v-if="nsList(row).length">
            <el-tag v-for="(n, i) in nsList(row)" :key="n" size="small" :type="i === 0 ? 'primary' : 'info'" style="margin:1px 3px 1px 0">{{ n }}</el-tag>
          </span>
          <span v-else class="unset">未配置（默认用 {{ row.name }}）</span>
        </template>
      </el-table-column>
      <el-table-column label="z-kv-secrets 路径" min-width="220">
        <template #default="{ row }">
          <code v-if="row.zkv_secrets_path">{{ row.zkv_secrets_path }}</code>
          <span v-else class="unset">自动推（{{ autoZkv(row) }}）</span>
        </template>
      </el-table-column>
      <el-table-column label="操作" width="100">
        <template #default="{ row }">
          <el-button link type="primary" @click="openEdit(row)">编辑</el-button>
        </template>
      </el-table-column>
    </el-table>
    <div class="tip">以后要加"域名后缀"等按项目注入的参数，就在这张表加一列。</div>

    <el-dialog v-model="dialog" :title="`编辑项目参数 · ${editing?.name || ''}`" width="600px" :close-on-click-modal="false">
      <el-form label-width="110px">
        <el-form-item label="ingress 网关名">
          <el-input v-model="gateway" placeholder="如 istio-system/g66-uat-istio-ingressgateway-extra" />
          <div class="form-hint">新增模块部署到 {{ editing?.name }} 时，网关名自动带出这个值（仍可手改）</div>
        </el-form-item>
        <el-form-item label="Harbor 项目">
          <el-input v-model="harbor" :placeholder="`留空=用项目名 ${projName(editing)}`" />
          <div class="form-hint">镜像仓库 = 全局 Harbor 域名 / 这里的项目 / 服务名；留空自动用项目名，预填后仍可手改</div>
        </el-form-item>
        <el-form-item label="域名后缀">
          <el-input v-model="domain" placeholder="如 uat.slileisure.com" />
          <div class="form-hint">前端模块(-frontend)访问域名自动带出 = 模块名去 -frontend + . + 这里的后缀；留空=域名不自动带，预填后仍可手改</div>
        </el-form-item>
        <el-form-item label="namespace 列表">
          <el-input v-model="namespaces" type="textarea" :rows="4" :placeholder="`一行一个（可配多个），第一个作默认。留空=用环境名 ${editing?.name}`" />
          <div class="form-hint">新增模块时 namespace 自动填第一个，可下拉从这里选、也可手输列表外的</div>
        </el-form-item>
        <el-form-item label="z-kv-secrets 路径">
          <el-input v-model="zkvPath" :placeholder="`留空=自动推 ${autoZkv(editing)}`" />
          <div class="form-hint">后端 secret 集中定义处（专属 secret 追加到这）。默认自动推 &lt;chart目录&gt;/z-kv-secrets；历史遗留共用的（如 g33 复用 g32）填 charts/g32-uat/z-kv-secrets</div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialog = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="save">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import * as api from '../api'

const envs = ref([])
const loading = ref(false)
const dialog = ref(false)
const saving = ref(false)
const editing = ref(null)
const gateway = ref('')
const harbor = ref('')
const domain = ref('')
const namespaces = ref('')
const zkvPath = ref('')

// 把配置的 namespace 原文拆成数组（换行/逗号/空格分隔），供展示
function nsList(row) {
  return (row?.default_namespaces || '').split(/[\n,\s]+/).map(s => s.trim()).filter(Boolean)
}

// z-kv-secrets 自动推导默认路径 = <chart_base_path>/z-kv-secrets（跟后端一致，展示用）
function autoZkv(row) {
  const base = (row?.chart_base_path || '').replace(/\/+$/, '')
  return base ? `${base}/z-kv-secrets` : 'charts/<环境>/z-kv-secrets'
}

// 项目名 = 环境名去掉 -env 后缀（跟后端一致），用于 Harbor 项目默认值提示
function projName(row) {
  if (!row?.name) return ''
  const t = row.env_type ? `-${row.env_type}` : ''
  return t && row.name.endsWith(t) ? row.name.slice(0, -t.length) : row.name
}

function openEdit(row) {
  editing.value = row
  gateway.value = row.ingress_gateway || ''
  harbor.value = row.harbor_project || ''
  domain.value = row.domain_suffix || ''
  namespaces.value = row.default_namespaces || ''
  zkvPath.value = row.zkv_secrets_path || ''
  dialog.value = true
}

async function save() {
  saving.value = true
  try {
    await api.updateEnvGateway(editing.value.id, gateway.value.trim())
    await api.updateEnvHarbor(editing.value.id, harbor.value.trim())
    await api.updateEnvDomain(editing.value.id, domain.value.trim())
    await api.updateEnvNamespaces(editing.value.id, namespaces.value.trim())
    await api.updateEnvZkvPath(editing.value.id, zkvPath.value.trim())
    editing.value.ingress_gateway = gateway.value.trim()
    editing.value.harbor_project = harbor.value.trim()
    editing.value.domain_suffix = domain.value.trim()
    editing.value.default_namespaces = namespaces.value.trim()
    editing.value.zkv_secrets_path = zkvPath.value.trim()
    dialog.value = false
    ElMessage.success('已保存')
  } finally {
    saving.value = false
  }
}

async function load() {
  loading.value = true
  try {
    // 只列 K8s 项目环境（VM 没有 ingress 网关）
    envs.value = ((await api.listProjectEnvs()) || []).filter(e => e.git_repo !== undefined)
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<style scoped>
.ep-page { padding: 16px 20px; }
.page-head h2 { margin: 0 0 4px; font-size: 18px; }
.sub { color: #909399; font-size: 13px; margin: 0 0 16px; }
.env-name { font-weight: 600; }
.unset { color: #c0c4cc; }
.tip { margin-top: 10px; color: #909399; font-size: 12px; }
.form-hint { color: #909399; font-size: 12px; margin-top: 4px; }
</style>
