<template>
  <div class="hist">
    <div v-if="loading" class="dim">加载中…</div>
    <div v-else-if="err" class="err">变更历史加载失败：{{ err }}</div>
    <div v-else-if="!rows.length" class="dim">这个对象还没有被改动过</div>
    <el-timeline v-else>
      <el-timeline-item v-for="r in rows" :key="r.change_id" :timestamp="r.at" placement="top"
                        :type="r.reverted_at ? 'info' : dotType(r.op)">
        <div class="row-head">
          <el-tag size="small" :type="opType(r.op)" effect="plain">{{ opLabel(r.op) }}</el-tag>
          <span class="who">{{ r.username }}</span>
          <code>{{ r.action }}</code>
          <el-tag v-if="r.reverted_at" size="small" type="info" effect="plain">已回滚</el-tag>
        </div>
        <ChangeDiff :diff="r.diff" />
      </el-timeline-item>
    </el-timeline>
  </div>
</template>

<script setup>
// 对象详情页内嵌的「变更历史」。
//
// 为什么要有它：一个孤立的审计菜单，用起来是"我记得这条记录前天被谁改过，
// 去审计页翻吧"——实际没人翻。把历史放在对象自己身上，
// 打开域名/证书/集群详情就能看到它这一路被谁改成了现在的样子。
import { ref, watch, onMounted } from 'vue'
import { auditHistory } from '../api/cmdb'
import ChangeDiff from './ChangeDiff.vue'

const props = defineProps({
  table: { type: String, required: true },   // 审计里的表名，如 domains / certificates
  pk: { type: [String, Number], required: true },
  limit: { type: Number, default: 20 },
})

const rows = ref([])
const loading = ref(false)
const err = ref('')

async function load() {
  if (!props.table || props.pk === undefined || props.pk === null || props.pk === '') return
  loading.value = true
  err.value = ''
  try {
    const r = await auditHistory({ table: props.table, pk: String(props.pk), limit: props.limit })
    rows.value = r.list || []
  } catch (e) {
    // 失败要说出来，不能静默变成"没有变更历史"——那是把故障伪装成正常
    err.value = e?.message || '请求失败'
    rows.value = []
  } finally {
    loading.value = false
  }
}

onMounted(load)
watch(() => [props.table, props.pk], load)
defineExpose({ reload: load })

function opLabel(op) { return { INSERT: '新建', UPDATE: '修改', DELETE: '删除' }[op] || op }
function opType(op) { return { INSERT: 'success', UPDATE: 'warning', DELETE: 'danger' }[op] || 'info' }
function dotType(op) { return { INSERT: 'success', UPDATE: 'warning', DELETE: 'danger' }[op] || 'primary' }
</script>

<style scoped>
.hist { padding: 4px 2px; }
.row-head { display: flex; align-items: center; gap: 8px; margin-bottom: 6px; font-size: 13px; }
.who { color: #303133; font-weight: 500; }
.dim { color: #909399; font-size: 13px; }
.err { color: #f56c6c; font-size: 13px; }
code { background: #f5f7fa; padding: 1px 5px; border-radius: 3px; font-size: 12px; color: #606266; }
</style>
