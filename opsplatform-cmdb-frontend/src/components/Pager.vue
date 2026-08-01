<template>
  <div class="pager" v-if="total > pageSize">
    <el-pagination layout="total, sizes, prev, pager, next" :total="total"
      :current-page="page" :page-size="pageSize" :page-sizes="[20,50,100]" size="small" background
      @update:current-page="$emit('update:page', $event)" @update:page-size="$emit('update:pageSize', $event)" />
  </div>
</template>
<script setup>
defineProps({ total: Number, page: Number, pageSize: Number })
defineEmits(['update:page', 'update:pageSize'])
</script>
<style scoped>
.pager { display: flex; justify-content: flex-end; margin-top: 12px; }
</style>
