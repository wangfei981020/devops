<template>
  <!-- 无权限：不是故障，所以用 warning、不给重试按钮（重试一百次也还是没权限） -->
  <el-alert v-if="denied" type="warning" show-icon :closable="false" class="load-error">
    <template #title><span>无权查看这部分数据</span></template>
    <div class="detail">{{ error }}</div>
    <div class="hint">
      这块内容需要额外的 CMDB 权限。需要开通请联系管理员，在运维平台「角色管理」里为你的角色勾选对应权限。
    </div>
  </el-alert>

  <el-alert v-else-if="error" type="error" show-icon :closable="false" class="load-error">
    <template #title>
      <span>{{ title }}</span>
    </template>
    <div class="detail">{{ error }}</div>
    <div class="hint">页面上的数字与结论本次都没有取到，<b>请勿据此认为没有问题</b>。</div>
    <el-button size="small" type="primary" plain style="margin-top:8px" @click="$emit('retry')">重试</el-button>
  </el-alert>
</template>

<script setup>
// 加载失败横幅。故意做得显眼：它替代的是「静默显示空数据」这种最危险的表现，
// 见 CMDB-013。任何用它的页面，在 error 非空时都不应再渲染统计卡与结论文案。
//
// 「没有权限」和「加载失败」必须分开说：前者是配置问题、重试无用，
// 后者是故障、值得重试。都套一个红色"请勿据此认为没有问题"，
// 会让正常的权限边界看起来像系统坏了。
import { computed } from 'vue'

const props = defineProps({
  error: { type: String, default: '' },
  title: { type: String, default: '数据加载失败' },
})
defineEmits(['retry'])

// 依据 normalizeError 对 403 的固定措辞判定（见 api/http.js）
const denied = computed(() => !!props.error && props.error.includes('没有权限'))
</script>

<style scoped>
.load-error { margin-bottom: 12px; }
.detail { font-size: 13px; word-break: break-all; }
.hint { font-size: 12px; margin-top: 4px; opacity: .85; }
</style>
