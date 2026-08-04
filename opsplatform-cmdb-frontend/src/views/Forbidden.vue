<template>
  <div class="page">
    <el-result icon="warning" title="无权访问该页面"
               :sub-title="from ? `你的账号没有被授予「${from}」的访问权限` : '你的账号没有被授予该页面的访问权限'">
      <template #extra>
        <div class="tip">
          CMDB 的权限由运维平台统一分配。需要开通请联系管理员，
          在运维平台「角色管理」里为你的角色勾选对应的 CMDB 菜单权限。
        </div>
        <!-- 按钮文案跟着实际去处走：没有总览权限时写死"返回总览"是骗人的，
             点了还会弹回这一页 -->
        <el-button type="primary" v-if="homePath !== '/forbidden'" @click="$router.replace(homePath)">
          {{ homeLabel }}
        </el-button>
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
import { firstAllowedPath, permOf } from '../router'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const from = computed(() => route.query.from || '')
const refreshing = ref(false)

// 去处按"这个人实际进得去哪"算，不写死 /overview——
// 没有总览权限的人点「返回总览」会被弹回本页，怎么点都出不去。
const homePath = computed(() => firstAllowedPath(auth))
const MENU_LABEL = {
  '/overview': '总览', '/hosts': '主机', '/domains': '域名', '/certs': '证书',
  '/k8s-clusters': 'K8s 集群', '/event-center': '事件中心', '/alerts': '告警',
}
const homeLabel = computed(() =>
  homePath.value === '/overview' ? '返回总览' : `去${MENU_LABEL[homePath.value] || '我有权限的页面'}`)

async function refresh() {
  refreshing.value = true
  try {
    await auth.refreshPermissions()
    const target = from.value
    // 刷新后**确认真拿到权限了**再回去。原来无条件 push，没拿到权限就会被
    // 守卫再弹回本页，看起来像"点了没反应"——用户不知道是刷新失败还是权限没给。
    if (target && auth.hasMenu(permOf(target))) {
      ElMessage.success('权限已生效')
      await router.replace(target)
      return
    }
    ElMessage.warning(target
      ? `权限已刷新，但仍然没有「${target}」的访问权限，请联系管理员开通`
      : '权限已刷新')
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
