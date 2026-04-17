<template>
  <div class="card">
    <div class="card-header">
      <div class="card-title">全局配置 <span class="text-sm text-gray" style="font-weight:normal">(GitLab / Harbor 共享配置; ArgoCD 请在「环境字典」中为每个环境独立配置)</span></div>
    </div>

    <h3 class="section-title">GitLab</h3>
    <div class="grid">
      <div class="form-group">
        <label class="form-label">GitLab URL</label>
        <input v-model="form.gitlab_url" class="form-input" placeholder="http://gitlab.xx" />
      </div>
      <div class="form-group">
        <label class="form-label">GitLab Token</label>
        <input v-model="form.gitlab_token" class="form-input" type="password" placeholder="留空不修改" />
        <div class="form-help">AES 加密存储; 显示掩码</div>
      </div>
      <div class="form-group">
        <label class="form-label">Commit 用户名</label>
        <input v-model="form.gitlab_user" class="form-input" placeholder="deploy-bot" />
      </div>
      <div class="form-group">
        <label class="form-label">Commit 邮箱</label>
        <input v-model="form.gitlab_email" class="form-input" placeholder="deploy-bot@xx.com" />
      </div>
    </div>
    <div style="margin: 8px 0 24px">
      <button class="btn btn-sm" @click="onTest('gitlab')">测试 GitLab 连通</button>
    </div>

    <h3 class="section-title">Harbor (镜像仓库)</h3>
    <div class="grid">
      <div class="form-group">
        <label class="form-label">Harbor URL</label>
        <input v-model="form.harbor_url" class="form-input" placeholder="http://harbor.xx" />
      </div>
      <div class="form-group">
        <label class="form-label">Harbor 用户</label>
        <input v-model="form.harbor_user" class="form-input" />
      </div>
      <div class="form-group">
        <label class="form-label">Harbor 密码</label>
        <input v-model="form.harbor_password" class="form-input" type="password" placeholder="留空不修改" />
      </div>
    </div>
    <div style="margin: 8px 0 24px">
      <button class="btn btn-sm" @click="onTest('harbor')">测试 Harbor 连通</button>
    </div>

    <div class="argocd-hint">
      <div class="hint-icon">💡</div>
      <div>
        <div style="font-weight:600">ArgoCD 配置位置变更</div>
        <div class="text-sm text-gray" style="margin-top:4px">
          每个环境有独立的 ArgoCD 实例（dev / test / uat / prod 各一套），请前往
          <router-link to="/environments" style="color:#2563eb">环境字典</router-link>
          页面为每个环境单独配置 ArgoCD URL 和 Token。
        </div>
      </div>
    </div>

    <div style="border-top:1px solid #e5e7eb;padding-top:16px;display:flex;justify-content:flex-end;margin-top:20px">
      <button class="btn btn-primary" @click="onSave">保存全局配置</button>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { globalConfigApi } from '../api'
import { success, error, info } from '../stores/ui'

const form = ref({
  gitlab_url: '', gitlab_token: '', gitlab_user: '', gitlab_email: '',
  harbor_url: '', harbor_user: '', harbor_password: ''
})

async function load() {
  try {
    const data = (await globalConfigApi.get()).data || {}
    form.value.gitlab_url = data.gitlab_url || ''
    form.value.gitlab_token = data.gitlab_token || ''
    form.value.gitlab_user = data.gitlab_user || ''
    form.value.gitlab_email = data.gitlab_email || ''
    form.value.harbor_url = data.harbor_url || ''
    form.value.harbor_user = data.harbor_user || ''
    form.value.harbor_password = data.harbor_password || ''
  } catch (e) { error('加载失败: ' + e.message) }
}

async function onSave() {
  try { await globalConfigApi.update(form.value); success('保存成功'); load() }
  catch (e) { error('保存失败: ' + (e.response?.data?.message || e.message)) }
}

async function onTest(kind) {
  try {
    let res
    if (kind === 'gitlab') res = await globalConfigApi.testGitlab()
    else res = await globalConfigApi.testHarbor()
    info(res.data?.msg || '测试完成')
  } catch (e) { error('测试失败: ' + e.message) }
}

onMounted(load)
</script>

<style scoped>
.section-title { font-size: 14px; font-weight: 600; margin: 16px 0 8px; color: #374151; padding-bottom: 6px; border-bottom: 1px solid #f3f4f6; }
.grid { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
.argocd-hint {
  display: flex; gap: 12px;
  background: #eff6ff;
  border: 1px solid #bfdbfe;
  border-radius: 6px;
  padding: 14px 16px;
  margin: 16px 0;
}
.hint-icon { font-size: 20px; flex-shrink: 0; }
</style>
