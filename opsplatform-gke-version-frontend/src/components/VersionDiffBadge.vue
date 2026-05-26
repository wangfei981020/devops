<template>
  <el-tag :type="tagType" disable-transitions>
    落后 {{ behind }} 个 / 算术差 {{ diff.toFixed(4) }}
  </el-tag>
</template>
<script setup>
import { computed } from 'vue'
const props = defineProps({
  behind: { type: Number, default: 0 },
  diff: { type: Number, default: 0 },
})
const tagType = computed(() => {
  if (props.behind === 0) return 'success'
  if (props.behind <= 5) return 'warning'
  return 'danger'
})
</script>
