<template>
  <span :style="{ color }">{{ date || '-' }}</span>
</template>
<script setup>
import { computed } from 'vue'
const props = defineProps({ date: String })
const color = computed(() => {
  if (!props.date) return '#999'
  const days = Math.floor((new Date(props.date) - new Date()) / 86400000)
  if (days <= 30) return '#f56c6c'
  if (days <= 90) return '#e6a23c'
  return '#67c23a'
})
</script>
