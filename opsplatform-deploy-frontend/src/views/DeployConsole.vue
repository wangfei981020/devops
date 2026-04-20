<template>
  <div class="deploy">
    <!-- 刚才发布的快捷回滚橙条 -->
    <div v-if="recentDeployment" class="quick-bar">
      <span class="icon">⚠</span>
      <span>刚完成的发布 <b class="mono">#{{ recentDeployment.id }}</b> ({{ recentDeployment.time }})</span>
      <span style="flex:1;"></span>
      <el-button type="warning" size="small" @click="openRollback(recentDeployment.id)">立即回滚</el-button>
      <el-button size="small" text @click="recentDeployment = null">关闭</el-button>
    </div>

    <!-- 顶部 selector -->
    <div class="selector-bar">
      <span class="label">目标环境</span>
      <el-select v-model="selectedID" size="default" style="width:300px;" @change="onEnvChange" placeholder="选择 project_env">
        <el-option v-for="e in envs" :key="e.id" :value="e.id" :label="e.name">
          <div style="display:flex;justify-content:space-between;align-items:center;">
            <span>{{ e.name }}<span v-if="e.display_name" style="color:#999;margin-left:6px;">— {{ e.display_name }}</span></span>
            <span :class="'env-chip ' + e.env_type">{{ e.env_type.toUpperCase() }}</span>
          </div>
        </el-option>
      </el-select>
      <span v-if="currentEnv" :class="'env-chip ' + currentEnv.env_type">{{ currentEnv.env_type.toUpperCase() }}</span>

      <div class="stats" v-if="currentEnv">
        <span><b class="mono">{{ modules.length }}</b>个模块</span>
        <span>auto-sync <b class="mono">{{ currentEnv.auto_sync ? 'ON' : 'OFF' }}</b></span>
      </div>

      <el-button :loading="scanning" :icon="Refresh" @click="onScan" size="default" style="margin-left:auto;">
        扫描 Git 重建模块列表
      </el-button>
    </div>

    <!-- 双栏 -->
    <div class="split" v-if="currentEnv">
      <div class="panel"><BatchUpdatePanel :project-env="currentEnv" :modules="modules" @done="handleDeployDone" /></div>
      <div class="panel"><RestartPanel :project-env="currentEnv" :modules="modules" /></div>
    </div>
    <div v-else class="empty">请先选择一个 project_env（或去「项目配置」新增一个）</div>

    <RollbackDialog v-model="rbVis" :deployment-id="rbID" @done="onRollbackDone" />
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Refresh } from '@element-plus/icons-vue'
import dayjs from 'dayjs'
import { listProjectEnvs, listModules, scanModules } from '../api'
import BatchUpdatePanel from '../components/BatchUpdatePanel.vue'
import RestartPanel from '../components/RestartPanel.vue'
import RollbackDialog from '../components/RollbackDialog.vue'

const envs = ref([])
const selectedID = ref(null)
const modules = ref([])
const scanning = ref(false)

const currentEnv = computed(() => envs.value.find(e => e.id === selectedID.value))

const recentDeployment = ref(null)
const rbVis = ref(false)
const rbID = ref(null)

async function loadEnvs() {
  envs.value = (await listProjectEnvs()) || []
  if (envs.value.length && !selectedID.value) {
    selectedID.value = envs.value[0].id
    await loadModules()
  }
}
async function loadModules() {
  if (!selectedID.value) return
  modules.value = (await listModules(selectedID.value)) || []
}
function onEnvChange() { loadModules() }
async function onScan() {
  if (!selectedID.value) return
  scanning.value = true
  try {
    const r = await scanModules(selectedID.value)
    ElMessage.success(`扫描到 ${r.count} 个模块`)
    await loadModules()
  } finally { scanning.value = false }
}

function handleDeployDone(depID) {
  recentDeployment.value = { id: depID, time: dayjs().format('HH:mm') }
  setTimeout(() => {
    if (recentDeployment.value?.id === depID) recentDeployment.value = null
  }, 5 * 60 * 1000)
  setTimeout(loadModules, 2000)
}
function openRollback(id) { rbID.value = id; rbVis.value = true }
function onRollbackDone() { recentDeployment.value = null; loadModules() }

onMounted(loadEnvs)
</script>

<style scoped>
.quick-bar {
  display: flex; align-items: center; gap: 10px;
  background: #fff7ed; border: 1px solid #fed7aa; border-radius: 8px;
  padding: 10px 16px; margin-bottom: 14px; font-size: 13px;
}
.quick-bar .icon { color: #ea580c; font-weight: bold; }
.selector-bar { background: #fff; border: 1px solid var(--border); border-radius: 8px; padding: 14px 18px; margin-bottom: 14px; display: flex; align-items: center; gap: 16px; }
.label { font-weight: 600; font-size: 13px; }
.stats { display: flex; gap: 18px; margin-left: 18px; font-size: 12px; color: var(--text-2); }
.stats b { color: var(--text); margin-right: 4px; }
.split { display: grid; grid-template-columns: 1.05fr 1fr; gap: 14px; }
.panel { background: #fff; border: 1px solid var(--border); border-radius: 8px; overflow: hidden; }
.empty { padding: 80px; text-align: center; color: var(--text-3); background: #fff; border: 1px solid var(--border); border-radius: 8px; }
</style>
