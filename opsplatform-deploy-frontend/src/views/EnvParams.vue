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
      <el-table-column label="主域名（可配多个）" min-width="200">
        <template #default="{ row }">
          <span v-if="domList(row).length">
            <el-tag v-for="(d, i) in domList(row)" :key="d" size="small" :type="i === 0 ? 'primary' : 'info'" style="margin:1px 3px 1px 0">{{ d }}</el-tag>
          </span>
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
      <el-table-column label="固定艾特人" min-width="160">
        <template #default="{ row }">
          <span v-if="atNames(row).length">
            <el-tag v-for="n in atNames(row)" :key="n" size="small" type="warning" style="margin:1px 3px 1px 0">{{ n }}</el-tag>
          </span>
          <span v-else class="unset">未配置</span>
        </template>
      </el-table-column>
      <el-table-column label="z-kv-secrets" min-width="280">
        <template #default="{ row }">
          <div>
            <code v-if="row.zkv_secrets_path">{{ row.zkv_secrets_path }}</code>
            <span v-else class="unset">自动推（{{ autoZkv(row) }}）</span>
          </div>
          <div style="margin-top:4px">
            <el-button link size="small" @click="checkZkv(row)">{{ zkvState[row.id]?.loading ? '检测中…' : '检测' }}</el-button>
            <span v-if="zkvState[row.id]?.checked">
              <el-tag v-if="zkvState[row.id].exists" size="small" type="success">✓ 存在</el-tag>
              <template v-else>
                <el-tag size="small" type="danger">⚠ 不存在</el-tag>
                <el-button link type="primary" size="small" @click="openInit(row)">初始化</el-button>
              </template>
            </span>
          </div>
        </template>
      </el-table-column>
      <el-table-column label="操作" width="100" fixed="right">
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
        <el-form-item label="主域名">
          <el-input v-model="domain" type="textarea" :rows="3" placeholder="一行一个（可配多个），如 uat.slileisure.com / dragontiger-game.com" />
          <div class="form-hint">非生产：访问域名 = 模块名去 -frontend + . + 第一个主域名（自动带）。生产：新增时按数量把域名平均分配到这些主域名生成占位（xxx1.主域名…）再手改。留空=域名不自动带。</div>
        </el-form-item>
        <el-form-item label="namespace 列表">
          <el-input v-model="namespaces" type="textarea" :rows="4" :placeholder="`一行一个（可配多个），第一个作默认。留空=用环境名 ${editing?.name}`" />
          <div class="form-hint">新增模块时 namespace 自动填第一个，可下拉从这里选、也可手输列表外的</div>
        </el-form-item>
        <el-form-item label="固定艾特人">
          <el-select v-model="atLarks" multiple filterable placeholder="选通知人（可多个）" style="width:100%">
            <el-option v-for="c in contactsWithLark" :key="c.lark_id" :label="`${c.name}（${c.lark_id}）`" :value="c.lark_id" />
          </el-select>
          <div class="form-hint">新增模块到 {{ editing?.name }} 时，Lark 自动艾特这些人（另加操作人、新增时临时选的）。选项来自「系统设置→通知人」</div>
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

    <!-- 初始化 z-kv-secrets（新项目从模板库 zkv 模板复制一份）-->
    <el-dialog v-model="initDlg" :title="`初始化 z-kv-secrets · ${initRow?.name || ''}`" width="820px" :close-on-click-modal="false" top="5vh">
      <el-form label-width="90px">
        <el-form-item label="参照模板" required>
          <el-select v-model="initTplId" placeholder="选 z-kv-secrets 类型的模板" filterable style="width:420px">
            <el-option v-for="t in zkvTemplates" :key="t.id" :label="`${t.name}（源 ${t.src_env}）`" :value="t.id" />
          </el-select>
          <el-button style="margin-left:10px" :loading="copying" :disabled="!initTplId" @click="doCopyZkv">复制</el-button>
          <span class="form-hint" style="margin-left:8px">目标 {{ initPath }}</span>
        </el-form-item>
        <el-form-item v-if="initYaml || initSecrets.length || initTidb.length" label="内容">
          <div style="width:100%">
            <div class="init-hint">⚠ 名字已改成本项目前缀；请改 <b>redis/nacos/rocketmq/tidb/uid/kafka/doris</b> 等的值为本项目实际值（dev/test 共库可不改）</div>
            <el-radio-group v-model="initMode" size="small" style="margin:6px 0">
              <el-radio-button label="form">表单</el-radio-button>
              <el-radio-button label="yaml">YAML</el-radio-button>
            </el-radio-group>
            <!-- 表单模式 -->
            <div v-if="initMode === 'form'" class="init-form">
              <div v-if="initTidbCommon" class="init-grp">
                <div class="init-gt">tidbCommon（共享连接）</div>
                <div v-for="(kv,i) in initTidbCommon" :key="i" class="init-row">
                  <span class="init-k">{{ kv.k }}</span><el-input v-model="kv.v" size="small" style="width:340px" />
                </div>
              </div>
              <div v-for="(s,si) in initSecrets" :key="'s'+si" class="init-grp">
                <div class="init-gt">{{ s.name }} <el-tag size="small">普通</el-tag> <span class="init-ns">ns: {{ s.namespace }}</span></div>
                <div v-for="(kv,i) in s.kv" :key="i" class="init-row">
                  <el-input v-model="kv.k" size="small" style="width:180px" placeholder="键" />
                  <el-input v-model="kv.v" size="small" style="width:340px;margin:0 6px" placeholder="值" />
                </div>
              </div>
              <div v-for="(s,si) in initTidb" :key="'t'+si" class="init-grp">
                <div class="init-gt">{{ s.name }} <el-tag size="small" type="primary">TiDB</el-tag> <span class="init-ns">ns: {{ s.namespace }}</span></div>
                <div class="init-row"><span class="init-k">database</span><el-input v-model="s.database" size="small" style="width:340px" /></div>
                <div v-for="(kv,i) in s.extra" :key="i" class="init-row">
                  <el-input v-model="kv.k" size="small" style="width:180px" placeholder="键" />
                  <el-input v-model="kv.v" size="small" style="width:340px;margin:0 6px" placeholder="值" />
                </div>
              </div>
            </div>
            <!-- YAML 模式 -->
            <CodeEditor v-else v-model="initYaml" />
          </div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="initDlg = false">取消</el-button>
        <el-button type="primary" :loading="submitting" :disabled="!initYaml" @click="submitInit">提交</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { parse, stringify } from 'yaml'
import CodeEditor from '../components/CodeEditor.vue'
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
const atLarks = ref([]) // 固定艾特人（lark_id 列表）
const contacts = ref([]) // 通知人列表
const contactsWithLark = computed(() => contacts.value.filter(c => c.lark_id))

// 该环境固定艾特人的展示名（lark_id → name）
function atNames(row) {
  const ids = (row?.at_lark_ids || '').split(/[\n,\s]+/).map(s => s.trim()).filter(Boolean)
  return ids.map(id => contacts.value.find(c => c.lark_id === id)?.name || id)
}

// 把配置的 namespace 原文拆成数组（换行/逗号/空格分隔），供展示
function nsList(row) {
  return (row?.default_namespaces || '').split(/[\n,\s]+/).map(s => s.trim()).filter(Boolean)
}
// 主域名列表（domain_suffix 现在存多个，换行/逗号/空格分隔）
function domList(row) {
  return (row?.domain_suffix || '').split(/[\n,\s]+/).map(s => s.trim()).filter(Boolean)
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
  atLarks.value = (row.at_lark_ids || '').split(/[\n,\s]+/).map(s => s.trim()).filter(Boolean)
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
    const atStr = atLarks.value.join('\n')
    await api.updateEnvAtLarks(editing.value.id, atStr)
    editing.value.ingress_gateway = gateway.value.trim()
    editing.value.harbor_project = harbor.value.trim()
    editing.value.domain_suffix = domain.value.trim()
    editing.value.default_namespaces = namespaces.value.trim()
    editing.value.zkv_secrets_path = zkvPath.value.trim()
    editing.value.at_lark_ids = atStr
    dialog.value = false
    ElMessage.success('已保存')
  } finally {
    saving.value = false
  }
}

// ---- z-kv-secrets 检测 + 初始化 ----
const zkvState = reactive({}) // { [envId]: {loading, checked, exists} }
async function checkZkv(row) {
  zkvState[row.id] = { loading: true, checked: false, exists: false }
  try {
    const r = await api.zkvStatus(row.id)
    zkvState[row.id] = { loading: false, checked: true, exists: !!r.exists }
  } catch {
    zkvState[row.id] = { loading: false, checked: true, exists: false }
  }
}

const initDlg = ref(false)
const initRow = ref(null)
const initTplId = ref(null)
const initPath = ref('')
const zkvTemplates = ref([])
const copying = ref(false)
const submitting = ref(false)
const initMode = ref('form')
const initYaml = ref('')
let initCopiedYaml = '' // 复制回来的原文（软提示：值没改就等于它）
const initSecrets = ref([])
const initTidb = ref([])
const initTidbCommon = ref(null)

async function openInit(row) {
  initRow.value = row
  initTplId.value = null
  initPath.value = row.zkv_secrets_path || autoZkv(row)
  initYaml.value = ''; initCopiedYaml = ''
  initSecrets.value = []; initTidb.value = []; initTidbCommon.value = null
  initMode.value = 'form'
  zkvTemplates.value = ((await api.listTemplates()) || []).filter(t => t.module_type === 'zkv')
  initDlg.value = true
}

async function doCopyZkv() {
  copying.value = true
  try {
    const r = await api.zkvPreview({ template_id: initTplId.value, target_env_id: initRow.value.id })
    if (r.exists) { ElMessage.warning('目标 z-kv-secrets 已存在，无需初始化'); return }
    initYaml.value = r.values_yaml || ''
    initCopiedYaml = initYaml.value
    yamlToForm(initYaml.value)
  } catch (e) {
    ElMessage.error(e?.response?.data?.message || '复制失败')
  } finally { copying.value = false }
}

// YAML → 表单（secrets/tidbSecrets/tidbCommon）
function yamlToForm(text) {
  let doc = {}
  try { doc = parse(text) || {} } catch { doc = {} }
  initTidbCommon.value = doc.tidbCommon ? Object.entries(doc.tidbCommon).map(([k, v]) => ({ k, v: String(v ?? '') })) : null
  initSecrets.value = (doc.secrets || []).map(s => ({
    name: s.name, namespace: s.namespace || '',
    kv: Object.entries(s.stringData || {}).map(([k, v]) => ({ k, v: String(v ?? '') })),
  }))
  initTidb.value = (doc.tidbSecrets || []).map(s => ({
    name: s.name, namespace: s.namespace || '', database: s.database || '',
    extra: Object.entries(s.extraStringData || {}).map(([k, v]) => ({ k, v: String(v ?? '') })),
  }))
}
// 表单 → YAML（提交前，若在表单模式）
function formToYaml() {
  let doc = {}
  try { doc = parse(initCopiedYaml) || {} } catch { doc = {} }
  if (initTidbCommon.value) doc.tidbCommon = Object.fromEntries(initTidbCommon.value.map(kv => [kv.k, kv.v]))
  doc.secrets = initSecrets.value.map(s => ({
    name: s.name, ...(s.namespace ? { namespace: s.namespace } : {}), type: 'Opaque',
    stringData: Object.fromEntries(s.kv.filter(kv => kv.k.trim()).map(kv => [kv.k, kv.v])),
  }))
  doc.tidbSecrets = initTidb.value.map(s => ({
    name: s.name, ...(s.namespace ? { namespace: s.namespace } : {}), database: s.database,
    extraStringData: Object.fromEntries(s.extra.filter(kv => kv.k.trim()).map(kv => [kv.k, kv.v])),
  }))
  return stringify(doc)
}

async function submitInit() {
  const yaml = initMode.value === 'form' ? formToYaml() : initYaml.value
  // 软提示：值没改动过就问一下
  if (initMode.value === 'yaml' && yaml.trim() === initCopiedYaml.trim()) {
    try { await ElMessageBox.confirm('当前值未改动（还是源项目的值），确定提交？', '确认', { type: 'warning' }) } catch { return }
  }
  submitting.value = true
  try {
    await api.initZkv({ template_id: initTplId.value, target_env_id: initRow.value.id, values_yaml: yaml })
    ElMessage.success('已提交，z-kv-secrets 初始化完成')
    initDlg.value = false
    if (initRow.value) checkZkv(initRow.value)
  } catch (e) {
    ElMessage.error(e?.response?.data?.message || '提交失败')
  } finally { submitting.value = false }
}

async function load() {
  loading.value = true
  try {
    // 只列 K8s 项目环境（VM 没有 ingress 网关）
    envs.value = ((await api.listProjectEnvs()) || []).filter(e => e.git_repo !== undefined)
    contacts.value = (await api.listContacts()) || []
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
.init-hint { color: #d97706; font-size: 12.5px; margin-bottom: 4px; }
.init-form { max-height: 52vh; overflow: auto; }
.init-grp { border: 1px dashed var(--el-border-color); border-radius: 6px; padding: 8px 10px; margin-bottom: 8px; }
.init-gt { font-weight: 600; font-size: 13px; margin-bottom: 6px; }
.init-ns { color: #909399; font-size: 12px; font-weight: normal; }
.init-row { display: flex; align-items: center; margin: 4px 0; }
.init-k { width: 130px; color: #606266; font-size: 12px; }
</style>
