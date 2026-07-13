<template>
  <div class="orch-page">
    <div class="page-head">
      <div class="head-left">
        <h2>服务编排</h2>
        <el-select v-model="envId" placeholder="选择项目环境" filterable style="width: 260px" @change="loadModules">
          <el-option v-for="e in envs" :key="e.id" :label="`${e.name}（${e.env_type}）`" :value="e.id" />
        </el-select>
      </div>
      <div class="head-btns">
        <el-button :disabled="!envId" @click="openBatch">批量新增</el-button>
        <el-button type="primary" :disabled="!envId" @click="openAdd">+ 新增模块</el-button>
      </div>
    </div>

    <el-table :data="modules" border stripe v-loading="loadingMods" empty-text="选择环境后显示其模块（新增的模块提交后会被扫描进来）">
      <el-table-column label="模块" prop="name" min-width="260" />
      <el-table-column label="镜像仓库" prop="image_repository" min-width="240" show-overflow-tooltip />
      <el-table-column label="当前 tag" prop="current_tag" min-width="180" show-overflow-tooltip />
      <el-table-column label="namespace" prop="namespace" width="140" />
      <el-table-column label="操作" width="150">
        <template #default>
          <el-tooltip content="下一期：直接改 values.yaml / configmap / secret，不用再拉 GitLab 到本地" placement="top">
            <el-button link disabled>编辑（即将）</el-button>
          </el-tooltip>
        </template>
      </el-table-column>
    </el-table>

    <!-- 新增模块弹窗 -->
    <el-dialog v-model="addDialog" title="新增模块" width="820px" :close-on-click-modal="false" top="5vh">
      <el-form :model="form" label-width="110px">
        <el-form-item label="目标环境">
          <span>{{ curEnvLabel }}</span>
        </el-form-item>
        <el-form-item label="参照模板" required>
          <el-select v-model="form.templateId" placeholder="选样板服务当模子" filterable style="width: 420px" @change="resetPreview">
            <el-option v-for="t in templates" :key="t.id"
              :label="`${t.project || '全局'} · ${t.src_service} · ${t.module_type === 'frontend' ? '前端' : '后端'}`"
              :value="t.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="模块名" required>
          <el-input v-model="form.moduleName" placeholder="完整模块名，如 g32-baccarat-settle-backend" style="width: 420px" @input="resetPreview" />
          <el-button style="margin-left: 10px" :disabled="!canPrefill" :loading="prefilling" @click="doPrefill">预填 values.yaml</el-button>
        </el-form-item>
        <el-form-item label="namespace" required>
          <el-input v-model="form.namespace" placeholder="如 g32-base / g32-bet-settle" style="width: 420px" @input="resetPreview" />
        </el-form-item>
        <el-form-item label="配置">
          <div v-if="form.valuesYaml" style="width:100%">
            <ValuesEditor ref="editorRef" :modelValue="form.valuesYaml" :moduleType="selectedTemplateType" />
          </div>
          <span v-else class="hint">先填模块名并点「预填 values.yaml」，再在此用 表单/YAML 配置</span>
        </el-form-item>
        <el-form-item v-if="configmaps.length" label="ConfigMap">
          <el-tabs v-model="cmTab" type="border-card" style="width:100%">
            <el-tab-pane v-for="cm in configmaps" :key="cm.path" :label="cmName(cm.path)" :name="cm.path">
              <CodeEditor v-model="cm.content" />
            </el-tab-pane>
          </el-tabs>
          <div class="hint">自动从 templates/ 扫出的 configmap（多个按文件名分 tab），改里面的配置值；helm 变量 {{ }} 别动</div>
        </el-form-item>
        <el-form-item label="ArgoCD">
          <el-switch v-model="form.disable" :active-value="true" :inactive-value="false"
            active-text="disable:true 安全预演（先不生成 Application）"
            inactive-text="关闭=直接部署（生成 Application，默认）" />
        </el-form-item>

        <div v-if="preview" class="preview-box">
          <el-alert v-if="preview.helm_skipped" type="warning" :closable="false" title="helm 未安装，跳过渲染校验" />
          <el-alert v-else :type="preview.helm_ok ? 'success' : 'error'" :closable="false"
            :title="preview.helm_ok ? 'helm 渲染校验通过 ✓' : 'helm 渲染校验失败 ✗（已阻止提交）'" />
          <div v-if="!preview.helm_ok && !preview.helm_skipped" class="err-card">
            <div class="err-head">
              <el-icon><CircleCloseFilled /></el-icon>
              <span>helm 渲染校验失败</span>
              <el-button link type="primary" class="err-copy" @click="copyErr(preview.helm_output)">复制报错</el-button>
            </div>
            <pre class="err-body">{{ preview.helm_output }}</pre>
          </div>
          <div class="changed-title">将提交的改动（{{ (preview.changed_files || []).length }}）：</div>
          <ul class="changed"><li v-for="f in preview.changed_files" :key="f"><code>{{ f }}</code></li></ul>
          <div class="hint">🔒 提交抢环境写锁 + 硬同步远端，不覆盖别人</div>
        </div>
        <el-alert v-if="submitted" type="success" :closable="false" class="submitted"
          :title="`已提交 commit ${submitted.commit_sha}`" show-icon />
      </el-form>

      <template #footer>
        <el-button @click="addDialog = false">关闭</el-button>
        <el-button type="primary" :disabled="!canPreview" :loading="previewing" @click="doPreview">helm 校验并预览</el-button>
        <el-button type="success" :disabled="!canSubmit" :loading="submitting" @click="doSubmit">确认提交</el-button>
      </template>
    </el-dialog>

    <!-- 批量新增弹窗 -->
    <el-dialog v-model="batchDialog" title="批量新增模块" width="880px" :close-on-click-modal="false" top="5vh">
      <el-form label-width="110px">
        <el-form-item label="目标环境"><span>{{ curEnvLabel }}</span></el-form-item>
        <el-form-item label="参照模板" required>
          <el-select v-model="batch.templateId" placeholder="选样板服务当模子" filterable style="width: 420px" @change="resetBatch">
            <el-option v-for="t in templates" :key="t.id"
              :label="`${t.project || '全局'} · ${t.src_service} · ${t.module_type === 'frontend' ? '前端' : '后端'}`" :value="t.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="粘贴模块名">
          <el-input v-model="batch.paste" type="textarea" :rows="3" placeholder="一行一个完整模块名，粘贴后点『解析成行』" style="width: 520px" />
          <el-button style="margin-left: 10px" @click="parsePaste">解析成行</el-button>
        </el-form-item>
        <el-form-item label="模块清单">
          <el-table :data="batch.rows" border size="small" style="width: 640px">
            <el-table-column label="模块名" min-width="260">
              <template #default="{ row }"><el-input v-model="row.module_name" size="small" @input="resetBatch" /></template>
            </el-table-column>
            <el-table-column label="namespace" width="180">
              <template #default="{ row }"><el-input v-model="row.namespace" size="small" @input="resetBatch" /></template>
            </el-table-column>
            <el-table-column label="配置" width="90">
              <template #default="{ row }">
                <el-button link type="primary" :disabled="!batch.templateId || !row.module_name.trim()" @click="openRowConfig(row)">
                  {{ row.values_yaml ? '已改·配置' : '配置' }}
                </el-button>
              </template>
            </el-table-column>
            <el-table-column label="校验" width="80">
              <template #default="{ row }">
                <el-tag v-if="rowStatus(row.module_name)" :type="rowStatus(row.module_name).ok ? 'success' : 'danger'" size="small">
                  {{ rowStatus(row.module_name).ok ? '通过' : '失败' }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column label="" width="44">
              <template #default="{ $index }"><el-button link type="danger" @click="batch.rows.splice($index, 1)">×</el-button></template>
            </el-table-column>
          </el-table>
          <el-button link type="primary" @click="batch.rows.push({ module_name: '', namespace: '' })">+ 加一行</el-button>
        </el-form-item>
        <el-form-item label="ArgoCD">
          <el-switch v-model="batch.disable" :active-value="true" :inactive-value="false" active-text="disable:true 安全预演" inactive-text="关闭=直接部署（默认）" />
        </el-form-item>

        <div v-if="batchResult" class="preview-box">
          <el-alert :type="batchResult.all_ok ? 'success' : 'error'" :closable="false"
            :title="batchResult.all_ok ? `全部通过 ✓（共 ${batchResult.rows.length} 个模块，${batchResult.changed_files} 个文件）` : '有行未通过，已阻止提交（见清单红标）'" />
          <div v-for="row in batchResult.rows.filter(r => r.error)" :key="row.module_name" class="err-card">
            <div class="err-head">
              <el-icon><CircleCloseFilled /></el-icon>
              <span>{{ row.module_name }} 校验失败</span>
              <el-button link type="primary" class="err-copy" @click="copyErr(row.error)">复制报错</el-button>
            </div>
            <pre class="err-body">{{ row.error }}</pre>
          </div>
        </div>
        <el-alert v-if="batchSubmitted" type="success" :closable="false" class="submitted"
          :title="`已提交 commit ${batchSubmitted.commit_sha}（${batchSubmitted.rows.length} 个模块）`" show-icon />
      </el-form>

      <template #footer>
        <el-button @click="batchDialog = false">关闭</el-button>
        <el-button type="primary" :disabled="!canBatchPreview" :loading="batchPreviewing" @click="doBatchPreview">批量校验预览</el-button>
        <el-button type="success" :disabled="!canBatchSubmit" :loading="batchSubmitting" @click="doBatchSubmit">确认批量提交</el-button>
      </template>
    </el-dialog>

    <!-- 批量·单行配置弹窗 -->
    <el-dialog v-model="rowDialog" :title="`配置模块 ${rowEditing?.module_name || ''}`" width="820px" :close-on-click-modal="false" top="5vh" append-to-body>
      <div v-if="rowYaml" v-loading="rowLoading">
        <ValuesEditor ref="rowEditorRef" :modelValue="rowYaml" :moduleType="selectedBatchTemplateType" />
        <div v-if="rowConfigmaps.length" class="cm-block">
          <div class="cm-title">ConfigMap</div>
          <el-tabs v-model="rowCmTab" type="border-card">
            <el-tab-pane v-for="cm in rowConfigmaps" :key="cm.path" :label="cmName(cm.path)" :name="cm.path">
              <CodeEditor v-model="cm.content" />
            </el-tab-pane>
          </el-tabs>
        </div>
      </div>
      <div v-else v-loading="rowLoading" style="min-height:80px">加载模板中…</div>
      <template #footer>
        <el-button @click="rowDialog = false">取消</el-button>
        <el-button type="primary" @click="saveRowConfig">保存该模块配置</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import * as api from '../api'
import ValuesEditor from '../components/ValuesEditor.vue'
import CodeEditor from '../components/CodeEditor.vue'
import { CircleCloseFilled } from '@element-plus/icons-vue'

function cmName(p) { return (p || '').split('/').pop() }

function copyErr(text) {
  navigator.clipboard?.writeText(text || '').then(() => ElMessage.success('已复制报错')).catch(() => {})
}

const envs = ref([])
const templates = ref([])
const envId = ref(null)
const modules = ref([])
const loadingMods = ref(false)

const curEnvLabel = computed(() => {
  const e = envs.value.find(x => x.id === envId.value)
  return e ? `${e.name}（${e.env_type}）` : ''
})

async function loadModules() {
  if (!envId.value) { modules.value = []; return }
  loadingMods.value = true
  try { modules.value = (await api.listModules(envId.value)) || [] }
  catch { modules.value = [] }
  finally { loadingMods.value = false }
}

// ---- 新增模块弹窗 ----
const addDialog = ref(false)
const editorRef = ref(null)
const form = ref({ templateId: null, moduleName: '', namespace: '', valuesYaml: '', disable: false })
const configmaps = ref([]) // [{path, content}]，前端服务才有；prefill 带出
const cmTab = ref('')
const selectedTemplateType = computed(() => templates.value.find(t => t.id === form.value.templateId)?.module_type || 'backend')
const prefilling = ref(false)
const previewing = ref(false)
const submitting = ref(false)
const preview = ref(null)
const submitted = ref(null)

const canPrefill = computed(() => envId.value && form.value.templateId && form.value.moduleName.trim())
const canPreview = computed(() => canPrefill.value && form.value.namespace.trim() && form.value.valuesYaml.trim())
const canSubmit = computed(() => preview.value && (preview.value.helm_ok || preview.value.helm_skipped))

function resetPreview() { preview.value = null; submitted.value = null }

function openAdd() {
  form.value = { templateId: null, moduleName: '', namespace: '', valuesYaml: '', disable: false }
  configmaps.value = []
  cmTab.value = ''
  resetPreview()
  addDialog.value = true
}

function reqBody() {
  // 从 ValuesEditor 取最终 YAML（表单模式会把字段写回、保留原顺序；YAML 模式取原文）
  const yaml = editorRef.value?.getYaml?.() || form.value.valuesYaml
  return {
    template_id: form.value.templateId,
    target_env_id: envId.value,
    module_name: form.value.moduleName.trim(),
    namespace: form.value.namespace.trim(),
    values_yaml: yaml,
    configmaps: configmaps.value.map(c => ({ path: c.path, content: c.content })),
    disable: form.value.disable,
  }
}

async function doPrefill() {
  prefilling.value = true
  try {
    const r = await api.prefillModule({ template_id: form.value.templateId, target_env_id: envId.value, module_name: form.value.moduleName.trim() })
    form.value.valuesYaml = r.values_yaml || ''
    configmaps.value = (r.configmaps || []).map(c => ({ ...c }))
    cmTab.value = configmaps.value[0]?.path || ''
    if (!form.value.namespace) form.value.namespace = r.suggest_namespace || ''
    resetPreview()
    ElMessage.success('已带出样板 values.yaml，请复核后编辑')
  } finally { prefilling.value = false }
}

async function doPreview() {
  previewing.value = true
  submitted.value = null
  try { preview.value = await api.previewModule(reqBody()) } finally { previewing.value = false }
}

async function doSubmit() {
  try {
    await ElMessageBox.confirm(`确认提交新增模块 ${form.value.moduleName.trim()}？（disable:${form.value.disable}）`, '确认提交', { type: 'warning' })
  } catch { return }
  submitting.value = true
  try {
    submitted.value = await api.submitModule(reqBody())
    ElMessage.success('提交成功')
    await loadModules()
  } finally { submitting.value = false }
}

// ---- 批量新增 ----
const batchDialog = ref(false)
const batch = ref({ templateId: null, paste: '', disable: false, rows: [] })
const batchResult = ref(null)
const batchSubmitted = ref(null)
const batchPreviewing = ref(false)
const batchSubmitting = ref(false)

const validRows = computed(() => batch.value.rows.filter(r => r.module_name.trim() && r.namespace.trim()))
const canBatchPreview = computed(() => batch.value.templateId && validRows.value.length > 0)
const canBatchSubmit = computed(() => batchResult.value && batchResult.value.all_ok)
const selectedBatchTemplateType = computed(() => templates.value.find(t => t.id === batch.value.templateId)?.module_type || 'backend')

// 批量·单行配置弹窗
const rowDialog = ref(false)
const rowEditorRef = ref(null)
const rowEditing = ref(null)
const rowYaml = ref('')
const rowConfigmaps = ref([])
const rowCmTab = ref('')
const rowLoading = ref(false)

async function openRowConfig(row) {
  rowEditing.value = row
  rowDialog.value = true
  rowYaml.value = ''
  rowConfigmaps.value = []
  // 已改过就用已存的；否则拉该模块的派生预填
  if (row.values_yaml) {
    rowYaml.value = row.values_yaml
    rowConfigmaps.value = (row.configmaps || []).map(c => ({ ...c }))
    rowCmTab.value = rowConfigmaps.value[0]?.path || ''
    return
  }
  rowLoading.value = true
  try {
    const r = await api.prefillModule({ template_id: batch.value.templateId, target_env_id: envId.value, module_name: row.module_name.trim() })
    rowYaml.value = r.values_yaml || ''
    rowConfigmaps.value = (r.configmaps || []).map(c => ({ ...c }))
    rowCmTab.value = rowConfigmaps.value[0]?.path || ''
  } finally { rowLoading.value = false }
}

function saveRowConfig() {
  if (rowEditing.value && rowEditorRef.value) {
    rowEditing.value.values_yaml = rowEditorRef.value.getYaml()
    rowEditing.value.configmaps = rowConfigmaps.value.map(c => ({ path: c.path, content: c.content }))
    resetBatch()
    ElMessage.success('已保存该模块配置')
  }
  rowDialog.value = false
}

function resetBatch() { batchResult.value = null; batchSubmitted.value = null }
function rowStatus(name) {
  if (!batchResult.value) return null
  const r = batchResult.value.rows.find(x => x.module_name === name.trim())
  return r ? { ok: r.helm_ok || r.helm_skipped } : null
}

function openBatch() {
  batch.value = { templateId: null, paste: '', disable: false, rows: [] }
  resetBatch()
  batchDialog.value = true
}

function parsePaste() {
  const names = batch.value.paste.split('\n').map(s => s.trim()).filter(Boolean)
  const ns = envs.value.find(e => e.id === envId.value)?.name || ''
  batch.value.rows = names.map(n => ({ module_name: n, namespace: ns }))
  resetBatch()
}

function batchBody() {
  return {
    template_id: batch.value.templateId,
    target_env_id: envId.value,
    disable: batch.value.disable,
    rows: validRows.value.map(r => ({ module_name: r.module_name.trim(), namespace: r.namespace.trim(), values_yaml: r.values_yaml || '', configmaps: r.configmaps || [] })),
  }
}

async function doBatchPreview() {
  batchPreviewing.value = true
  batchSubmitted.value = null
  try { batchResult.value = await api.batchPreviewModules(batchBody()) } finally { batchPreviewing.value = false }
}

async function doBatchSubmit() {
  try {
    await ElMessageBox.confirm(`确认批量提交 ${validRows.value.length} 个模块？（disable:${batch.value.disable}）`, '确认批量提交', { type: 'warning' })
  } catch { return }
  batchSubmitting.value = true
  try {
    batchSubmitted.value = await api.batchSubmitModules(batchBody())
    ElMessage.success('批量提交成功')
    await loadModules()
  } finally { batchSubmitting.value = false }
}

onMounted(async () => {
  envs.value = (await api.listProjectEnvs()) || []
  templates.value = (await api.listTemplates()) || []
})
</script>

<style scoped>
.orch-page { padding: 16px 20px; }
.page-head { display: flex; justify-content: space-between; align-items: center; margin-bottom: 16px; }
.head-left { display: flex; align-items: center; gap: 14px; }
.head-left h2 { margin: 0; font-size: 18px; }
.yaml-editor :deep(textarea) { font-family: 'Menlo', 'Consolas', monospace; font-size: 13px; line-height: 1.5; }
.preview-box { margin: 8px 0 0 110px; padding: 12px 14px; background: var(--el-fill-color-light); border-radius: 8px; }
.err-card { margin: 8px 0; border: 1px solid #fbc4c4; border-radius: 8px; overflow: hidden; background: #fff5f5; }
.err-head { display: flex; align-items: center; gap: 6px; padding: 8px 12px; background: #fde2e2; color: #c0392b; font-weight: 600; font-size: 13px; }
.err-head .err-copy { margin-left: auto; }
.err-body { margin: 0; padding: 10px 12px; background: #1e1e1e; color: #ff9b9b; overflow-x: auto; font-size: 12px; line-height: 1.5; white-space: pre-wrap; font-family: 'Menlo', 'Consolas', monospace; max-height: 260px; }
.changed-title { font-weight: 600; margin: 8px 0 4px; }
.changed { margin: 0; padding-left: 18px; }
.changed code { font-size: 12px; }
.hint { color: #909399; font-size: 12px; margin-top: 6px; }
.submitted { margin: 10px 0 0 110px; }
.cm-block { margin-top: 12px; }
.cm-title { font-weight: 600; font-size: 13px; margin-bottom: 6px; color: var(--el-color-primary); }
</style>
