<template>
  <div class="ss">
    <div class="rail">
      <div class="rail-title">配置分区</div>
      <div v-for="t in tabs" :key="t.v" :class="['rail-item', { active: tab === t.v }]" @click="tab = t.v">
        {{ t.label }}
      </div>
    </div>

    <div class="pane" v-loading="loading">
      <div v-if="tab === 'cred'" class="section">
        <div class="sec-head">
          <div class="sec-title">GitLab 全局凭证</div>
          <div class="sec-desc">用于 clone/commit/push · token AES 加密</div>
        </div>
        <div class="sec-body">
          <el-form :model="gc" label-width="140px" label-position="left" size="default">
            <el-form-item label="GitLab URL"><el-input v-model="gc.gitlab_url" class="mono" /></el-form-item>
            <el-form-item label="User"><el-input v-model="gc.gitlab_user" /></el-form-item>
            <el-form-item label="Email"><el-input v-model="gc.gitlab_email" /></el-form-item>
            <el-form-item label="Token">
              <el-input v-model="gc.gitlab_token" type="password" show-password placeholder="已设置，留空则不覆盖" />
            </el-form-item>
          </el-form>
          <div class="actions">
            <el-button @click="onTestGit" :loading="testing.git">测试连接</el-button>
            <el-button type="primary" @click="saveGlobal" :loading="saving">保存</el-button>
          </div>
        </div>
      </div>

      <div v-if="tab === 'lark'" class="section">
        <div class="sec-head">
          <div class="sec-title">默认 Lark 通知</div>
          <div class="sec-desc">project_env 未配置时使用此 webhook</div>
        </div>
        <div class="sec-body">
          <el-form :model="gc" label-width="140px" label-position="left" size="default">
            <el-form-item label="Webhook URL"><el-input v-model="gc.lark_default_webhook" class="mono" /></el-form-item>
            <el-form-item label="Secret">
              <el-input v-model="gc.lark_default_secret" type="password" show-password placeholder="可空，留空不更新" />
            </el-form-item>
          </el-form>
          <div class="actions">
            <el-button type="primary" @click="saveGlobal" :loading="saving">保存</el-button>
          </div>
        </div>
      </div>

      <div v-if="tab === 'poll'" class="section">
        <div class="sec-head">
          <div class="sec-title">ArgoCD 同步轮询策略</div>
          <div class="sec-desc">触发 sync 后，后端每隔 N 秒查状态，直到 Synced+Healthy 或超时</div>
        </div>
        <div class="sec-body">
          <div class="slider-row">
            <div class="lbl"><b>轮询间隔</b><div class="desc">过短给 ArgoCD 压力大；过长反馈慢</div></div>
            <el-slider v-model="gc.poll_interval_sec" :min="5" :max="60" :step="1" style="flex:1;margin:0 16px" />
            <div class="val mono">{{ gc.poll_interval_sec }}s</div>
          </div>
          <div class="slider-row">
            <div class="lbl"><b>最长等待</b><div class="desc">超时则标 partial/timeout 并发 Lark</div></div>
            <el-slider v-model="gc.poll_timeout_min" :min="1" :max="10" :step="1" style="flex:1;margin:0 16px" />
            <div class="val mono">{{ gc.poll_timeout_min }}min</div>
          </div>
          <div class="slider-row">
            <div class="lbl"><b>Git Push 重试</b><div class="desc">冲突 pull rebase 再推，最多 N 次</div></div>
            <el-slider v-model="gc.git_retry_count" :min="1" :max="10" :step="1" style="flex:1;margin:0 16px" />
            <div class="val mono">{{ gc.git_retry_count }} 次</div>
          </div>
          <div class="actions">
            <el-button type="primary" @click="saveGlobal" :loading="saving">保存</el-button>
          </div>
        </div>
      </div>

      <div v-if="tab === 'about'" class="section">
        <div class="sec-head">
          <div class="sec-title">Deploy Center</div>
          <div class="sec-desc">GitOps 发布控制台 V1</div>
        </div>
        <div class="sec-body">
          <div class="info-grid">
            <div class="info"><div class="l">后端版本</div><div class="v mono">v20</div></div>
            <div class="info"><div class="l">前端版本</div><div class="v mono">v20</div></div>
            <div class="info"><div class="l">数据库</div><div class="v">MySQL 8.0 · deploy_center</div></div>
            <div class="info"><div class="l">技术栈</div><div class="v">Go · net/http · gorilla/mux</div></div>
            <div class="info"><div class="l">前端</div><div class="v">Vue 3 · Vite · Element Plus</div></div>
            <div class="info"><div class="l">部署</div><div class="v">K8s single-replica + PVC</div></div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { getGlobalConfig, updateGlobalConfig, testGitlab } from '../api'

const tab = ref('cred')
const tabs = [
  { v: 'cred', label: '全局凭证' },
  { v: 'lark', label: 'Lark 通知' },
  { v: 'poll', label: '同步策略' },
  { v: 'about', label: '关于' }
]
const gc = reactive({
  gitlab_url: '', gitlab_user: '', gitlab_email: '', gitlab_token: '',
  lark_default_webhook: '', lark_default_secret: '',
  poll_interval_sec: 10, poll_timeout_min: 3, git_retry_count: 3
})
const loading = ref(false)
const saving = ref(false)
const testing = reactive({ git: false })

async function load() {
  loading.value = true
  try {
    const r = await getGlobalConfig()
    Object.assign(gc, r, { gitlab_token: '', lark_default_secret: '' })
  } finally { loading.value = false }
}
async function saveGlobal() {
  const payload = { ...gc }
  if (!payload.gitlab_token) delete payload.gitlab_token
  if (!payload.lark_default_secret) delete payload.lark_default_secret
  saving.value = true
  try {
    await updateGlobalConfig(payload)
    ElMessage.success('已保存')
    await load()
  } finally { saving.value = false }
}
async function onTestGit() {
  testing.git = true
  try { await testGitlab(); ElMessage.success('GitLab 连通 OK') }
  finally { testing.git = false }
}

onMounted(load)
</script>

<style scoped>
.ss { display: grid; grid-template-columns: 200px 1fr; gap: 0; height: calc(100vh - 120px); }
.rail { background: #fff; border: 1px solid var(--border); border-radius: 8px 0 0 8px; padding: 16px 0; border-right: none; }
.rail-title { padding: 0 16px; font-size: 10px; text-transform: uppercase; letter-spacing: 1px; color: var(--text-3); font-weight: 600; margin-bottom: 8px; }
.rail-item { padding: 9px 16px; cursor: pointer; font-size: 13px; color: var(--text-2); border-left: 3px solid transparent; }
.rail-item:hover { background: #fafbfc; color: var(--text); }
.rail-item.active { background: #eff6ff; border-left-color: var(--primary); color: var(--primary); font-weight: 600; }
.pane { flex: 1; overflow: auto; padding: 18px 24px; background: #fff; border: 1px solid var(--border); border-radius: 0 8px 8px 0; }
.section { margin-bottom: 14px; }
.sec-head { padding-bottom: 12px; border-bottom: 1px solid var(--border-soft); margin-bottom: 14px; }
.sec-title { font-size: 14px; font-weight: 600; }
.sec-desc { font-size: 11.5px; color: var(--text-3); margin-top: 2px; }
.actions { margin-top: 10px; text-align: right; }
.slider-row { display: flex; align-items: center; padding: 10px 0; border-bottom: 1px dashed var(--border-soft); }
.lbl { width: 180px; font-size: 12.5px; }
.lbl b { display: block; }
.lbl .desc { color: var(--text-3); font-size: 11px; margin-top: 2px; }
.val { width: 80px; text-align: right; color: var(--primary); font-weight: 600; }
.info-grid { display: grid; grid-template-columns: repeat(3, 1fr); gap: 12px; }
.info { background: #fafbfc; border: 1px solid var(--border-soft); border-radius: 6px; padding: 12px; }
.info .l { font-size: 11px; color: var(--text-3); text-transform: uppercase; letter-spacing: .5px; }
.info .v { font-size: 13px; margin-top: 4px; font-weight: 500; }
</style>
