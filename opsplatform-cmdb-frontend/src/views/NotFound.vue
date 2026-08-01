<template>
  <div class="page">
    <el-result icon="warning" title="页面不存在" :sub-title="`没有找到路径 ${path} 对应的页面`">
      <template #extra>
        <div class="tip">检查一下地址有没有敲错，或者从左侧菜单进入。</div>
        <el-button type="primary" @click="$router.push('/overview')">返回总览</el-button>
        <el-button @click="$router.back()">返回上一页</el-button>
      </template>
    </el-result>
  </div>
</template>

<script setup>
// 404 页。此前访问不存在的路由既不跳转也不提示，渲染出一个空白页 + 面包屑写着「总览」，
// 看起来像"总览页坏了"而不是"这个地址不存在"（CMDB-018）。
import { computed } from 'vue'
import { useRoute } from 'vue-router'

const route = useRoute()
const path = computed(() => route.fullPath)
</script>

<style scoped>
.tip { color: #909399; font-size: 13px; margin-bottom: 12px; }
</style>
