<template>
  <el-alert v-if="error" type="error" show-icon :closable="false" class="load-error">
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
defineProps({
  error: { type: String, default: '' },
  title: { type: String, default: '数据加载失败' },
})
defineEmits(['retry'])
</script>

<style scoped>
.load-error { margin-bottom: 12px; }
.detail { font-size: 13px; word-break: break-all; }
.hint { font-size: 12px; margin-top: 4px; opacity: .85; }
</style>
