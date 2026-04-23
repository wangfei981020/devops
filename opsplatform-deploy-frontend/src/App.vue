<template>
  <RouterView />
  <InflightDock />
</template>

<script setup>
import { onMounted, watch } from 'vue'
import InflightDock from './components/InflightDock.vue'
import { useAuthStore } from './stores/auth'
import { useDeploymentsStore } from './stores/deployments'

const auth = useAuthStore()
const deployments = useDeploymentsStore()

// 登录状态确立后，把"我自己最近 30 分钟未完成的发布"自动接管 polling，
// 用户刷新/重新登录都能继续看进度
function recoverIfLoggedIn() {
  if (auth.isLoggedIn) deployments.recoverInflight()
}
onMounted(recoverIfLoggedIn)
watch(() => auth.isLoggedIn, (yes) => { if (yes) deployments.recoverInflight() })
</script>
