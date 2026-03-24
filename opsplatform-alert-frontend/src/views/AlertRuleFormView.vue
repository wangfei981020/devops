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
              <option value="info">信息 (蓝色)</option>
              <option value="warning">警告 (橙色)</option>
              <option value="critical">严重 (红色)</option>
            </select>
          </div>
        </div>

        <div class="form-row">
          <div class="form-group">
            <label class="form-label">ES 连接 *</label>
            <select v-model="form.es_connection_id" class="form-select" required>
              <option :value="0" disabled>请选择 ES 连接</option>
              <option v-for="c in esConnections" :key="c.id" :value="c.id">
                {{ c.name }} ({{ c.version }}.x)
              </option>
            </select>
          </div>
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

        <div class="form-row">
          <div class="form-group">
            <label class="form-label">执行周期 (Cron)</label>
            <input v-model="form.schedule" class="form-input" placeholder="*/5 * * * *" />
            <div class="form-hint">Cron 表达式，如 */5 * * * * 每5分钟</div>
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
          <router-link to="/alert-rules" class="btn btn-outline">取消</router-link>
          <button type="submit" class="btn btn-primary" :disabled="submitting">
            {{ submitting ? '保存中...' : (isEdit ? '更新规则' : '创建规则') }}
          </button>
        </div>
      </form>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import api from '../api'
import { useToast } from '../stores/ui'

const toast = useToast()

const route = useRoute()
const router = useRouter()
const isEdit = computed(() => !!route.params.id)
const submitting = ref(false)

const esConnections = ref([])
const larkConfigs = ref([])

const form = ref({
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
  severity: 'warning',
  dedup_field: '',
  dedup_ttl: 3600,
  max_alerts: 10,
  prometheus_config: ''
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

async function loadOptions() {
  try {
    const [esRes, larkRes] = await Promise.all([
      api.get('/es-connections'),
      api.get('/lark-configs')
    ])
    if (esRes.code === 0) esConnections.value = esRes.data.filter(c => c.status === 1)
    if (larkRes.code === 0) larkConfigs.value = larkRes.data.filter(c => c.status === 1)
  } catch (e) { /* ignore */ }
}

async function loadRule() {
  if (!route.params.id) return
  try {
    const res = await api.get(`/alert-rules/${route.params.id}`)
    if (res.code === 0) {
      const d = res.data
      form.value = {
        name: d.name,
        es_connection_id: d.es_connection_id,
        lark_config_id: d.lark_config_id,
        es_index: d.es_index,
        schedule: d.schedule,
        time_range: d.time_range,
        query_dsl: d.query_dsl || '',
        keyword: d.keyword,
        filter_fields: d.filter_fields || '',
        extract_fields: d.extract_fields || '',
        message_title: d.message_title,
        message_template: d.message_template || '',
        at_users: d.at_users || '',
        at_all: d.at_all,
        severity: d.severity,
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

onMounted(() => {
  loadOptions()
  loadRule()
})
</script>
