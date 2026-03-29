<template>
  <div>
    <div class="card">
      <div class="card-header">
        <div class="card-title">{{ isEdit ? '编辑告警规则' : '新建告警规则' }}</div>
        <router-link to="/alert-rules" class="btn btn-outline">返回列表</router-link>
      </div>

      <form @submit.prevent="handleSubmit">
        <!-- 基本信息 -->
        <h3 style="margin-bottom: 12px; font-size: 15px; color: var(--text-secondary);">基本信息</h3>
        <div class="form-row">
          <div class="form-group">
            <label class="form-label">规则名称 *</label>
            <input v-model="form.name" class="form-input" placeholder="如: G32 resource alarm" required />
          </div>
          <div class="form-group">
            <label class="form-label">告警级别</label>
            <select v-model="form.severity" class="form-select">
              <option value="S1">S1 灾难</option>
              <option value="S2">S2 严重</option>
              <option value="S3">S3 警告</option>
            </select>
          </div>
        </div>

        <div class="form-row">
          <div class="form-group">
            <label class="form-label">告警模式</label>
            <select v-model="form.alert_mode" class="form-select">
              <option value="found">搜到关键词 → 告警</option>
              <option value="not_found">搜不到关键词 → 告警</option>
            </select>
            <div class="form-hint">
              <template v-if="form.alert_mode === 'found'">ES 搜到匹配日志时触发告警（默认）</template>
              <template v-else>指定时间内搜不到匹配日志时触发告警，搜到后发送恢复通知</template>
            </div>
          </div>
          <div class="form-group" v-if="form.alert_mode === 'not_found'">
            <label class="form-label">
              <input type="checkbox" v-model="recoveryChecked" style="margin-right: 6px;" />
              启用恢复通知
            </label>
            <div class="form-hint">恢复后发送绿色通知卡片</div>
          </div>
        </div>

        <!-- 数据源 + 连接 -->
        <div class="form-row">
          <div class="form-group">
            <label class="form-label">数据源类型</label>
            <select v-model="form.data_source_type" class="form-select">
              <option value="es">Elasticsearch</option>
              <option value="loki">Loki</option>
            </select>
          </div>
          <div class="form-group" v-if="form.data_source_type === 'es'">
            <label class="form-label">ES 连接 *</label>
            <select v-model="form.es_connection_id" class="form-select" required>
              <option :value="0" disabled>请选择 ES 连接</option>
              <option v-for="c in esConnections" :key="c.id" :value="c.id">
                {{ c.name }} ({{ c.version }}.x)
              </option>
            </select>
          </div>
          <div class="form-group" v-else>
            <label class="form-label">Loki 连接 *</label>
            <select v-model="form.loki_connection_id" class="form-select" required>
              <option :value="0" disabled>请选择 Loki 连接</option>
              <option v-for="c in lokiConnections" :key="c.id" :value="c.id">{{ c.name }}</option>
            </select>
          </div>
        </div>

        <div class="form-row">
          <div class="form-group">
            <label class="form-label">Lark 配置 *</label>
            <select v-model="form.lark_config_id" class="form-select" required>
              <option :value="0" disabled>请选择 Lark 配置</option>
              <option v-for="c in larkConfigs" :key="c.id" :value="c.id">
                {{ c.name }} ({{ c.lark_type }})
              </option>
            </select>
          </div>
        </div>

        <!-- 搜索配置 -->
        <h3 style="margin: 20px 0 12px; font-size: 15px; color: var(--text-secondary);">搜索配置</h3>

        <!-- ES 搜索配置 -->
        <template v-if="form.data_source_type === 'es'">
          <div class="form-row">
            <div class="form-group">
              <label class="form-label">ES 索引</label>
              <input v-model="form.es_index" class="form-input" placeholder="如: app-logs-* 或 filebeat-*" />
              <div class="form-hint">支持通配符，多个用逗号分隔</div>
            </div>
            <div class="form-group">
              <label class="form-label">搜索关键词</label>
              <input v-model="form.keyword" class="form-input" placeholder='如: "搜不到指定格式日志" OR "跳局"' />
              <div class="form-hint">支持 Lucene 语法，AND/OR/NOT</div>
            </div>
          </div>
        </template>

        <!-- Loki 搜索配置 -->
        <template v-else>
          <div class="form-group">
            <label class="form-label">LogQL 查询 *</label>
            <input v-model="form.logql" class="form-input" placeholder='{namespace="default", container="app"} |~ "error"' />
            <div class="form-hint">Loki LogQL 查询语句，可在日志查询页调试后复制过来</div>
          </div>
        </template>

        <div class="form-row">
          <div class="form-group">
            <label class="form-label">执行周期 (Cron)</label>
            <input v-model="form.schedule" class="form-input" placeholder="*/5 * * * *" />
            <div class="form-hint">{{ cronHint || 'Cron 表达式，如 */5 * * * * 每5分钟' }}</div>
          </div>
          <div class="form-group">
            <label class="form-label">搜索时间范围</label>
            <input v-model="form.time_range" class="form-input" placeholder="5m" />
            <div class="form-hint">如 5m(分钟)、1h(小时)、30s(秒)</div>
          </div>
        </div>

        <!-- 过滤字段 -->
        <div class="form-group">
          <label class="form-label">过滤字段 (JSON)</label>
          <textarea v-model="form.filter_fields" class="form-textarea" rows="3"
            placeholder='[{"field":"kubernetes.namespace","value":"g32-uat","op":"match"}]'></textarea>
          <div class="form-hint">op 支持: match(默认), term, wildcard, exists</div>
        </div>

        <!-- 自定义 DSL -->
        <div class="form-group">
          <label class="form-label">自定义查询 DSL (JSON, 可选)</label>
          <textarea v-model="form.query_dsl" class="form-textarea" rows="4"
            placeholder="留空则自动根据关键词和过滤字段构建查询"></textarea>
          <div class="form-hint">填写后将覆盖关键词和过滤字段的自动构建</div>
        </div>

        <!-- 字段提取 -->
        <h3 style="margin: 20px 0 12px; font-size: 15px; color: var(--text-secondary);">字段提取</h3>
        <div class="form-group">
          <label class="form-label">提取规则 (JSON)</label>
          <textarea v-model="form.extract_fields" class="form-textarea" rows="4"
            :placeholder="extractFieldsPlaceholder"></textarea>
          <div class="form-hint">name=变量名, path=ES字段路径(支持嵌套如 kubernetes.namespace), pattern=正则(捕获组1)</div>
        </div>

        <!-- 消息模板 -->
        <h3 style="margin: 20px 0 12px; font-size: 15px; color: var(--text-secondary);">消息模板</h3>
        <div class="form-group">
          <label class="form-label">告警标题</label>
          <input v-model="form.message_title" class="form-input" placeholder="如: G32 resource alarm" />
        </div>
        <div class="form-group">
          <label class="form-label">消息模板</label>
          <textarea v-model="form.message_template" class="form-textarea" rows="6"
            :placeholder="templatePlaceholder"></textarea>
          <div class="form-hint">支持 Go template 语法，如 &#123;&#123;.field&#125;&#125;，变量来自字段提取或 ES _source 原始字段</div>
        </div>

        <!-- 恢复通知模板 (仅 not_found 模式) -->
        <template v-if="form.alert_mode === 'not_found' && recoveryChecked">
          <h3 style="margin: 20px 0 12px; font-size: 15px; color: var(--text-secondary);">恢复通知配置</h3>
          <div class="form-group">
            <label class="form-label">恢复通知标题</label>
            <input v-model="form.recovery_title" class="form-input" placeholder="留空则自动用: 告警标题 - 已恢复" />
          </div>
          <div class="form-group">
            <label class="form-label">恢复通知模板</label>
            <textarea v-model="form.recovery_template" class="form-textarea" rows="4"
              placeholder="留空则使用告警消息模板，恢复时变量来自第一条搜到的日志"></textarea>
          </div>
        </template>

        <!-- @用户 -->
        <h3 style="margin: 20px 0 12px; font-size: 15px; color: var(--text-secondary);">通知配置</h3>
        <div class="form-group">
          <label class="form-label">@用户 (JSON)</label>
          <textarea v-model="form.at_users" class="form-textarea" rows="3"
            placeholder='[{"name":"Bruce","user_id":"ou_xxxxx"},{"name":"Cesar","user_id":"ou_yyyyy"}]'></textarea>
          <div class="form-hint">user_id 为 Lark 的 open_id，可在飞书管理后台获取</div>
        </div>
        <div class="form-group">
          <label class="form-label">
            <input type="checkbox" v-model="atAllChecked" style="margin-right: 6px;" />
            @所有人
          </label>
        </div>

        <!-- 分组配置 -->
        <h3 style="margin: 20px 0 12px; font-size: 15px; color: var(--text-secondary);">分组配置</h3>
        <div class="form-group">
          <label class="form-label">分组字段</label>
          <div class="flex gap-2">
            <input v-model="form.group_by" class="form-input" placeholder="如: container" style="flex: 1;" />
            <select class="form-select" style="width: 220px;" @change="selectGroupBy($event)">
              <option value="">快捷选择...</option>
              <optgroup label="Loki Labels" v-if="(form.data_source_type || 'es') === 'loki'">
                <option value="container">container</option>
                <option value="namespace">namespace</option>
                <option value="pod">pod</option>
                <option value="job">job</option>
                <option value="instance">instance</option>
                <option value="service_name">service_name</option>
                <option value="node_name">node_name</option>
                <option value="stream">stream</option>
              </optgroup>
              <optgroup label="ES 常用字段" v-else>
                <option value="kubernetes.container_name">kubernetes.container_name</option>
                <option value="kubernetes.namespace">kubernetes.namespace</option>
                <option value="kubernetes.pod_name">kubernetes.pod_name</option>
                <option value="kubernetes.node_name">kubernetes.node_name</option>
                <option value="kubernetes.labels.app">kubernetes.labels.app</option>
                <option value="host.name">host.name</option>
                <option value="service.name">service.name</option>
              </optgroup>
            </select>
          </div>
          <div class="form-hint">按此字段分组，每组独立告警/恢复。留空则不分组</div>
        </div>

        <div v-if="form.group_by" class="form-group">
          <label class="form-label">期望容器列表 (可选)</label>
          <textarea v-model="form.expected_groups" class="form-textarea" rows="3"
            placeholder='["roulette-resource-backend","baccarat-resource-backend","dragon-tiger-resource-backend"]'></textarea>
          <div class="form-hint">JSON 数组，指定需要监控的容器。填写后按此列表检查，容器挂了也能发现。留空则自动从 24h 日志发现</div>
        </div>

        <div v-if="form.group_by" class="form-group">
          <label class="form-label">查询并发数</label>
          <select v-model.number="form.query_concurrency" class="form-select" style="width: 200px;">
            <option :value="1">1 (串行)</option>
            <option :value="3">3</option>
            <option :value="5">5 (默认)</option>
            <option :value="10">10</option>
            <option :value="20">20</option>
          </select>
          <div class="form-hint">单规则检查分组时的并发数。规则多时建议降低，避免给 Loki/ES 过大压力</div>
        </div>

        <!-- 去重配置 -->
        <h3 style="margin: 20px 0 12px; font-size: 15px; color: var(--text-secondary);">去重配置</h3>
        <div class="form-row">
          <div class="form-group">
            <label class="form-label">去重字段</label>
            <input v-model="form.dedup_field" class="form-input" placeholder="如: round,timestamp" />
            <div class="form-hint">多个字段用逗号分隔，相同组合不重复告警</div>
          </div>
          <div class="form-group">
            <label class="form-label">去重有效期 (秒)</label>
            <input v-model.number="form.dedup_ttl" type="number" class="form-input" placeholder="3600" />
          </div>
        </div>

        <div class="form-group">
          <label class="form-label">单次最大告警条数</label>
          <input v-model.number="form.max_alerts" type="number" class="form-input" style="width: 200px;" placeholder="10" />
        </div>

        <!-- Prometheus 配置 -->
        <h3 style="margin: 20px 0 12px; font-size: 15px; color: var(--text-secondary);">Prometheus 指标配置</h3>
        <div class="form-group">
          <label class="form-label">
            <input type="checkbox" v-model="promEnabled" style="margin-right: 6px;" />
            启用自定义 Prometheus 指标
          </label>
          <div class="form-hint">启用后，每次规则执行会输出自定义指标到 /metrics 端点（内置指标始终输出）</div>
        </div>
        <template v-if="promEnabled">
          <div class="form-row">
            <div class="form-group">
              <label class="form-label">指标名称前缀</label>
              <input v-model="promConfig.metric_name" class="form-input" placeholder="如: g32_resource" />
              <div class="form-hint">Prometheus 指标名前缀，只允许字母数字下划线</div>
            </div>
            <div class="form-group">
              <label class="form-label">静态 Labels (JSON)</label>
              <input v-model="promLabelsStr" class="form-input" placeholder='{"namespace":"g32-uat","env":"prod"}' />
            </div>
          </div>
          <div class="form-group">
            <label class="form-label">自定义指标 (JSON)</label>
            <textarea v-model="promCustomMetricsStr" class="form-textarea" rows="4"
              :placeholder="promMetricsPlaceholder"></textarea>
            <div class="form-hint">name=指标后缀, help=描述, type=gauge/counter, value_from=从提取字段获取数值</div>
          </div>
        </template>

        <!-- Submit -->
        <div class="modal-footer" style="border-top: none; padding-top: 24px;">
          <button type="button" class="btn btn-outline" @click="handlePreview" :disabled="previewing">
            {{ previewing ? '查询中...' : '预览告警结果' }}
          </button>
          <button type="button" class="btn btn-warning" @click="handleTestSend" :disabled="testSending">
            {{ testSending ? '发送中...' : '测试发送到 Lark' }}
          </button>
          <router-link to="/alert-rules" class="btn btn-outline">取消</router-link>
          <button type="submit" class="btn btn-primary" :disabled="submitting">
            {{ submitting ? '保存中...' : (isEdit ? '更新规则' : '创建规则') }}
          </button>
        </div>
      </form>
    </div>

    <!-- Preview Modal -->
    <div v-if="previewData" class="modal-overlay" @click.self="previewData = null">
      <div class="modal" style="min-width: 800px; max-width: 95vw; max-height: 90vh; display: flex; flex-direction: column;">
        <div class="modal-header" style="position: sticky; top: 0; background: var(--bg-card, #fff); z-index: 10; flex-shrink: 0;">
          <div class="modal-title">告警预览结果</div>
          <button class="btn-icon" @click="previewData = null"><X :size="18" /></button>
        </div>

        <div style="overflow-y: auto; flex: 1; padding: 0 4px;">
        <div style="display: grid; grid-template-columns: 1fr 1fr 1fr; gap: 12px; margin-bottom: 16px;">
          <div class="stat-card" style="padding: 12px;">
            <div class="label">数据源</div>
            <div style="font-weight: 600;">{{ previewData.source_name }}</div>
          </div>
          <div class="stat-card" style="padding: 12px;">
            <div class="label">详情</div>
            <div style="font-weight: 600;">{{ previewData.source_detail }}</div>
          </div>
          <div class="stat-card" style="padding: 12px;">
            <div class="label">{{ previewData.group_by ? '分组数 / 总日志' : '命中数' }}</div>
            <div style="font-weight: 600; font-size: 20px;" :style="{ color: previewData.hit_count > 0 ? 'var(--warning)' : 'var(--success)' }">
              {{ previewData.group_by ? previewData.group_count + ' 组' : previewData.hit_count }} / {{ previewData.total }}
            </div>
          </div>
        </div>

        <!-- Alert mode hint -->
        <div v-if="form.alert_mode === 'not_found'" class="card" style="padding: 12px; margin-bottom: 12px;"
          :style="{ background: previewData.hit_count === 0 ? '#fef2f2' : '#ecfdf5', borderColor: previewData.hit_count === 0 ? '#fecaca' : '#a7f3d0' }">
          <strong>{{ previewData.hit_count === 0 ? '将触发告警' : '正常（不会告警）' }}</strong>
          — 反向模式: {{ previewData.hit_count === 0 ? '在指定时间范围内未搜到匹配日志' : '搜到匹配日志，不会触发告警' }}
        </div>
        <div v-else-if="previewData.hit_count > 0" class="card" style="padding: 12px; margin-bottom: 12px; background: #fffbeb; border-color: #fde68a;">
          <strong>将触发 {{ previewData.hit_count }} 条告警</strong>
        </div>
        <div v-else class="card" style="padding: 12px; margin-bottom: 12px; background: #ecfdf5; border-color: #a7f3d0;">
          <strong>正常（无命中，不会告警）</strong>
        </div>

        <!-- Rendered messages -->
        <div v-if="previewData.hits && previewData.hits.length > 0">
          <h4 style="margin-bottom: 8px;">渲染后的告警消息</h4>
          <div v-for="(hit, idx) in previewData.hits" :key="idx" class="card" style="margin-bottom: 8px; padding: 12px;">
            <div class="flex justify-between items-center" style="margin-bottom: 8px;">
              <span class="badge badge-info">{{ hit.vars?._group_key ? hit.vars._group_key : '命中 #' + (idx + 1) }}</span>
              <button class="btn btn-sm btn-outline" @click="hit._showRaw = !hit._showRaw">
                {{ hit._showRaw ? '隐藏原始数据' : '查看原始数据' }}
              </button>
            </div>
            <pre style="background: #f1f5f9; padding: 12px; border-radius: 6px; font-size: 13px; white-space: pre-wrap; max-height: 200px; overflow-y: auto;">{{ hit.rendered }}</pre>
            <div v-if="hit._showRaw" style="margin-top: 8px;">
              <div class="text-sm text-secondary" style="margin-bottom: 4px;">提取变量:</div>
              <pre style="background: #f8fafc; padding: 8px; border-radius: 4px; font-size: 12px; max-height: 150px; overflow-y: auto;">{{ formatVars(hit.vars) }}</pre>
              <div class="text-sm text-secondary" style="margin: 4px 0;">ES 原始数据:</div>
              <pre style="background: #f8fafc; padding: 8px; border-radius: 4px; font-size: 12px; max-height: 200px; overflow-y: auto;">{{ formatJSON(hit.raw) }}</pre>
            </div>
          </div>
        </div>

        <!-- ES Query -->
        <details style="margin-top: 12px;">
          <summary class="text-sm text-secondary" style="cursor: pointer;">查看查询语句</summary>
          <pre style="background: #f1f5f9; padding: 12px; border-radius: 6px; font-size: 12px; margin-top: 8px; max-height: 200px; overflow-y: auto;">{{ previewData.query }}</pre>
        </details>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import api from '../api'
import { useToast, useConfirm } from '../stores/ui'
import { X } from 'lucide-vue-next'

const toast = useToast()
const dialog = useConfirm()
const previewing = ref(false)
const previewData = ref(null)
const testSending = ref(false)

const route = useRoute()
const router = useRouter()
const isEdit = computed(() => !!route.params.id)
const submitting = ref(false)

const esConnections = ref([])
const lokiConnections = ref([])
const larkConfigs = ref([])

const form = ref({
  data_source_type: 'es',
  loki_connection_id: 0,
  logql: '',
  name: '',
  es_connection_id: 0,
  lark_config_id: 0,
  es_index: '*',
  schedule: '*/5 * * * *',
  time_range: '5m',
  query_dsl: '',
  keyword: '',
  filter_fields: '',
  extract_fields: '',
  message_title: '',
  message_template: '',
  at_users: '',
  at_all: 0,
  alert_mode: 'found',
  recovery_enabled: 0,
  recovery_title: '',
  recovery_template: '',
  severity: 'S2',
  group_by: '',
  expected_groups: '',
  query_concurrency: 5,
  dedup_field: '',
  dedup_ttl: 3600,
  max_alerts: 10,
  prometheus_config: ''
})

const recoveryChecked = computed({
  get: () => form.value.recovery_enabled === 1,
  set: (v) => { form.value.recovery_enabled = v ? 1 : 0 }
})

// Prometheus config helpers
const promEnabled = ref(false)
const promConfig = ref({ metric_name: '', labels: {}, custom_metrics: [] })
const promLabelsStr = ref('')
const promCustomMetricsStr = ref('')

const promMetricsPlaceholder = `[
  {"name":"hit_count","help":"ES搜索命中数","type":"gauge","value_from":""},
  {"name":"error_count","help":"错误日志数","type":"counter","value_from":"error_count"}
]`

// Sync prometheus config to form.prometheus_config before submit
function syncPromConfig() {
  if (!promEnabled.value) {
    form.value.prometheus_config = ''
    return
  }
  try { promConfig.value.labels = JSON.parse(promLabelsStr.value || '{}') } catch { promConfig.value.labels = {} }
  try { promConfig.value.custom_metrics = JSON.parse(promCustomMetricsStr.value || '[]') } catch { promConfig.value.custom_metrics = [] }
  promConfig.value.enabled = true
  form.value.prometheus_config = JSON.stringify(promConfig.value)
}

// Load prometheus config from form data
function loadPromConfig(configStr) {
  if (!configStr) return
  try {
    const cfg = JSON.parse(configStr)
    promEnabled.value = cfg.enabled || false
    promConfig.value = { metric_name: cfg.metric_name || '', labels: cfg.labels || {}, custom_metrics: cfg.custom_metrics || [] }
    promLabelsStr.value = JSON.stringify(cfg.labels || {})
    promCustomMetricsStr.value = cfg.custom_metrics?.length ? JSON.stringify(cfg.custom_metrics, null, 2) : ''
  } catch { /* ignore */ }
}

function selectGroupBy(e) {
  if (e.target.value) {
    form.value.group_by = e.target.value
    e.target.value = ''
  }
}

const atAllChecked = computed({
  get: () => form.value.at_all === 1,
  set: (v) => { form.value.at_all = v ? 1 : 0 }
})

const extractFieldsPlaceholder = `[
  {"name":"namespace","path":"kubernetes.namespace","pattern":""},
  {"name":"round","path":"message","pattern":"Round:\\\\s*(\\\\S+)"},
  {"name":"link","path":"message","pattern":"Link-(\\\\S+)"}
]`

const templatePlaceholder = `**Namespace:** {{.namespace}}
**Container:** {{.container}}
**Round:** {{.round}}
**Message:** {{.message}}
**Time:** {{.time}}`

// Cron 表达式可读化
function cronToHuman(cron) {
  if (!cron) return ''
  const parts = cron.trim().split(/\s+/)
  if (parts.length !== 5) return cron
  const [min, hour, dom, mon, dow] = parts
  if (min.startsWith('*/') && hour === '*') return `每 ${min.slice(2)} 分钟执行`
  if (min !== '*' && hour.startsWith('*/')) return `每 ${hour.slice(2)} 小时的第 ${min} 分钟执行`
  if (min !== '*' && hour !== '*' && dom === '*') return `每天 ${hour}:${min.padStart(2,'0')} 执行`
  return cron
}

const cronHint = computed(() => cronToHuman(form.value.schedule))

async function loadOptions() {
  try {
    const [esRes, lokiRes, larkRes] = await Promise.all([
      api.get('/es-connections'),
      api.get('/loki-connections'),
      api.get('/lark-configs')
    ])
    if (esRes.code === 0) esConnections.value = esRes.data
    if (lokiRes.code === 0) lokiConnections.value = lokiRes.data
    if (larkRes.code === 0) larkConfigs.value = larkRes.data
  } catch (e) { /* ignore */ }
}

async function loadRule() {
  if (!route.params.id) return
  try {
    const res = await api.get(`/alert-rules/${route.params.id}`)
    if (res.code === 0) {
      const d = res.data
      form.value = {
        data_source_type: d.data_source_type || 'es',
        name: d.name,
        es_connection_id: d.es_connection_id,
        loki_connection_id: d.loki_connection_id || 0,
        lark_config_id: d.lark_config_id,
        es_index: d.es_index,
        schedule: d.schedule,
        time_range: d.time_range,
        query_dsl: d.query_dsl || '',
        keyword: d.keyword,
        logql: d.logql || '',
        filter_fields: d.filter_fields || '',
        extract_fields: d.extract_fields || '',
        message_title: d.message_title,
        message_template: d.message_template || '',
        at_users: d.at_users || '',
        at_all: d.at_all,
        alert_mode: d.alert_mode || 'found',
        recovery_enabled: d.recovery_enabled || 0,
        recovery_title: d.recovery_title || '',
        recovery_template: d.recovery_template || '',
        severity: d.severity,
        group_by: d.group_by || '',
        expected_groups: d.expected_groups || '',
        query_concurrency: d.query_concurrency || 5,
        dedup_field: d.dedup_field,
        dedup_ttl: d.dedup_ttl,
        max_alerts: d.max_alerts,
        prometheus_config: d.prometheus_config || ''
      }
      loadPromConfig(d.prometheus_config)
    }
  } catch (e) { /* ignore */ }
}

async function handleSubmit() {
  syncPromConfig()
  submitting.value = true
  try {
    const data = { ...form.value }
    let res
    if (isEdit.value) {
      res = await api.put(`/alert-rules/${route.params.id}`, data)
    } else {
      res = await api.post('/alert-rules', data)
    }
    if (res.code === 0) {
      router.push('/alert-rules')
    } else {
      toast.error(res.message || '保存失败')
    }
  } catch (e) {
    toast.error('保存失败: ' + (e.response?.data?.message || e.message))
  }
  submitting.value = false
}

async function handleTestSend() {
  const ds = form.value.data_source_type || 'es'
  if (ds === 'es' && !form.value.es_connection_id) { toast.error('请先选择 ES 连接'); return }
  if (ds === 'loki' && !form.value.loki_connection_id) { toast.error('请先选择 Loki 连接'); return }
  if (!form.value.lark_config_id) { toast.error('请先选择 Lark 配置'); return }

  const ok = await dialog.confirm({ title: '测试发送', message: '将查询数据源并真实发送一条告警到 Lark，确认？' })
  if (!ok) return

  testSending.value = true
  try {
    const res = await api.post('/alert-rules/test-send', form.value)
    if (res.code === 0) {
      if (res.data.would_alert === false) {
        let msg = res.data.message
        if (res.data.found_groups) {
          msg += '\n\n正常容器: ' + res.data.found_groups.join(', ')
        }
        toast.info(msg)
      } else {
        toast.success(res.data.response ? `测试发送成功！命中 ${res.data.hit_count} 条` : res.data.message)
      }
    } else {
      toast.error(res.message)
    }
  } catch (e) {
    toast.error('测试发送失败: ' + (e.response?.data?.message || e.message))
  }
  testSending.value = false
}

async function handlePreview() {
  const ds = form.value.data_source_type || 'es'
  if (ds === 'es' && !form.value.es_connection_id) {
    toast.error('请先选择 ES 连接')
    return
  }
  previewing.value = true
  try {
    const res = await api.post('/alert-rules/preview', form.value)
    if (res.code === 0) {
      // Add _showRaw toggle to each hit
      if (res.data.hits) {
        res.data.hits.forEach(h => { h._showRaw = false })
      }
      previewData.value = res.data
    } else {
      toast.error(res.message || '预览失败')
    }
  } catch (e) {
    toast.error('预览失败: ' + (e.response?.data?.message || e.message))
  }
  previewing.value = false
}

function formatVars(vars) {
  if (!vars) return ''
  const filtered = {}
  for (const [k, v] of Object.entries(vars)) {
    if (k !== '_id' && k !== '_index' && typeof v !== 'object') filtered[k] = v
  }
  return JSON.stringify(filtered, null, 2)
}

function formatJSON(obj) {
  try { return JSON.stringify(obj, null, 2) } catch { return String(obj) }
}

onMounted(async () => {
  await loadOptions()
  await loadRule()

  // Pre-fill from query params (from ES Explore page)
  const q = route.query
  if (!isEdit.value && q.es_connection_id) {
    form.value.es_connection_id = parseInt(q.es_connection_id) || 0
    if (q.es_index) form.value.es_index = q.es_index
    if (q.keyword) form.value.keyword = q.keyword
    if (q.filter_fields) form.value.filter_fields = q.filter_fields
    if (q.time_range) form.value.time_range = q.time_range
  }
})
</script>
