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
//
// 关键守卫：URL 带 portal_token 时（SSO 入站，router beforeEach 正在跑 portalAuth），
// 不能立刻 fire 这个 API ——
// 因为 localStorage 里的 deploy_token 可能是上一个 portal session 留下的过期 token，
// 拿它去查会撞 401，axios 401 拦截器把页面踢到 /login 把握手冲掉。
// 等 portalAuth 完成后，watch(isLoggedIn) 不会触发（token 旧→新都 truthy 不变），
// 所以这里加 watch(token) 在 token 真正换值后补 recoverInflight 一次。
function hasPortalTokenInURL() {
  return new URLSearchParams(location.search).has('portal_token')
}
function recoverIfLoggedIn() {
  if (hasPortalTokenInURL()) return // 让 portalAuth 跑完
  if (auth.isLoggedIn) deployments.recoverInflight()
}
onMounted(recoverIfLoggedIn)
watch(() => auth.isLoggedIn, (yes) => { if (yes && !hasPortalTokenInURL()) deployments.recoverInflight() })
// portalAuth 后 token 字符串本身会变（旧→新），追加一个 watch 让 token 真正被替换时再补做
watch(() => auth.token, (newTok, oldTok) => {
  if (newTok && oldTok && newTok !== oldTok) deployments.recoverInflight()
})
</script>
