<template>
  <div>
    <div class="page-header">
      <h1>连接设置</h1>
      <p>配置 Jira 和 Confluence 连接信息、轮询策略</p>
    </div>

    <!-- Jira 连接 -->
    <div class="card">
      <div class="card-header">
        <span class="card-title">Jira 连接</span>
      </div>

      <div class="form-group">
        <label class="form-label">Jira URL</label>
        <input v-model="settings.jira_url" class="form-input" placeholder="https://jira.example.com" />
      </div>
      <div class="form-group">
        <label class="form-label">用户名</label>
        <input v-model="settings.jira_username" class="form-input" placeholder="Jira 用户名" />
      </div>
      <div class="form-group">
        <label class="form-label">密码 / Token</label>
        <input v-model="settings.jira_password" type="password" class="form-input" placeholder="密码或 Personal Access Token" />
      </div>

      <div style="display: flex; gap: 10px; margin-top: 20px;">
        <button class="btn btn-primary" @click="handleSave" :disabled="saving">{{ saving ? '保存中...' : '保存设置' }}</button>
        <button class="btn btn-secondary" @click="handleTestJira" :disabled="testingJira">{{ testingJira ? '测试中...' : '测试连接' }}</button>
      </div>

      <div v-if="jiraResult" :class="['test-result', jiraResult.ok ? 'success' : 'error']">
        {{ jiraResult.message }}
      </div>
    </div>

    <!-- Confluence 连接 -->
    <div class="card">
      <div class="card-header">
        <span class="card-title">Confluence 连接</span>
      </div>

      <div class="form-group">
        <label class="form-label">Confluence URL</label>
        <input v-model="settings.confluence_url" class="form-input" placeholder="https://confsys.solidleisure.com" />
      </div>
      <div class="form-group">
        <label class="form-label">用户名</label>
        <input v-model="settings.confluence_username" class="form-input" placeholder="管理员用户名" />
      </div>
      <div class="form-group">
        <label class="form-label">密码</label>
        <input v-model="settings.confluence_password" type="password" class="form-input" placeholder="密码" />
      </div>

      <div style="display: flex; gap: 10px; margin-top: 20px;">
        <button class="btn btn-primary" @click="handleSave" :disabled="saving">{{ saving ? '保存中...' : '保存设置' }}</button>
        <button class="btn btn-secondary" @click="handleTestConfluence" :disabled="testingConf">{{ testingConf ? '测试中...' : '测试连接' }}</button>
      </div>

      <div v-if="confResult" :class="['test-result', confResult.ok ? 'success' : 'error']">
        {{ confResult.message }}
      </div>
    </div>

    <!-- 轮询设置 -->
    <div class="card">
      <div class="card-header">
        <span class="card-title">轮询设置</span>
      </div>

      <div style="font-size: 13px; color: var(--text-secondary); margin-bottom: 16px; line-height: 1.7;">
        轮询器会定时查询 Jira 工单状态，自动处理遗漏的 Webhook 事件。<br/>
        与 Webhook 实时推送互为补充，确保不丢事件。
      </div>

      <div class="form-group">
        <label class="form-label">轮询间隔（秒）<span style="color: var(--text-muted);"> 设为 0 则禁用轮询，最小 60 秒</span></label>
        <input v-model="settings.poll_interval" class="form-input" type="number" min="0" step="60" placeholder="300（默认5分钟）" style="width: 200px;" />
      </div>

      <button class="btn btn-primary" @click="handleSave" :disabled="saving">保存</button>
    </div>

    <!-- Webhook 配置说明 -->
    <div class="card">
      <div class="card-header">
        <span class="card-title">Jira Webhook 配置说明（可选，推荐配置）</span>
      </div>
      <div style="font-size: 13px; color: var(--text-secondary); line-height: 1.8;">
        <p>配置 Webhook 后，Jira 审批变更会实时通知本服务，响应更快。未配置也不影响，轮询器会兜底。</p>
        <ol style="padding-left: 20px; margin-top: 8px;">
          <li>进入 Jira 管理 → 系统 → Webhook</li>
          <li>新增 Webhook</li>
          <li>URL 填写：<code style="background: var(--bg-primary); padding: 2px 8px; border-radius: 4px; color: var(--accent);">http://opsplatform-jira-confluence-lock-backend.jira-confluence-lock.svc:8080/api/webhook/jira</code></li>
          <li>事件选择：<strong>Issue → updated</strong></li>
          <li>如需限定项目，可在 JQL 过滤中填写：<code style="background: var(--bg-primary); padding: 2px 8px; border-radius: 4px;">project = YX</code></li>
        </ol>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted, inject } from 'vue'
import { getSettings, updateSettings, testConfluence, testJira } from '@/api'

const toast = inject('toast')
const settings = ref({
  jira_url: '',
  jira_username: '',
  jira_password: '',
  confluence_url: '',
  confluence_username: '',
  confluence_password: '',
  poll_interval: '300'
})
const saving = ref(false)
const testingJira = ref(false)
const testingConf = ref(false)
const jiraResult = ref(null)
const confResult = ref(null)

async function loadSettings() {
  try {
    const res = await getSettings()
    settings.value = { ...settings.value, ...res.data }
  } catch (e) {
    toast(e.message, 'error')
  }
}

async function handleSave() {
  saving.value = true
  try {
    await updateSettings(settings.value)
    toast('设置已保存', 'success')
  } catch (e) {
    toast(e.message, 'error')
  } finally {
    saving.value = false
  }
}

async function handleTestJira() {
  testingJira.value = true
  jiraResult.value = null
  try {
    const res = await testJira(settings.value)
    jiraResult.value = { ok: true, message: `连接成功！当前用户: ${res.data.displayName} (${res.data.username})` }
  } catch (e) {
    jiraResult.value = { ok: false, message: e.message }
  } finally {
    testingJira.value = false
  }
}

async function handleTestConfluence() {
  testingConf.value = true
  confResult.value = null
  try {
    const res = await testConfluence(settings.value)
    confResult.value = { ok: true, message: `连接成功！当前用户: ${res.data.displayName} (${res.data.username})` }
  } catch (e) {
    confResult.value = { ok: false, message: e.message }
  } finally {
    testingConf.value = false
  }
}

onMounted(loadSettings)
</script>

<style scoped>
.test-result {
  margin-top: 16px;
  padding: 12px 16px;
  border-radius: var(--radius);
  font-size: 13px;
}
.test-result.success {
  background: rgba(16, 185, 129, 0.1);
  color: #10B981;
  border: 1px solid rgba(16, 185, 129, 0.2);
}
.test-result.error {
  background: rgba(239, 68, 68, 0.1);
  color: #EF4444;
  border: 1px solid rgba(239, 68, 68, 0.2);
}
</style>
