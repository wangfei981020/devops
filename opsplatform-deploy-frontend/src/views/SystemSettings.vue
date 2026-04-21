<template>
  <div class="ss">
    <div class="rail">
      <div class="rail-title">配置分区</div>
      <div v-for="t in tabs" :key="t.v" :class="['rail-item', { active: tab === t.v }]" @click="tab = t.v">
        {{ t.label }}
      </div>
    </div>

    <div class="pane">
      <!-- 全局凭证 -->
      <div v-if="tab === 'cred'" class="section">
        <div class="sec-head">
          <div class="sec-title">GitLab 全局凭证</div>
          <div class="sec-desc">用于 clone/commit/push · token AES 加密</div>
        </div>
        <div class="sec-body" v-loading="loading.cred">
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
            <el-button type="primary" @click="saveGlobal" :loading="saving.cred">保存</el-button>
          </div>
        </div>
      </div>

      <!-- ArgoCD 实例 -->
      <div v-if="tab === 'argocd'" class="section">
        <div class="sec-head">
          <div class="sec-title-row">
            <div>
              <div class="sec-title">ArgoCD 实例</div>
              <div class="sec-desc">全局可管理多个 ArgoCD · 项目环境只从这里选一个</div>
            </div>
            <button class="add-btn" @click="openArgoCreate">
              <el-icon><Plus /></el-icon>新增实例
            </button>
          </div>
        </div>
        <div class="sec-body">
          <table class="tbl">
            <thead>
              <tr>
                <th>名称</th>
                <th>URL</th>
                <th>描述</th>
                <th style="width:160px">创建时间</th>
                <th style="width:140px">操作</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="a in argoInstances" :key="a.id">
                <td class="mono"><b>{{ a.name }}</b></td>
                <td class="mono">{{ a.url }}</td>
                <td>{{ a.description || '—' }}</td>
                <td class="mono">{{ fmt(a.created_at) }}</td>
                <td>
                  <button class="act" @click="onTestArgo(a)">测试</button>
                  <button class="act" @click="openArgoEdit(a)">编辑</button>
                  <button class="act danger" @click="onDeleteArgo(a)">删除</button>
                </td>
              </tr>
              <tr v-if="!argoInstances.length">
                <td colspan="5" class="empty-row">还没有 ArgoCD 实例，点右上「+ 新增实例」添加</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>

      <!-- 账号管理 -->
      <div v-if="tab === 'accounts'" class="section">
        <div class="sec-head">
          <div class="sec-title-row">
            <div>
              <div class="sec-title">账号管理</div>
              <div class="sec-desc">身份选择用 · Lark 艾特根据 lark_id 匹配</div>
            </div>
            <button class="add-btn" @click="openAccCreate">
              <el-icon><Plus /></el-icon>新增账号
            </button>
          </div>
        </div>
        <div class="sec-body">
          <table class="tbl">
            <thead>
              <tr>
                <th style="width:140px">用户名</th>
                <th style="width:140px">显示名</th>
                <th>Lark ID</th>
                <th>Email</th>
                <th>备注</th>
                <th style="width:120px">操作</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="a in accounts" :key="a.id">
                <td class="mono"><b>{{ a.username }}</b></td>
                <td>{{ a.display_name || '—' }}</td>
                <td class="mono">{{ a.lark_id || '—' }}</td>
                <td>{{ a.email || '—' }}</td>
                <td>{{ a.remark || '—' }}</td>
                <td>
                  <button class="act" @click="openAccEdit(a)">编辑</button>
                  <button class="act danger" @click="onDeleteAcc(a)">删除</button>
                </td>
              </tr>
              <tr v-if="!accounts.length">
                <td colspan="6" class="empty-row">还没有账号，点右上「+ 新增账号」添加</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>

      <!-- Lark -->
      <div v-if="tab === 'lark'" class="section">
        <div class="sec-head">
          <div class="sec-title">默认 Lark 通知</div>
          <div class="sec-desc">project_env 未配置时使用此 webhook · 发布完成后艾特操作人</div>
        </div>
        <div class="sec-body" v-loading="loading.cred">
          <el-form :model="gc" label-width="140px" label-position="left" size="default">
            <el-form-item label="Webhook URL"><el-input v-model="gc.lark_default_webhook" class="mono" /></el-form-item>
            <el-form-item label="Secret">
              <el-input v-model="gc.lark_default_secret" type="password" show-password placeholder="可空，留空不更新" />
            </el-form-item>
          </el-form>
          <div class="actions">
            <el-button type="primary" @click="saveGlobal" :loading="saving.cred">保存</el-button>
          </div>
        </div>
      </div>

      <!-- 同步策略 -->
      <div v-if="tab === 'poll'" class="section">
        <div class="sec-head">
          <div class="sec-title">ArgoCD 同步轮询策略</div>
          <div class="sec-desc">触发 sync 后，后端每隔 N 秒查状态，直到 Synced+Healthy 或超时</div>
        </div>
        <div class="sec-body" v-loading="loading.cred">
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
            <el-button type="primary" @click="saveGlobal" :loading="saving.cred">保存</el-button>
          </div>
        </div>
      </div>

      <!-- About -->
      <div v-if="tab === 'about'" class="section">
        <div class="sec-head">
          <div class="sec-title">Deploy Center</div>
          <div class="sec-desc">GitOps 发布控制台 V1</div>
        </div>
        <div class="sec-body">
          <div class="info-grid">
            <div class="info"><div class="l">后端版本</div><div class="v mono">v33</div></div>
            <div class="info"><div class="l">前端版本</div><div class="v mono">v41</div></div>
            <div class="info"><div class="l">数据库</div><div class="v">MySQL 8.0 · deploy_center</div></div>
          </div>
        </div>
      </div>
    </div>

    <!-- ArgoCD 实例弹窗 -->
    <el-dialog v-model="argoDlg.vis" :title="argoDlg.isEdit ? '编辑 ArgoCD 实例' : '新增 ArgoCD 实例'" width="520px">
      <el-form :model="argoDlg.form" label-width="100px" label-position="top" size="default">
        <el-form-item label="名称 *">
          <el-input v-model="argoDlg.form.name" :disabled="argoDlg.isEdit" class="mono" placeholder="如: uat-cluster" />
        </el-form-item>
        <el-form-item label="URL *">
          <el-input v-model="argoDlg.form.url" class="mono" placeholder="http://argocd.xx" />
        </el-form-item>
        <el-form-item :label="argoDlg.isEdit ? 'Token（留空不更新）' : 'Token *'">
          <el-input v-model="argoDlg.form.token" type="password" show-password />
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="argoDlg.form.description" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="argoDlg.vis = false">取消</el-button>
        <el-button type="primary" @click="onSaveArgo">保存</el-button>
      </template>
    </el-dialog>

    <!-- 账号弹窗 -->
    <el-dialog v-model="accDlg.vis" :title="accDlg.isEdit ? '编辑账号' : '新增账号'" width="520px">
      <el-form :model="accDlg.form" label-width="100px" label-position="top" size="default">
        <el-form-item label="用户名 *">
          <el-input v-model="accDlg.form.username" :disabled="accDlg.isEdit" class="mono" placeholder="如: zhangsan" />
        </el-form-item>
        <el-form-item label="显示名">
          <el-input v-model="accDlg.form.display_name" placeholder="如: 张三" />
        </el-form-item>
        <el-form-item label="Lark ID">
          <el-input v-model="accDlg.form.lark_id" class="mono" placeholder="ou_xxxxxxxxxxxxxxxxxxxx" />
        </el-form-item>
        <el-form-item label="Email">
          <el-input v-model="accDlg.form.email" />
        </el-form-item>
        <el-form-item label="备注">
          <el-input v-model="accDlg.form.remark" type="textarea" :rows="2" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="accDlg.vis = false">取消</el-button>
        <el-button type="primary" @click="onSaveAcc">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus } from '@element-plus/icons-vue'
import dayjs from 'dayjs'
import {
  getGlobalConfig, updateGlobalConfig, testGitlab,
  listAccounts, createAccount, updateAccount, deleteAccount,
  listArgocdInstances, createArgocdInstance, updateArgocdInstance, deleteArgocdInstance, testArgocdInstance
} from '../api'

const tab = ref('cred')
const tabs = [
  { v: 'cred', label: '全局凭证' },
  { v: 'argocd', label: 'ArgoCD 实例' },
  { v: 'accounts', label: '账号管理' },
  { v: 'lark', label: 'Lark 通知' },
  { v: 'poll', label: '同步策略' },
  { v: 'about', label: '关于' }
]

const gc = reactive({
  gitlab_url: '', gitlab_user: '', gitlab_email: '', gitlab_token: '',
  lark_default_webhook: '', lark_default_secret: '',
  poll_interval_sec: 10, poll_timeout_min: 3, git_retry_count: 3
})
const accounts = ref([])
const argoInstances = ref([])
const loading = reactive({ cred: false })
const saving = reactive({ cred: false })
const testing = reactive({ git: false })

function fmt(s) { return s ? dayjs(s).format('YYYY-MM-DD HH:mm') : '' }

async function loadGlobal() {
  loading.cred = true
  try {
    const r = await getGlobalConfig()
    Object.assign(gc, r, { gitlab_token: '', lark_default_secret: '' })
  } finally { loading.cred = false }
}
async function saveGlobal() {
  const payload = { ...gc }
  if (!payload.gitlab_token) delete payload.gitlab_token
  if (!payload.lark_default_secret) delete payload.lark_default_secret
  saving.cred = true
  try {
    await updateGlobalConfig(payload)
    ElMessage.success('已保存')
    await loadGlobal()
  } finally { saving.cred = false }
}
async function onTestGit() {
  testing.git = true
  try { await testGitlab(); ElMessage.success('GitLab 连通 OK') }
  finally { testing.git = false }
}

// === ArgoCD 实例 ===
const argoDlg = reactive({ vis: false, isEdit: false, editingID: null, form: { name: '', url: '', token: '', description: '' } })
async function loadArgo() { argoInstances.value = (await listArgocdInstances()) || [] }
function openArgoCreate() {
  argoDlg.isEdit = false; argoDlg.editingID = null
  Object.assign(argoDlg.form, { name: '', url: '', token: '', description: '' })
  argoDlg.vis = true
}
function openArgoEdit(a) {
  argoDlg.isEdit = true; argoDlg.editingID = a.id
  Object.assign(argoDlg.form, { name: a.name, url: a.url, token: '', description: a.description || '' })
  argoDlg.vis = true
}
async function onSaveArgo() {
  if (!argoDlg.isEdit && !argoDlg.form.name.trim()) { ElMessage.warning('名称必填'); return }
  if (!argoDlg.form.url.trim()) { ElMessage.warning('URL 必填'); return }
  if (!argoDlg.isEdit && !argoDlg.form.token) { ElMessage.warning('Token 必填'); return }
  if (argoDlg.isEdit) await updateArgocdInstance(argoDlg.editingID, argoDlg.form)
  else await createArgocdInstance(argoDlg.form)
  ElMessage.success('已保存')
  argoDlg.vis = false
  await loadArgo()
}
async function onTestArgo(a) {
  try {
    const r = await testArgocdInstance(a.id)
    ElMessage.success(`连通 OK · version=${r.version}`)
  } catch (_) {}
}
async function onDeleteArgo(a) {
  try { await ElMessageBox.confirm(`确认删除 ArgoCD 实例「${a.name}」？`, '删除确认', { type: 'warning' }) }
  catch { return }
  try { await deleteArgocdInstance(a.id); ElMessage.success('已删除'); await loadArgo() } catch (_) {}
}

// === 账号 ===
const accDlg = reactive({ vis: false, isEdit: false, editingID: null, form: { username: '', display_name: '', lark_id: '', email: '', remark: '' } })
async function loadAcc() { accounts.value = (await listAccounts()) || [] }
function openAccCreate() {
  accDlg.isEdit = false; accDlg.editingID = null
  Object.assign(accDlg.form, { username: '', display_name: '', lark_id: '', email: '', remark: '' })
  accDlg.vis = true
}
function openAccEdit(a) {
  accDlg.isEdit = true; accDlg.editingID = a.id
  Object.assign(accDlg.form, { username: a.username, display_name: a.display_name, lark_id: a.lark_id, email: a.email, remark: a.remark })
  accDlg.vis = true
}
async function onSaveAcc() {
  if (!accDlg.isEdit && !accDlg.form.username.trim()) { ElMessage.warning('用户名必填'); return }
  if (accDlg.isEdit) await updateAccount(accDlg.editingID, accDlg.form)
  else await createAccount(accDlg.form)
  ElMessage.success('已保存')
  accDlg.vis = false
  await loadAcc()
}
async function onDeleteAcc(a) {
  try { await ElMessageBox.confirm(`确认删除账号「${a.username}」？`, '删除确认', { type: 'warning' }) }
  catch { return }
  await deleteAccount(a.id)
  ElMessage.success('已删除')
  await loadAcc()
}

watch(tab, (t) => {
  if (t === 'argocd') loadArgo()
  else if (t === 'accounts') loadAcc()
  else if (['cred', 'lark', 'poll'].includes(t)) loadGlobal()
})

onMounted(loadGlobal)
</script>

<style scoped>
.ss { display: grid; grid-template-columns: 200px 1fr; gap: 0; height: calc(100vh - 120px); }
.rail { background: var(--bg-card); border: 1px solid var(--border); border-radius: var(--radius) 0 0 var(--radius); padding: 16px 0; border-right: none; }
.rail-title { padding: 0 16px; font-size: 10px; text-transform: uppercase; letter-spacing: 1px; color: var(--text-3); font-weight: 600; margin-bottom: 8px; }
.rail-item { padding: 9px 16px; cursor: pointer; font-size: 13px; color: var(--text-2); border-left: 3px solid transparent; }
.rail-item:hover { background: var(--bg-hover); color: var(--text); }
.rail-item.active { background: var(--primary-bg); border-left-color: var(--primary); color: var(--primary); font-weight: 600; }

.pane { flex: 1; overflow: auto; padding: 18px 24px; background: var(--bg-card); border: 1px solid var(--border); border-radius: 0 var(--radius) var(--radius) 0; }
.section { margin-bottom: 14px; }
.sec-head { padding-bottom: 14px; border-bottom: 1px solid var(--border-soft); margin-bottom: 16px; }
.sec-title-row { display: flex; justify-content: space-between; align-items: flex-start; }
.sec-title { font-size: 14px; font-weight: 600; color: var(--text); }
.sec-desc { font-size: 11.5px; color: var(--text-3); margin-top: 3px; }
.actions { margin-top: 14px; text-align: right; }
.slider-row { display: flex; align-items: center; padding: 12px 0; border-bottom: 1px dashed var(--border-soft); }
.lbl { width: 180px; font-size: 12.5px; }
.lbl b { display: block; }
.lbl .desc { color: var(--text-3); font-size: 11px; margin-top: 2px; }
.val { width: 80px; text-align: right; color: var(--primary); font-weight: 600; }
.info-grid { display: grid; grid-template-columns: repeat(3, 1fr); gap: 12px; }
.info { background: var(--bg-input); border: 1px solid var(--border-soft); border-radius: 6px; padding: 12px; }
.info .l { font-size: 11px; color: var(--text-3); text-transform: uppercase; letter-spacing: .5px; }
.info .v { font-size: 13px; margin-top: 4px; font-weight: 500; }

/* 通用表格 */
.tbl { width: 100%; border-collapse: collapse; font-size: 13px; }
.tbl th { background: var(--bg-input); color: var(--text-2); text-align: left; padding: 10px 12px; font-size: 11px; text-transform: uppercase; letter-spacing: .5px; font-weight: 600; border-bottom: 1px solid var(--border); }
.tbl td { padding: 10px 12px; border-bottom: 1px solid var(--border-soft); }
.tbl tr:hover td { background: var(--bg-hover); }
.tbl .mono { font-family: var(--mono); font-size: 12px; }
.tbl .empty-row { text-align: center; color: var(--text-3); padding: 40px 20px; font-size: 12.5px; }

.add-btn { display: flex; align-items: center; gap: 4px; background: var(--primary); color: #fff; border: none; padding: 7px 14px; border-radius: 5px; font: 500 12.5px var(--body); cursor: pointer; }
.add-btn:hover { background: var(--primary-dark); }
.add-btn .el-icon { font-size: 14px; }

.act { background: transparent; border: 1px solid var(--border); color: var(--text-2); padding: 4px 10px; border-radius: 4px; cursor: pointer; font-size: 11.5px; font-family: var(--body); margin-right: 4px; }
.act:hover { border-color: var(--primary); color: var(--primary); }
.act.danger:hover { border-color: var(--danger); color: var(--danger); }
</style>
