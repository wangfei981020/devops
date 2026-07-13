<template>
  <div class="values-editor">
    <div class="mode-bar">
      <span class="mode-label">编辑方式</span>
      <el-radio-group v-model="mode" size="small" @change="onModeChange">
        <el-radio-button label="form">表单模式</el-radio-button>
        <el-radio-button label="yaml">YAML 模式</el-radio-button>
      </el-radio-group>
      <span class="mode-hint">{{ mode === 'form' ? '中文填字段，不用碰 YAML' : '直接改 values.yaml 原文（保留原顺序/注释）' }}</span>
    </div>

    <!-- 表单模式 -->
    <div v-show="mode === 'form'" class="form-mode">
      <div class="grp">镜像</div>
      <div class="row"><label>镜像仓库</label><el-input v-model="form.imageRepository" size="small" /></div>
      <div class="row"><label>初始版本 tag</label><el-input v-model="form.imageTag" size="small" placeholder="留空=用 chart 默认版本" /></div>

      <div class="grp">规模</div>
      <div class="row"><label>自动扩容</label>
        <el-switch v-model="form.autoscalingEnabled" />
        <template v-if="form.autoscalingEnabled">
          <span class="inline">最小</span><el-input-number v-model="form.minReplicas" :min="1" size="small" controls-position="right" />
          <span class="inline">最大</span><el-input-number v-model="form.maxReplicas" :min="1" size="small" controls-position="right" />
          <template v-if="hasTargetMemory"><span class="inline">目标内存</span><el-input-number v-model="form.targetMemory" :min="1" :max="100" size="small" controls-position="right" /><span class="inline">%</span></template>
          <template v-if="hasTargetCPU"><span class="inline">目标CPU</span><el-input-number v-model="form.targetCPU" :min="1" :max="100" size="small" controls-position="right" /><span class="inline">%</span></template>
        </template>
        <template v-else>
          <span class="inline">固定实例数</span><el-input-number v-model="form.replicaCount" :min="0" size="small" controls-position="right" />
        </template>
      </div>

      <div class="grp">对外访问</div>
      <div class="row"><label>对外暴露</label><el-switch v-model="form.ingressEnabled" /></div>
      <template v-if="form.ingressEnabled">
        <div class="row"><label>网关名</label><el-input v-model="form.ingressName" size="small" placeholder="如 istio-system/g66-uat-istio-ingressgateway-extra" /></div>
        <div class="row"><label>访问域名</label><el-input v-model="form.ingressHost" size="small" placeholder="如 xxx.k8s-g32-uat.com" /></div>
      </template>

      <div class="grp">资源</div>
      <div class="row"><label>CPU</label><span class="inline">请求</span><el-input v-model="form.cpuReq" size="small" style="width:110px" /><span class="inline">上限</span><el-input v-model="form.cpuLim" size="small" style="width:110px" /></div>
      <div class="row"><label>内存</label><span class="inline">请求</span><el-input v-model="form.memReq" size="small" style="width:110px" /><span class="inline">上限</span><el-input v-model="form.memLim" size="small" style="width:110px" /></div>

      <template v-if="hasSecretField">
        <div class="grp">依赖密钥（Secret）</div>
        <div class="secrets">
          <el-tag v-for="(s, i) in form.secrets" :key="i" closable size="small" @close="form.secrets.splice(i,1)">{{ s }}</el-tag>
          <el-input v-model="newSecret" size="small" style="width:220px" placeholder="secret 名，回车添加" @keyup.enter="addSecret" />
          <el-button link type="primary" @click="addSecret">+ 添加</el-button>
        </div>
      </template>

      <div class="form-note">ⓘ 服务端口、健康检查探针一般用模板默认，未在表单展示；需要改切「YAML 模式」。</div>
    </div>

    <!-- YAML 模式（CodeMirror：高亮 + 行号，VSCode 式）-->
    <div v-show="mode === 'yaml'" class="yaml-mode">
      <CodeEditor v-model="yamlText" />
    </div>
  </div>
</template>

<script setup>
import { ref, reactive, watch } from 'vue'
import { parseDocument } from 'yaml'
import CodeEditor from './CodeEditor.vue'

const props = defineProps({
  modelValue: { type: String, default: '' },  // 预填的 values.yaml 原文
  moduleType: { type: String, default: 'backend' },
})

const mode = ref('form')
const yamlText = ref(props.modelValue || '')
const newSecret = ref('')
let doc = parseDocument(props.modelValue || '')

const form = reactive({
  imageRepository: '', imageTag: '',
  autoscalingEnabled: false, minReplicas: 1, maxReplicas: 3, targetMemory: 70, targetCPU: 80, replicaCount: 1,
  ingressEnabled: false, ingressName: '', ingressHost: '', ingressPrefix: '/', ingressPort: 8080,
  cpuReq: '', cpuLim: '', memReq: '', memLim: '',
  ports: [], secrets: [],
  livenessPath: '', livenessPort: '', readinessPath: '', readinessPort: '',
})
const hasProbeTimings = ref(false)
const hasSecretField = ref(true) // 模板有 extraEnvVars 才显示 Secret 段（后端有、前端没有）
// 目标利用率：values 里存在(没被注释)才显示；注释掉的解析不到 → 不显示、也不回写
const hasTargetMemory = ref(false)
const hasTargetCPU = ref(false)
// 脏标记：加载后快照，只回写用户真正改过的字段（没改的原样保留，含注释/格式）
let original = {}
function snapshot() { original = JSON.parse(JSON.stringify(form)) }
function changed(k) { return JSON.stringify(form[k]) !== JSON.stringify(original[k]) }

function firstOf(v) { return Array.isArray(v) ? (v[0] ?? '') : (v ?? '') }

function loadFormFromDoc() {
  const g = (p, d) => { const v = doc.getIn(p); return v === undefined || v === null ? d : v }
  form.imageRepository = g(['image', 'repository'], '')
  form.imageTag = g(['image', 'tag'], '')
  form.autoscalingEnabled = !!g(['autoscaling', 'enabled'], false)
  form.minReplicas = Number(g(['autoscaling', 'minReplicas'], 1))
  form.maxReplicas = Number(g(['autoscaling', 'maxReplicas'], 3))
  form.targetMemory = Number(g(['autoscaling', 'targetMemoryUtilizationPercentage'], 70))
  form.targetCPU = Number(g(['autoscaling', 'targetCPUUtilizationPercentage'], 80))
  hasTargetMemory.value = doc.getIn(['autoscaling', 'targetMemoryUtilizationPercentage']) !== undefined
  hasTargetCPU.value = doc.getIn(['autoscaling', 'targetCPUUtilizationPercentage']) !== undefined
  form.replicaCount = Number(g(['replicaCount'], 1))
  // 入口：模板里没配 enabled 时，按模块类型给默认（后端 false / 前端 true）
  const ie = doc.getIn(['ingressGateway', 'enabled'])
  form.ingressEnabled = ie === undefined || ie === null ? (props.moduleType === 'frontend') : !!ie
  form.ingressName = g(['ingressGateway', 'name'], '')
  form.ingressHost = firstOf(doc.getIn(['ingressGateway', 'host'])?.toJSON?.() ?? g(['ingressGateway', 'host'], ''))
  form.ingressPrefix = firstOf(doc.getIn(['ingressGateway', 'matchPrefix'])?.toJSON?.() ?? '/') || '/'
  form.ingressPort = Number(g(['ingressGateway', 'port'], 8080))
  form.cpuReq = g(['resources', 'requests', 'cpu'], '')
  form.cpuLim = g(['resources', 'limits', 'cpu'], '')
  form.memReq = g(['resources', 'requests', 'memory'], '')
  form.memLim = g(['resources', 'limits', 'memory'], '')
  const ports = doc.getIn(['service', 'ports'])?.toJSON?.() || []
  form.ports = ports.map(p => ({ name: p.name || '', port: Number(p.port) || 80, targetPort: Number(p.targetPort) || 80 }))
  const envs = doc.getIn(['extraEnvVars'])?.toJSON?.() || []
  form.secrets = envs.map(e => e?.name).filter(Boolean)
  hasSecretField.value = doc.getIn(['extraEnvVars']) !== undefined
  form.livenessPath = g(['livenessProbe', 'httpGet', 'path'], '')
  form.livenessPort = String(g(['livenessProbe', 'httpGet', 'port'], '') || '')
  form.readinessPath = g(['readinessProbe', 'httpGet', 'path'], '')
  form.readinessPort = String(g(['readinessProbe', 'httpGet', 'port'], '') || '')
  // 探针有 initialDelay 等超时字段时提示"已保留、YAML 可改"
  hasProbeTimings.value = doc.getIn(['livenessProbe', 'initialDelaySeconds']) !== undefined ||
    doc.getIn(['livenessProbe', 'periodSeconds']) !== undefined ||
    doc.getIn(['readinessProbe', 'initialDelaySeconds']) !== undefined
  snapshot() // 记录加载后的基线，供脏标记比对
}

function num(v) { const n = Number(v); return Number.isFinite(n) ? n : v }

// 只回写「改过」的字段（脏标记）；没碰的字段在 doc 里原样保留，含注释/格式/顺序
function writeFormToDoc() {
  if (changed('imageRepository')) doc.setIn(['image', 'repository'], form.imageRepository)
  if (changed('imageTag')) doc.setIn(['image', 'tag'], form.imageTag)
  if (changed('autoscalingEnabled')) doc.setIn(['autoscaling', 'enabled'], form.autoscalingEnabled)
  if (changed('minReplicas')) doc.setIn(['autoscaling', 'minReplicas'], num(form.minReplicas))
  if (changed('maxReplicas')) doc.setIn(['autoscaling', 'maxReplicas'], num(form.maxReplicas))
  if (hasTargetMemory.value && changed('targetMemory')) doc.setIn(['autoscaling', 'targetMemoryUtilizationPercentage'], num(form.targetMemory))
  if (hasTargetCPU.value && changed('targetCPU')) doc.setIn(['autoscaling', 'targetCPUUtilizationPercentage'], num(form.targetCPU))
  if (changed('replicaCount')) doc.setIn(['replicaCount'], num(form.replicaCount))
  if (changed('ingressEnabled')) doc.setIn(['ingressGateway', 'enabled'], form.ingressEnabled)
  if (changed('ingressName')) doc.setIn(['ingressGateway', 'name'], form.ingressName)
  if (changed('ingressHost')) doc.setIn(['ingressGateway', 'host'], form.ingressHost ? [form.ingressHost] : [])
  if (changed('ingressPrefix')) doc.setIn(['ingressGateway', 'matchPrefix'], [form.ingressPrefix || '/'])
  if (changed('ingressPort')) doc.setIn(['ingressGateway', 'port'], num(form.ingressPort))
  if (changed('cpuReq')) doc.setIn(['resources', 'requests', 'cpu'], form.cpuReq)
  if (changed('cpuLim')) doc.setIn(['resources', 'limits', 'cpu'], form.cpuLim)
  if (changed('memReq')) doc.setIn(['resources', 'requests', 'memory'], form.memReq)
  if (changed('memLim')) doc.setIn(['resources', 'limits', 'memory'], form.memLim)
  if (changed('ports')) doc.setIn(['service', 'ports'], form.ports.map(p => ({ name: p.name, port: num(p.port), targetPort: num(p.targetPort) })))
  if (changed('secrets')) doc.setIn(['extraEnvVars'], form.secrets.map(n => ({ name: n })))
  if (changed('livenessPath')) doc.setIn(['livenessProbe', 'httpGet', 'path'], form.livenessPath)
  if (changed('livenessPort')) doc.setIn(['livenessProbe', 'httpGet', 'port'], num(form.livenessPort))
  if (changed('readinessPath')) doc.setIn(['readinessProbe', 'httpGet', 'path'], form.readinessPath)
  if (changed('readinessPort')) doc.setIn(['readinessProbe', 'httpGet', 'port'], num(form.readinessPort))
}

function addSecret() {
  const s = newSecret.value.trim()
  if (s && !form.secrets.includes(s)) form.secrets.push(s)
  newSecret.value = ''
}

function onModeChange(to) {
  if (to === 'yaml') {
    writeFormToDoc()
    yamlText.value = doc.toString()
  } else {
    try { doc = parseDocument(yamlText.value) } catch (_) { /* 保留旧 doc */ }
    loadFormFromDoc()
  }
}

// 供父组件在 预览/提交 时取最终 YAML
function getYaml() {
  if (mode.value === 'form') {
    writeFormToDoc()
    return doc.toString()
  }
  try { doc = parseDocument(yamlText.value) } catch (_) {}
  return yamlText.value
}
defineExpose({ getYaml })

// 父组件重新预填（换模板/模块名）时，重载
watch(() => props.modelValue, (v) => {
  yamlText.value = v || ''
  doc = parseDocument(v || '')
  loadFormFromDoc()
})

loadFormFromDoc()
</script>

<style scoped>
.values-editor { border: 1px solid var(--el-border-color); border-radius: 8px; padding: 12px 14px; }
.mode-bar { display: flex; align-items: center; gap: 12px; margin-bottom: 12px; }
.mode-label { font-size: 13px; color: var(--el-text-color-secondary); }
.mode-hint { font-size: 12px; color: #909399; }
.form-mode .grp { font-weight: 600; font-size: 13px; margin: 12px 0 6px; color: var(--el-color-primary); border-left: 3px solid var(--el-color-primary); padding-left: 8px; }
.row { display: flex; align-items: center; gap: 8px; margin-bottom: 8px; }
.row > label { width: 88px; text-align: right; font-size: 13px; color: var(--el-text-color-regular); flex: none; }
.row .el-input { max-width: 380px; }
.inline { font-size: 12px; color: #909399; margin: 0 2px; }
.ports .port-row { display: flex; align-items: center; gap: 8px; margin-bottom: 6px; margin-left: 96px; }
.secrets { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; margin-left: 96px; }
.probe-note { font-size: 12px; color: #909399; }
.form-note { font-size: 12px; color: #909399; margin-top: 10px; }
.yaml-ta :deep(textarea) { font-family: 'Menlo', 'Consolas', monospace; font-size: 13px; line-height: 1.5; }
</style>
