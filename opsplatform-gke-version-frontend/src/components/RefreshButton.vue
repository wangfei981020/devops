<template>
  <el-button :icon="Refresh" :loading="loading" size="small" @click="onClick">{{ label }}</el-button>
</template>
<script setup>
import { ref } from 'vue'
import { Refresh } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { refresh } from '../api/refresh'

const props = defineProps({
  label: { type: String, default: '刷新' },
  clusterIds: { type: Array, default: () => [] },
  all: Boolean,
})
const emit = defineEmits(['done'])
const loading = ref(false)
async function onClick() {
  if (!props.all && (!props.clusterIds || props.clusterIds.length === 0)) {
    ElMessage.warning('请先选择集群')
    return
  }
  loading.value = true
  try {
    await refresh(props.all ? { all: true } : { cluster_ids: props.clusterIds })
    ElMessage.success('已触发刷新，几秒后数据更新')
    emit('done')
  } catch (e) {
    ElMessage.error('刷新失败：' + (e.response?.data?.error || e.message))
  } finally {
    loading.value = false
  }
}
</script>
