<template>
  <div>
    <div style="margin-bottom:16px;display:flex;gap:8px;">
      <el-button type="primary" @click="openDlg()">添加告警规则</el-button>
      <el-button @click="historyDlg = true">查看告警历史</el-button>
    </div>
    <el-table :data="list" v-loading="loading" stripe>
      <el-table-column prop="name" label="规则名" min-width="180" />
      <el-table-column label="对象" width="100">
        <template #default="{ row }">
          <el-tag size="small">{{ row.target === 'cluster' ? '集群' : '节点池' }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="阈值" width="160">
        <template #default="{ row }">落后 ≥ {{ row.versions_behind_threshold }} 个</template>
      </el-table-column>
      <el-table-column label="生效集群" min-width="160">
        <template #default="{ row }">
          <span v-if="!row.cluster_ids || row.cluster_ids.length === 0">全部</span>
          <span v-else>{{ row.cluster_ids.length }} 个</span>
        </template>
      </el-table-column>
      <el-table-column label="发送到" min-width="140">
        <template #default="{ row }">{{ webhookName(row.webhook_id) }}</template>
      </el-table-column>
      <el-table-column label="@ 通知人" min-width="160">
        <template #default="{ row }">
          <el-tag v-for="uid in row.mention_user_ids" :key="uid" size="small" style="margin-right:4px">{{ userName(uid) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="间隔" width="110">
        <template #default="{ row }">{{ row.interval_minutes }} 分钟</template>
      </el-table-column>
      <el-table-column label="启用" width="80">
        <template #default="{ row }">
          <el-tag :type="row.enabled === 1 ? 'success' : 'info'" size="small">{{ row.enabled === 1 ? '启用' : '禁用' }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="操作" width="160" fixed="right">
        <template #default="{ row }">
          <el-button size="small" @click="openDlg(row)">编辑</el-button>
          <el-button size="small" type="danger" @click="onDelete(row)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <!-- 编辑/添加弹窗 -->
    <el-dialog v-model="dlg" :title="form.id ? '编辑告警规则' : '添加告警规则'" width="720px">
      <el-form :model="form" label-width="130px">
        <el-form-item label="规则名"><el-input v-model="form.name" /></el-form-item>
        <el-form-item label="对象">
          <el-radio-group v-model="form.target">
            <el-radio value="cluster">集群</el-radio>
            <el-radio value="nodepool">节点池</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="落后版本数阈值">
          <el-input-number v-model="form.versions_behind_threshold" :min="1" :max="999" />
          <span style="color:#999;font-size:12px;margin-left:8px">落后 ≥N 个版本时触发</span>
        </el-form-item>
        <el-form-item label="生效集群">
          <el-select v-model="form.cluster_ids" multiple filterable placeholder="留空表示对全部集群生效" style="width:100%">
            <el-option v-for="c in clusters" :key="c.id" :label="`${c.name} (${c.project_id}/${c.location})`" :value="c.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="Lark Webhook">
          <el-select v-model="form.webhook_id" placeholder="选择发送到哪个群" style="width:100%">
            <el-option v-for="w in webhooks" :key="w.id" :label="w.name" :value="w.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="@ 通知人">
          <el-select v-model="form.mention_user_ids" multiple filterable placeholder="选择要 @ 的通知人" style="width:100%">
            <el-option v-for="u in users" :key="u.id" :label="`${u.name}${u.remark ? ' (' + u.remark + ')' : ''}`" :value="u.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="去重间隔">
          <el-input-number v-model="form.interval_minutes" :min="5" :max="1440" />
          <span style="color:#999;font-size:12px;margin-left:8px">同一对象 N 分钟内只发一次</span>
        </el-form-item>
        <el-form-item label="启用"><el-switch v-model="form.enabled_bool" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dlg = false">取消</el-button>
        <el-button type="primary" @click="save">保存</el-button>
      </template>
    </el-dialog>

    <!-- 告警历史 -->
    <el-dialog v-model="historyDlg" title="告警历史（最近 200 条）" width="1000px">
      <el-table :data="history" stripe size="small" height="500">
        <el-table-column label="时间" width="170">
          <template #default="{ row }">{{ formatTime(row.trigger_time) }}</template>
        </el-table-column>
        <el-table-column label="规则" width="160">
          <template #default="{ row }">{{ ruleName(row.rule_id) }}</template>
        </el-table-column>
        <el-table-column label="集群" width="220">
          <template #default="{ row }">{{ clusterName(row.cluster_id) }}</template>
        </el-table-column>
        <el-table-column prop="nodepool_name" label="节点池" width="200" />
        <el-table-column label="落后" width="80">
          <template #default="{ row }">{{ row.versions_behind }}</template>
        </el-table-column>
        <el-table-column label="状态" width="80">
          <template #default="{ row }">
            <el-tag :type="row.status === 'sent' ? 'success' : 'danger'" size="small">{{ row.status }}</el-tag>
          </template>
        </el-table-column>
      </el-table>
    </el-dialog>
  </div>
</template>
<script setup>
import { ref, reactive, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { useAppStore } from '../stores/app'
import { listAlertRules, createAlertRule, updateAlertRule, deleteAlertRule, listAlertHistory } from '../api/alert_rules'
import { listClusters } from '../api/clusters'
import { listWebhooks } from '../api/lark_webhooks'
import { listNotifyUsers } from '../api/notify_users'

const list = ref([])
const clusters = ref([])
const webhooks = ref([])
const users = ref([])
const history = ref([])
const loading = ref(false)
const dlg = ref(false)
const historyDlg = ref(false)
const app = useAppStore()

const form = reactive({
  id: null,
  name: '',
  target: 'cluster',
  versions_behind_threshold: 5,
  cluster_ids: [],
  webhook_id: null,
  mention_user_ids: [],
  interval_minutes: 60,
  enabled: 1,
  enabled_bool: true,
})

async function load() {
  loading.value = true
  try {
    const [r, c, w, u] = await Promise.all([listAlertRules(), listClusters(), listWebhooks(), listNotifyUsers()])
    list.value = r
    clusters.value = c
    webhooks.value = w
    users.value = u
  } finally {
    loading.value = false
  }
}
function openDlg(row) {
  if (row) {
    Object.assign(form, {
      id: row.id,
      name: row.name,
      target: row.target,
      versions_behind_threshold: row.versions_behind_threshold,
      cluster_ids: row.cluster_ids || [],
      webhook_id: row.webhook_id,
      mention_user_ids: row.mention_user_ids || [],
      interval_minutes: row.interval_minutes,
      enabled_bool: row.enabled === 1,
    })
  } else {
    Object.assign(form, {
      id: null, name: '', target: 'cluster', versions_behind_threshold: 5,
      cluster_ids: [], webhook_id: webhooks.value[0]?.id || null, mention_user_ids: [],
      interval_minutes: 60, enabled_bool: true,
    })
  }
  dlg.value = true
}
async function save() {
  if (!form.webhook_id) {
    ElMessage.error('请先选择 Lark Webhook')
    return
  }
  try {
    const body = {
      name: form.name,
      target: form.target,
      versions_behind_threshold: form.versions_behind_threshold,
      cluster_ids: form.cluster_ids,
      webhook_id: form.webhook_id,
      mention_user_ids: form.mention_user_ids,
      interval_minutes: form.interval_minutes,
      enabled: form.enabled_bool ? 1 : 0,
    }
    if (form.id) await updateAlertRule(form.id, body)
    else await createAlertRule(body)
    dlg.value = false
    ElMessage.success('保存成功')
    await load()
  } catch (e) {
    ElMessage.error('保存失败：' + (e.response?.data?.error || e.message))
  }
}
async function onDelete(row) {
  if (!await app.showConfirm(`删除告警规则 ${row.name}？`)) return
  try {
    await deleteAlertRule(row.id)
    ElMessage.success('已删除')
    await load()
  } catch (e) {
    ElMessage.error('删除失败：' + (e.response?.data?.error || e.message))
  }
}
async function openHistory() {
  history.value = await listAlertHistory()
}

function webhookName(id) { return webhooks.value.find(w => w.id === id)?.name || '-' }
function userName(id) { return users.value.find(u => u.id === id)?.name || `#${id}` }
function clusterName(id) { return clusters.value.find(c => c.id === id)?.name || `#${id}` }
function ruleName(id) { return list.value.find(r => r.id === id)?.name || `#${id}` }
function formatTime(s) { return s ? new Date(s).toLocaleString('zh-CN') : '-' }

// 历史弹窗打开时拉数据
import { watch } from 'vue'
watch(historyDlg, (v) => { if (v) openHistory() })

onMounted(load)
</script>
