<template>
  <div class="app">
    <aside class="sidebar">
      <div class="brand">
        <div class="brand-logo">D</div>
        <div class="brand-n">Deploy</div>
        <div class="brand-t">v{{ version }}</div>
      </div>

      <nav class="nav">
        <div class="group">
          <div :class="['group-title', {opened: grp.publish}]" @click="grp.publish = !grp.publish">
            <el-icon class="ico"><Upload /></el-icon>
            <span>发布管理</span>
            <el-icon class="chev"><ArrowRight /></el-icon>
          </div>
          <template v-if="grp.publish">
            <RouterLink to="/deploy" class="sub-item" active-class="active">部署控制台</RouterLink>
            <RouterLink to="/history" class="sub-item" active-class="active">发布历史</RouterLink>
          </template>
        </div>

        <div class="group">
          <div :class="['group-title', {opened: grp.config}]" @click="grp.config = !grp.config">
            <el-icon class="ico"><Setting /></el-icon>
            <span>配置管理</span>
            <el-icon class="chev"><ArrowRight /></el-icon>
          </div>
          <template v-if="grp.config">
            <RouterLink to="/projects" class="sub-item" active-class="active">项目配置</RouterLink>
            <RouterLink to="/settings" class="sub-item" active-class="active">系统设置</RouterLink>
          </template>
        </div>
      </nav>

      <div class="sidebar-foot">
        <span>{{ currentIdentity || 'system' }}</span>
        <span>v{{ version }}</span>
      </div>
    </aside>

    <main class="main">
      <div class="topbar">
        <div class="crumb">部署中心 / <b>{{ $route.meta.title }}</b></div>
        <div class="topbar-r">
          <span class="lbl">当前用户</span>
          <div class="user-chip" @click="openPicker" :title="'点击切换身份'">
            <el-icon><User /></el-icon>
            <span>{{ currentIdentity || 'system' }}</span>
            <el-icon class="ch-arrow"><ArrowDown /></el-icon>
          </div>
          <span class="ver-badge">v{{ version }}</span>
        </div>
      </div>
      <div class="content">
        <RouterView />
      </div>
    </main>

    <!-- 身份选择器 -->
    <el-dialog v-model="pickerVis" title="选择身份" width="420px" custom-class="id-dialog">
      <div class="picker-body">
        <div class="picker-hint">选择你是谁 · 后续所有发布/重启/回滚都会记录为这个人 · 存本地浏览器</div>
        <div v-if="loading" class="picker-loading">加载中...</div>
        <div v-else-if="!accounts.length" class="picker-empty">
          没有账号 · 请先去 <RouterLink to="/settings" @click="pickerVis = false">系统设置 → 账号管理</RouterLink> 添加
        </div>
        <div v-else class="picker-list">
          <div
            v-for="a in accounts" :key="a.id"
            :class="['picker-item', {active: currentIdentity === a.username}]"
            @click="pick(a.username)">
            <div class="pi-avatar">{{ a.display_name?.[0] || a.username[0] }}</div>
            <div class="pi-main">
              <div class="pi-name">{{ a.display_name || a.username }}</div>
              <div class="pi-sub mono">{{ a.username }} <span v-if="a.lark_id" class="pi-lark">· @lark</span></div>
            </div>
            <el-icon v-if="currentIdentity === a.username" class="pi-check"><Check /></el-icon>
          </div>
          <div
            :class="['picker-item', 'sys', {active: !currentIdentity}]"
            @click="pick('')">
            <div class="pi-avatar sys">S</div>
            <div class="pi-main">
              <div class="pi-name">system</div>
              <div class="pi-sub">匿名 · 不艾特任何人</div>
            </div>
            <el-icon v-if="!currentIdentity" class="pi-check"><Check /></el-icon>
          </div>
        </div>
      </div>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Upload, Setting, ArrowRight, ArrowDown, User, Check } from '@element-plus/icons-vue'
import { listAccounts } from '../api'

const version = '42'

// 侧栏分组折叠状态（持久化到 localStorage）
const grpRaw = JSON.parse(localStorage.getItem('deploy_sidebar_groups') || '{"publish":true,"config":true}')
const grp = reactive(grpRaw)
import { watch } from 'vue'
watch(grp, (v) => localStorage.setItem('deploy_sidebar_groups', JSON.stringify(v)), { deep: true })
const accounts = ref([])
const loading = ref(false)
const pickerVis = ref(false)
const currentIdentity = ref(localStorage.getItem('deploy_identity') || '')

async function openPicker() {
  pickerVis.value = true
  if (!accounts.value.length) {
    loading.value = true
    try { accounts.value = (await listAccounts()) || [] }
    finally { loading.value = false }
  }
}

function pick(username) {
  currentIdentity.value = username
  if (username) localStorage.setItem('deploy_identity', username)
  else localStorage.removeItem('deploy_identity')
  ElMessage.success(`身份：${username || 'system'}`)
  pickerVis.value = false
}

onMounted(() => {})
</script>

<style scoped>
.app { display: flex; height: 100vh; }

.sidebar { width: 220px; background: var(--sidebar-bg); flex-shrink: 0; display: flex; flex-direction: column; }
.brand { padding: 18px 20px; border-bottom: 1px solid rgba(255, 255, 255, 0.06); display: flex; align-items: center; gap: 10px; }
.brand-logo { width: 30px; height: 30px; background: var(--primary); border-radius: 6px; display: flex; align-items: center; justify-content: center; color: #fff; font-weight: 700; font-size: 14px; }
.brand-n { color: #fff; font-weight: 600; font-size: 14px; }
.brand-t { margin-left: auto; font-family: var(--mono); font-size: 10px; font-weight: 500; color: rgba(255, 255, 255, 0.4); padding: 1px 6px; background: rgba(255, 255, 255, 0.05); border-radius: 3px; }

.nav { padding: 12px 0; flex: 1; overflow-y: auto; }
.group { margin-bottom: 4px; }
.group-title { display: flex; align-items: center; padding: 10px 16px; color: var(--sidebar-text); font-size: 13px; font-weight: 500; gap: 10px; cursor: pointer; }
.group-title:hover { background: var(--sidebar-hover); color: #fff; }
.group-title .ico { font-size: 15px; opacity: .8; }
.group-title .chev { margin-left: auto; opacity: .5; font-size: 12px; transition: transform .2s; }
.group-title.opened .chev { transform: rotate(90deg); opacity: .8; }

.sub-item { display: block; padding: 8px 16px 8px 42px; color: var(--sidebar-text-sub); font-size: 12.5px; text-decoration: none; border-left: 2px solid transparent; transition: all .12s; }
.sub-item:hover { color: #fff; background: var(--sidebar-hover); }
.sub-item.active { color: #fff; background: var(--sidebar-active-bg); border-left-color: var(--primary); font-weight: 500; }

.sidebar-foot { padding: 12px 16px; border-top: 1px solid rgba(255, 255, 255, 0.06); font-size: 11px; color: rgba(255, 255, 255, 0.4); font-family: var(--mono); display: flex; justify-content: space-between; }

.main { flex: 1; display: flex; flex-direction: column; overflow: hidden; }
.topbar { height: 52px; background: var(--bg-card); border-bottom: 1px solid var(--border); display: flex; align-items: center; justify-content: space-between; padding: 0 24px; flex-shrink: 0; }
.crumb { font-size: 13px; color: var(--text-2); }
.crumb b { color: var(--text); font-weight: 600; margin-left: 2px; }
.topbar-r { display: flex; align-items: center; gap: 14px; font-size: 12px; color: var(--text-3); }
.lbl { color: var(--text-3); }
.user-chip {
  display: flex; align-items: center; gap: 6px;
  padding: 4px 10px;
  background: var(--bg-hover);
  border: 1px solid transparent;
  border-radius: 99px;
  font-family: var(--mono);
  color: var(--text-2);
  cursor: pointer;
  transition: all .15s;
}
.user-chip:hover { border-color: var(--primary); color: var(--primary); background: var(--primary-bg); }
.user-chip .ch-arrow { font-size: 11px; opacity: .6; }
.ver-badge { padding: 2px 7px; background: var(--primary-bg); color: var(--primary); border-radius: 4px; font-family: var(--mono); font-size: 11px; font-weight: 600; }

.content { flex: 1; overflow: auto; padding: 20px 24px; }

/* ===== 身份选择器弹窗 ===== */
:deep(.id-dialog) { border-radius: 8px; }
:deep(.id-dialog .el-dialog__header) { padding: 16px 22px; margin: 0; border-bottom: 1px solid var(--border-soft); }
:deep(.id-dialog .el-dialog__title) { font-weight: 600; font-size: 15px; }
:deep(.id-dialog .el-dialog__body) { padding: 0; }

.picker-body { padding: 14px 22px 20px; }
.picker-hint { font-size: 12px; color: var(--text-3); margin-bottom: 14px; line-height: 1.6; }
.picker-loading, .picker-empty { text-align: center; padding: 30px; color: var(--text-3); font-size: 13px; }
.picker-empty a { color: var(--primary); text-decoration: none; }
.picker-list { display: flex; flex-direction: column; gap: 4px; }
.picker-item {
  display: flex; align-items: center; gap: 12px;
  padding: 10px 12px; border-radius: 6px; cursor: pointer;
  transition: background .12s; border: 1px solid transparent;
}
.picker-item:hover { background: var(--bg-hover); }
.picker-item.active { background: var(--primary-bg); border-color: var(--primary); }
.pi-avatar {
  width: 32px; height: 32px; border-radius: 50%;
  background: linear-gradient(135deg, var(--primary), #06b6d4);
  color: #fff; font-weight: 700; font-size: 13px;
  display: flex; align-items: center; justify-content: center;
}
.pi-avatar.sys { background: var(--text-3); }
.pi-main { flex: 1; }
.pi-name { font-weight: 500; font-size: 13px; color: var(--text); }
.pi-sub { font-size: 11px; color: var(--text-3); margin-top: 2px; }
.pi-lark { color: var(--success); font-weight: 500; }
.pi-check { color: var(--primary); font-size: 16px; }
</style>
