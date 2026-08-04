<template>
  <div class="page">
    <el-result icon="warning" title="无权访问该页面"
               :sub-title="from ? `你的账号没有被授予「${from}」的访问权限` : '你的账号没有被授予该页面的访问权限'">
      <template #extra>
        <div class="tip">
          CMDB 的权限由运维平台统一分配。需要开通请联系管理员，
          在运维平台「角色管理」里为你的角色勾选对应的 CMDB 菜单权限。
        </div>
        <el-button type="primary" @click="$router.push('/overview')">返回总览</el-button>
        <el-button :loading="refreshing" @click="refresh">刷新我的权限</el-button>
      </template>
    </el-result>
  </div>
</template>

<script setup>
// 无权限页。不能让没权限的页面渲染成空白或满屏 403 红条——
// 那样用户分不清"我没权限"和"这功能坏了"（全站三态约定：失败态禁止退化成空态）。
//
// 带一个刷新按钮：管理员刚给完权限，用户不必退出重登，点一下就能进。
import { ref, computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { useAuthStore } from '../stores/auth'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const from = computed(() => route.query.from || '')
const refreshing = ref(false)

async function refresh() {
  refreshing.value = true
  try {
    await auth.refreshPermissions()
    if (from.value) {
      // 刷新后真拿到权限了就直接回去，否则留在本页并说明还是没有
      const page = from.value
      await router.push(page)
      return
    }
    ElMessage.success('权限已刷新')
  } catch (_) {
    ElMessage.error('刷新权限失败，请稍后重试或重新登录')
  } finally {
    refreshing.value = false
  }
}
</script>

<style scoped>
.tip { color: #909399; font-size: 13px; margin-bottom: 12px; max-width: 520px; line-height: 1.7; }
</style>
