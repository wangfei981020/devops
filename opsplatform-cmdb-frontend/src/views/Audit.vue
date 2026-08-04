<template>
  <div class="page">
    <div class="page-head">
      <span class="page-title">操作审计</span>
      <div>
        <el-button :icon="Refresh" @click="load">刷新</el-button>
      </div>
    </div>

    <LoadError :error="error" title="审计记录加载失败" @retry="load" />

    <div class="filters">
      <el-input v-model="f.username" placeholder="操作人" clearable style="width:130px" @change="reload" />
      <el-input v-model="f.q" placeholder="对象 / 路径关键字" clearable style="width:190px" @change="reload" />
      <el-select v-model="f.action_prefix" placeholder="对象类型" clearable style="width:150px" @change="reload">
        <el-option v-for="t in actionPrefixes" :key="t.v" :label="t.l" :value="t.v" />
      </el-select>
      <el-select v-model="f.status" placeholder="结果" clearable style="width:120px" @change="reload">
        <el-option label="成功" value="success" />
        <el-option label="失败" value="fail" />
        <el-option label="被拒绝" value="denied" />
      </el-select>
      <el-select v-model="f.actor_source" placeholder="来源" clearable style="width:120px" @change="reload">
        <el-option label="运维平台" value="portal" />
        <el-option label="本地账号" value="local" />
        <el-option label="AI(MCP)" value="mcp" />
        <el-option label="系统" value="system" />
      </el-select>
      <el-date-picker v-model="range" type="datetimerange" value-format="YYYY-MM-DD HH:mm:ss"
                      start-placeholder="开始" end-placeholder="结束" style="width:340px" @change="reload" />
      <el-checkbox v-model="f.changed_only" @change="reload">只看有变更的</el-checkbox>
    </div>

    <el-table :data="rows" v-loading="loading" size="small" border style="width:100%">
      <el-table-column prop="at" label="时间" width="160" />
      <el-table-column prop="username" label="操作人" width="120">
        <template #default="{ row }">
          {{ row.username }}
          <el-tag v-if="row.actor_source === 'mcp'" size="small" type="warning" effect="plain">AI</el-tag>
          <el-tag v-else-if="row.actor_source === 'system'" size="small" type="info" effect="plain">系统</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="action" label="动作" width="200">
        <template #default="{ row }"><code>{{ row.action }}</code></template>
      </el-table-column>
      <el-table-column prop="target" label="对象" min-width="180" show-overflow-tooltip />
      <el-table-column label="结果" width="90">
        <template #default="{ row }">
          <el-tag v-if="row.status === 'success'" size="small" type="success" effect="plain">成功</el-tag>
          <el-tag v-else-if="row.status === 'denied'" size="small" type="warning" effect="plain">被拒</el-tag>
          <el-tag v-else size="small" type="danger" effect="plain">失败</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="变更" width="80" align="center">
        <template #default="{ row }">
          <el-tag v-if="row.change_count" size="small" effect="plain">{{ row.change_count }} 项</el-tag>
          <span v-else class="dim">—</span>
        </template>
      </el-table-column>
      <el-table-column prop="ip" label="来源 IP" width="130" />
      <el-table-column label="操作" width="90" fixed="right">
        <template #default="{ row }">
          <el-button link type="primary" size="small" @click="openDetail(row)">详情</el-button>
        </template>
      </el-table-column>
    </el-table>

    <Pager :total="total" v-model:page="page" v-model:page-size="pageSize" />

    <el-drawer v-model="drawer" :title="cur ? `${cur.action} · ${cur.target || '-'}` : '变更详情'" size="52%">
      <div v-if="cur" class="detail">
        <el-descriptions :column="2" size="small" border>
          <el-descriptions-item label="操作人">{{ cur.username }}</el-descriptions-item>
          <el-descriptions-item label="时间">{{ cur.at }}</el-descriptions-item>
          <el-descriptions-item label="动作"><code>{{ cur.action }}</code></el-descriptions-item>
          <el-descriptions-item label="结果">{{ cur.status }}</el-descriptions-item>
        </el-descriptions>

        <div v-for="ch in cur.changes || []" :key="ch.id" class="chg">
          <div class="chg-head">
            <span class="chg-title">
              <el-tag size="small" :type="opType(ch.op)" effect="plain">{{ opLabel(ch.op) }}</el-tag>
              <code>{{ ch.table }}</code><span v-if="ch.row_pk"> #{{ ch.row_pk }}</span>
            </span>
            <span v-if="ch.reverted_at" class="reverted">
              已于 {{ ch.reverted_at }} 由 {{ ch.reverted_by }} 回滚
            </span>
            <el-tooltip v-else-if="!ch.revertable" :content="ch.revert_blocked_reason" placement="top">
              <el-button size="small" disabled>不可回滚</el-button>
            </el-tooltip>
            <el-button v-else-if="canRevert" size="small" type="warning" plain
                       :loading="reverting === ch.id" @click="doRevert(ch)">回滚这项</el-button>
          </div>
          <ChangeDiff :diff="ch.diff" />
        </div>

        <div v-if="!(cur.changes || []).length" class="dim">
          这次操作没有产生数据行变更（同步、测试连接、手动执行任务这类动作本身不改数据）。
        </div>
      </div>
    </el-drawer>
  </div>
</template>

<script setup>
// 操作审计页。
//
// 关键取舍：列表默认按时间倒序、后端分页——审计表是只增不删的，
// 前端分页在数据攒起来之后必然把浏览器拖垮。
import { ref, reactive, computed, watch, onMounted } from 'vue'
import { Refresh } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { auditLogs, auditDetail, revertChange } from '../api/cmdb'
import { useLoadState } from '../composables/useLoadState'
import { useAppStore } from '../stores/app'
import { useAuthStore } from '../stores/auth'
import LoadError from '../components/LoadError.vue'
import ChangeDiff from '../components/ChangeDiff.vue'
import Pager from '../components/Pager.vue'

const app = useAppStore()
const auth = useAuthStore()
const canRevert = computed(() => auth.hasButton('revert_change'))

const { loading, error, run } = useLoadState()
const rows = ref([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const drawer = ref(false)
const cur = ref(null)
const reverting = ref(0)
const range = ref([])

const f = reactive({ username: '', q: '', action_prefix: '', status: '', actor_source: '', changed_only: false })

const actionPrefixes = [
  { l: '域名', v: 'domain.' }, { l: 'DNS 记录', v: 'dns_record.' }, { l: '解析台账', v: 'record.' },
  { l: '证书', v: 'cert.' }, { l: 'CDN', v: 'cdn' }, { l: '配置项/主机', v: 'ci.' },
  { l: '云账号', v: 'cloud_account.' }, { l: 'K8s 集群', v: 'cluster.' },
  { l: '接入凭据', v: 'registrar.' }, { l: '通知', v: 'lark_group.' },
  { l: '定时任务', v: 'cron.' }, { l: '基础配置', v: 'project.' }, { l: '成本', v: 'cost' },
]

async function load() {
  const params = { page: page.value, page_size: pageSize.value }
  for (const [k, v] of Object.entries(f)) {
    if (v === true) params[k] = 1
    else if (v) params[k] = v
  }
  if (range.value?.length === 2) {
    params.since = range.value[0]
    params.until = range.value[1]
  }
  const r = await run(() => auditLogs(params))
  if (r) { rows.value = r.list || []; total.value = r.total || 0 }
}

function reload() { page.value = 1; load() }
watch([page, pageSize], load)
onMounted(load)

async function openDetail(row) {
  drawer.value = true
  cur.value = null
  try { cur.value = await auditDetail(row.id) } catch (_) { drawer.value = false }
}

function opLabel(op) { return { INSERT: '新建', UPDATE: '修改', DELETE: '删除' }[op] || op }
function opType(op) { return { INSERT: 'success', UPDATE: 'warning', DELETE: 'danger' }[op] || 'info' }

async function doRevert(ch, force = false) {
  try {
    await app.showConfirm(
      force ? '这条记录在那次变更之后又被改过，强制回滚会覆盖后来的改动。确定继续？'
            : `将把 ${ch.table} #${ch.row_pk} 还原到这次变更之前的状态。回滚本身也会留下审计记录。`,
      force ? '强制回滚' : '确认回滚')
  } catch (_) { return } // 用户点了取消
  reverting.value = ch.id
  try {
    await revertChange(ch.id, force)
    ElMessage.success('已回滚')
    await openDetail({ id: cur.value.id })
    load()
  } catch (e) {
    const d = e?.raw?.response?.data
    // 409 = 冲突：把"被谁改过"讲清楚，再让用户决定要不要强制
    if (e?.status === 409 && d?.conflict) {
      const who = d.last_changed_by ? `${d.last_changed_by}（${d.last_changed_at}）` : '其他人'
      const fields = (d.fields || []).join('、')
      try {
        await app.showConfirm(
          `${d.error}。冲突字段：${fields}；最近改动：${who}。仍要强制回滚吗？`, '存在冲突')
      } catch (_) { return }
      return doRevert(ch, true)
    } else {
      ElMessage.error(d?.error || '回滚失败')
      if (d?.hint) ElMessage.info(d.hint)
    }
  } finally {
    reverting.value = 0
  }
}
</script>

<style scoped>
.filters { display: flex; gap: 8px; flex-wrap: wrap; align-items: center; margin-bottom: 12px; }
.detail { padding: 4px 2px; }
.chg { margin-top: 16px; border: 1px solid #ebeef5; border-radius: 6px; overflow: hidden; }
.chg-head { display: flex; align-items: center; gap: 10px; padding: 8px 10px; background: #fafbfc; border-bottom: 1px solid #ebeef5; }
.chg-title { display: flex; align-items: center; gap: 8px; flex: 1; font-size: 13px; }
.reverted { font-size: 12px; color: #909399; }
.dim { color: #909399; font-size: 13px; }
code { background: #f5f7fa; padding: 1px 5px; border-radius: 3px; font-size: 12px; }
</style>
