<template>
  <div class="page">
    <div class="page-head">
      <span class="page-title">接入管理</span>
      <span class="muted" style="margin-left:10px">
        所有外部系统的凭据与接入点集中在这里。凭据一律加密存储、保存后不回显。
      </span>
    </div>

    <el-card shadow="never">
      <el-tabs v-model="tab" @tab-change="onTab">
        <el-tab-pane label="域名注册商" name="registrar">
          <RegistrarPanel v-if="loaded.registrar" />
        </el-tab-pane>
        <el-tab-pane label="云账号" name="cloud">
          <CloudAccounts v-if="loaded.cloud" embedded />
        </el-tab-pane>
        <el-tab-pane label="CDN" name="cdn">
          <CdnPanel v-if="loaded.cdn" />
        </el-tab-pane>
        <el-tab-pane label="观测数据源" name="obs">
          <ObsEndpoints v-if="loaded.obs" embedded />
        </el-tab-pane>
        <el-tab-pane label="证书 CA" name="acme">
          <AcmePanel v-if="loaded.acme" />
        </el-tab-pane>
      </el-tabs>
    </el-card>
  </div>
</template>

<script setup>
import { ref, reactive, watch, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import RegistrarPanel from '../components/integrations/RegistrarPanel.vue'
import CdnPanel from '../components/integrations/CdnPanel.vue'
import AcmePanel from '../components/integrations/AcmePanel.vue'
import CloudAccounts from './CloudAccounts.vue'
import ObsEndpoints from './ObsEndpoints.vue'

const route = useRoute()
const router = useRouter()
const valid = ['registrar', 'cloud', 'cdn', 'obs', 'acme']
const tab = ref(valid.includes(route.query.tab) ? route.query.tab : 'registrar')

// 各 tab 懒加载：每个面板在 onMounted 里各自拉数据，一次性全挂会同时打五组请求，
// 而用户通常只看其中一个。切到哪个才加载哪个，加载过的保留（避免来回切时重复请求）。
const loaded = reactive({ registrar: false, cloud: false, cdn: false, obs: false, acme: false })
loaded[tab.value] = true

function onTab(name) {
  loaded[name] = true
  // tab 写进 URL：刷新后停在原处，也方便把链接直接发给别人
  router.replace({ query: { ...route.query, tab: name } })
}

// 支持从旧路由重定向过来时带 tab 参数
watch(() => route.query.tab, (v) => {
  if (valid.includes(v) && v !== tab.value) { tab.value = v; loaded[v] = true }
})

onMounted(() => { loaded[tab.value] = true })
</script>

<style scoped>
.page-head { margin-bottom: 14px; }
.page-title { font-size: 18px; font-weight: 600; }
.muted { color: #909399; font-size: 12px; }
</style>
