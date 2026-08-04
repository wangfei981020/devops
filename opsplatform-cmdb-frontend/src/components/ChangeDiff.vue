<template>
  <div v-if="!fields.length" class="empty">这次操作没有产生字段级变更</div>
  <table v-else class="diff">
    <thead>
      <tr><th style="width:180px">字段</th><th>变更前</th><th>变更后</th></tr>
    </thead>
    <tbody>
      <tr v-for="f in fields" :key="f.name">
        <td class="fname">{{ f.name }}</td>
        <template v-if="f.masked">
          <td colspan="2" class="masked">
            <el-tag size="small" type="warning" effect="plain">已变更</el-tag>
            <span class="tip">敏感字段不展示具体值</span>
          </td>
        </template>
        <template v-else>
          <td class="old"><span v-if="f.old !== null && f.old !== ''">{{ f.old }}</span><i v-else>（空）</i></td>
          <td class="new"><span v-if="f.new !== null && f.new !== ''">{{ f.new }}</span><i v-else>（空）</i></td>
        </template>
      </tr>
    </tbody>
  </table>
</template>

<script setup>
// 字段级 diff 展示。后端已经做过两件事，这里只负责呈现：
//   1. 没变化的字段根本不会出现——列出 40 个没动过的字段等于什么都没说
//   2. 敏感字段（密码/token/凭据）只给 {changed:true}，值不出库
import { computed } from 'vue'

const props = defineProps({ diff: { type: Object, default: () => ({}) } })

const fields = computed(() => {
  const d = props.diff || {}
  return Object.keys(d).sort().map((name) => {
    const v = d[name] || {}
    return { name, masked: v.changed === true, old: fmt(v.old), new: fmt(v.new) }
  })
})

function fmt(v) {
  if (v === null || v === undefined) return ''
  if (typeof v === 'object') return JSON.stringify(v)
  return String(v)
}
</script>

<style scoped>
.diff { width: 100%; border-collapse: collapse; font-size: 13px; }
.diff th { text-align: left; padding: 6px 10px; background: #f5f7fa; color: #606266; font-weight: 500; border-bottom: 1px solid #ebeef5; }
.diff td { padding: 6px 10px; border-bottom: 1px solid #f2f4f7; vertical-align: top; word-break: break-all; }
.fname { color: #303133; font-family: ui-monospace, Menlo, monospace; }
.old { background: #fef0f0; color: #a8323b; }
.new { background: #f0f9eb; color: #2f7d31; }
.old i, .new i { color: #c0c4cc; font-style: normal; }
.masked { color: #909399; }
.masked .tip { margin-left: 8px; font-size: 12px; }
.empty { color: #909399; font-size: 13px; padding: 8px 0; }
</style>
