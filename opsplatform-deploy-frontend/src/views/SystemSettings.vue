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

      <!-- 用户管理：平台登录账号（admin 可用）-->
      <div v-if="tab === 'accounts'" class="section">
        <div class="sec-head">
          <div class="sec-title-row">
            <div>
              <div class="sec-title">用户管理</div>
              <div class="sec-desc">登录此平台的账号 · portal 用户由运维平台 SSO 自动创建 · Lark 艾特请到「通知人」配置</div>
            </div>
            <button v-if="authStore.isAdmin" class="add-btn" @click="openUserCreate">
              <el-icon><Plus /></el-icon>新增本地用户
            </button>
          </div>
        </div>
        <div class="sec-body">
          <div v-if="!authStore.isAdmin" class="empty-row" style="padding:40px 20px;text-align:center;color:var(--text-3)">
            仅管理员可查看 · 需要 admin 角色
          </div>
          <table v-else class="tbl">
            <thead>
              <tr>
                <th style="width:140px">用户名</th>
                <th style="width:140px">显示名</th>
                <th style="width:90px">角色</th>
                <th style="width:90px">来源</th>
                <th style="width:80px">状态</th>
                <th style="width:240px">操作</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="u in users" :key="u.id">
                <td class="mono"><b>{{ u.username }}</b></td>
                <td>{{ u.display_name || '—' }}</td>
                <td>
                  <span :class="['role-tag', u.role]">{{ u.role }}</span>
                </td>
                <td>
                  <span :class="['src-tag', u.auth_source]">{{ u.auth_source }}</span>
                </td>
                <td>
                  <span v-if="u.status === 1" class="status-on">启用</span>
                  <span v-else class="status-off">禁用</span>
                </td>
                <td>
                  <button class="act" @click="openUserEdit(u)">编辑</button>
                  <button v-if="u.auth_source === 'local'" class="act" @click="onResetPwd(u)">重置密码</button>
                  <button class="act" @click="onToggleUser(u)">{{ u.status === 1 ? '禁用' : '启用' }}</button>
                  <button class="act danger" @click="onDeleteUser(u)">删除</button>
                </td>
              </tr>
              <tr v-if="!users.length">
                <td colspan="6" class="empty-row">还没有用户</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>

      <!-- 通知人：Lark 艾特专用 -->
      <div v-if="tab === 'contacts'" class="section">
        <div class="sec-head">
          <div class="sec-title-row">
            <div>
              <div class="sec-title">通知人</div>
              <div class="sec-desc">发布后根据操作人名字匹配 Lark ID · 艾特用</div>
            </div>
            <button class="add-btn" @click="openContactCreate">
              <el-icon><Plus /></el-icon>新增通知人
            </button>
          </div>
        </div>
        <div class="sec-body">
          <table class="tbl">
            <thead>
              <tr>
                <th style="width:200px">名称</th>
                <th>Lark ID</th>
                <th>备注</th>
                <th style="width:120px">操作</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="c in contacts" :key="c.id">
                <td class="mono"><b>{{ c.name }}</b></td>
                <td class="mono">{{ c.lark_id || '—' }}</td>
                <td>{{ c.remark || '—' }}</td>
                <td>
                  <button class="act" @click="openContactEdit(c)">编辑</button>
                  <button class="act danger" @click="onDeleteContact(c)">删除</button>
                </td>
              </tr>
              <tr v-if="!contacts.length">
                <td colspan="4" class="empty-row">还没有通知人，点右上「+ 新增通知人」添加</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>

      <!-- Lark 机器人：多 webhook -->
      <div v-if="tab === 'larkbots'" class="section">
        <div class="sec-head">
          <div class="sec-title-row">
            <div>
              <div class="sec-title">Lark 机器人</div>
              <div class="sec-desc">全局可配置多个 webhook · 项目环境只需选一个</div>
            </div>
            <button class="add-btn" @click="openBotCreate">
              <el-icon><Plus /></el-icon>新增机器人
            </button>
          </div>
        </div>
        <div class="sec-body">
          <table class="tbl">
            <thead>
              <tr>
                <th style="width:160px">名称</th>
                <th>Webhook</th>
                <th>描述</th>
                <th style="width:180px">操作</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="b in larkBots" :key="b.id">
                <td class="mono"><b>{{ b.name }}</b></td>
                <td class="mono webhook-cell">{{ b.webhook }}</td>
                <td>{{ b.description || '—' }}</td>
                <td>
                  <button class="act" @click="onTestBot(b)">测试</button>
                  <button class="act" @click="openBotEdit(b)">编辑</button>
                  <button class="act danger" @click="onDeleteBot(b)">删除</button>
                </td>
              </tr>
              <tr v-if="!larkBots.length">
                <td colspan="4" class="empty-row">还没有 Lark 机器人，点右上「+ 新增机器人」添加</td>
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
            <div class="info"><div class="l">后端版本</div><div class="v mono">v35</div></div>
            <div class="info"><div class="l">前端版本</div><div class="v mono">v44</div></div>
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

    <!-- 用户弹窗 -->
    <el-dialog v-model="userDlg.vis" :title="userDlg.isEdit ? '编辑用户' : '新增本地用户'" width="520px">
      <el-form :model="userDlg.form" label-width="100px" label-position="top" size="default">
        <el-form-item label="用户名 *">
          <el-input v-model="userDlg.form.username" :disabled="userDlg.isEdit" class="mono" placeholder="如: zhangsan" />
        </el-form-item>
        <el-form-item v-if="!userDlg.isEdit" label="初始密码 *">
          <el-input v-model="userDlg.form.password" type="password" show-password placeholder="用户首次登录后请修改" />
        </el-form-item>
        <el-form-item label="显示名">
          <el-input v-model="userDlg.form.display_name" placeholder="如: 张三" />
        </el-form-item>
        <el-form-item label="角色">
          <el-select v-model="userDlg.form.role" style="width:100%">
            <el-option value="user" label="user - 普通用户" />
            <el-option value="admin" label="admin - 管理员" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="userDlg.vis = false">取消</el-button>
        <el-button type="primary" @click="onSaveUser">保存</el-button>
      </template>
    </el-dialog>

    <!-- 通知人弹窗 -->
    <el-dialog v-model="contactDlg.vis" :title="contactDlg.isEdit ? '编辑通知人' : '新增通知人'" width="520px">
      <el-form :model="contactDlg.form" label-width="100px" label-position="top" size="default">
        <el-form-item label="名称 *">
          <el-input v-model="contactDlg.form.name" :disabled="contactDlg.isEdit" placeholder="如: 张三 或 zhangsan（和操作人名字一致时自动匹配）" />
        </el-form-item>
        <el-form-item label="Lark ID">
          <el-input v-model="contactDlg.form.lark_id" class="mono" placeholder="ou_xxxxxxxxxxxxxxxxxxxx" />
        </el-form-item>
        <el-form-item label="备注">
          <el-input v-model="contactDlg.form.remark" type="textarea" :rows="2" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="contactDlg.vis = false">取消</el-button>
        <el-button type="primary" @click="onSaveContact">保存</el-button>
      </template>
    </el-dialog>

    <!-- Lark 机器人弹窗 -->
    <el-dialog v-model="botDlg.vis" :title="botDlg.isEdit ? '编辑 Lark 机器人' : '新增 Lark 机器人'" width="560px">
      <el-form :model="botDlg.form" label-width="100px" label-position="top" size="default">
        <el-form-item label="名称 *">
          <el-input v-model="botDlg.form.name" :disabled="botDlg.isEdit" class="mono" placeholder="如: uat-deploy" />
        </el-form-item>
        <el-form-item label="Webhook *">
          <el-input v-model="botDlg.form.webhook" class="mono" placeholder="https://open.larksuite.com/open-apis/bot/v2/hook/..." />
        </el-form-item>
        <el-form-item :label="botDlg.isEdit ? 'Secret（留空不更新）' : 'Secret'">
          <el-input v-model="botDlg.form.secret" type="password" show-password placeholder="机器人开启签名校验才需要" />
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="botDlg.form.description" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="botDlg.vis = false">取消</el-button>
        <el-button type="primary" @click="onSaveBot">保存</el-button>
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
  listUsers, createUser, updateUser, toggleUser, resetUserPassword, deleteUser,
  listContacts, createContact, updateContact, deleteContact,
  listLarkBots, createLarkBot, updateLarkBot, deleteLarkBot, testLarkBot,
  listArgocdInstances, createArgocdInstance, updateArgocdInstance, deleteArgocdInstance, testArgocdInstance
} from '../api'
import { useAuthStore } from '../stores/auth'
const authStore = useAuthStore()

const tab = ref('cred')
const tabs = [
  { v: 'cred', label: '全局凭证' },
  { v: 'argocd', label: 'ArgoCD 实例' },
  { v: 'larkbots', label: 'Lark 机器人' },
  { v: 'accounts', label: '账号管理' },
  { v: 'contacts', label: '通知人' },
  { v: 'lark', label: 'Lark 默认' },
  { v: 'poll', label: '同步策略' },
  { v: 'about', label: '关于' }
]

const gc = reactive({
  gitlab_url: '', gitlab_user: '', gitlab_email: '', gitlab_token: '',
  lark_default_webhook: '', lark_default_secret: '',
  poll_interval_sec: 10, poll_timeout_min: 3, git_retry_count: 3
})
const users = ref([])
const contacts = ref([])
const larkBots = ref([])
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

// === 用户（登录）===
const userDlg = reactive({ vis: false, isEdit: false, editingID: null, form: { username: '', password: '', display_name: '', role: 'user' } })
async function loadUsers() {
  if (!authStore.isAdmin) return
  users.value = (await listUsers()) || []
}
function openUserCreate() {
  userDlg.isEdit = false; userDlg.editingID = null
  Object.assign(userDlg.form, { username: '', password: '', display_name: '', role: 'user' })
  userDlg.vis = true
}
function openUserEdit(u) {
  userDlg.isEdit = true; userDlg.editingID = u.id
  Object.assign(userDlg.form, { username: u.username, password: '', display_name: u.display_name, role: u.role })
  userDlg.vis = true
}
async function onSaveUser() {
  if (!userDlg.isEdit) {
    if (!userDlg.form.username.trim()) { ElMessage.warning('用户名必填'); return }
    if (!userDlg.form.password) { ElMessage.warning('初始密码必填'); return }
    await createUser(userDlg.form)
  } else {
    await updateUser(userDlg.editingID, { display_name: userDlg.form.display_name, role: userDlg.form.role })
  }
  ElMessage.success('已保存')
  userDlg.vis = false
  await loadUsers()
}
async function onDeleteUser(u) {
  try { await ElMessageBox.confirm(`确认删除用户「${u.username}」？`, '删除确认', { type: 'warning' }) }
  catch { return }
  await deleteUser(u.id)
  ElMessage.success('已删除')
  await loadUsers()
}
async function onToggleUser(u) {
  await toggleUser(u.id)
  ElMessage.success(u.status === 1 ? '已禁用' : '已启用')
  await loadUsers()
}
async function onResetPwd(u) {
  try {
    const { value } = await ElMessageBox.prompt(`为「${u.username}」设置新密码`, '重置密码', {
      inputType: 'password', inputPlaceholder: '至少 6 位',
      inputValidator: v => !!v && v.length >= 6 || '密码至少 6 位',
    })
    await resetUserPassword(u.id, value)
    ElMessage.success('密码已重置')
  } catch (_) { /* 取消 */ }
}

// === 通知人（Lark 艾特） ===
const contactDlg = reactive({ vis: false, isEdit: false, editingID: null, form: { name: '', lark_id: '', remark: '' } })
async function loadContacts() { contacts.value = (await listContacts()) || [] }
function openContactCreate() {
  contactDlg.isEdit = false; contactDlg.editingID = null
  Object.assign(contactDlg.form, { name: '', lark_id: '', remark: '' })
  contactDlg.vis = true
}
function openContactEdit(c) {
  contactDlg.isEdit = true; contactDlg.editingID = c.id
  Object.assign(contactDlg.form, { name: c.name, lark_id: c.lark_id, remark: c.remark })
  contactDlg.vis = true
}
async function onSaveContact() {
  if (!contactDlg.isEdit && !contactDlg.form.name.trim()) { ElMessage.warning('名称必填'); return }
  if (contactDlg.isEdit) await updateContact(contactDlg.editingID, contactDlg.form)
  else await createContact(contactDlg.form)
  ElMessage.success('已保存')
  contactDlg.vis = false
  await loadContacts()
}
async function onDeleteContact(c) {
  try { await ElMessageBox.confirm(`确认删除通知人「${c.name}」？`, '删除确认', { type: 'warning' }) }
  catch { return }
  await deleteContact(c.id)
  ElMessage.success('已删除')
  await loadContacts()
}

// === Lark 机器人 ===
const botDlg = reactive({ vis: false, isEdit: false, editingID: null, form: { name: '', webhook: '', secret: '', description: '' } })
async function loadBots() { larkBots.value = (await listLarkBots()) || [] }
function openBotCreate() {
  botDlg.isEdit = false; botDlg.editingID = null
  Object.assign(botDlg.form, { name: '', webhook: '', secret: '', description: '' })
  botDlg.vis = true
}
function openBotEdit(b) {
  botDlg.isEdit = true; botDlg.editingID = b.id
  Object.assign(botDlg.form, { name: b.name, webhook: b.webhook, secret: '', description: b.description || '' })
  botDlg.vis = true
}
async function onSaveBot() {
  if (!botDlg.isEdit && !botDlg.form.name.trim()) { ElMessage.warning('名称必填'); return }
  if (!botDlg.form.webhook.trim()) { ElMessage.warning('Webhook 必填'); return }
  const payload = { ...botDlg.form }
  if (botDlg.isEdit && !payload.secret) delete payload.secret
  if (botDlg.isEdit) await updateLarkBot(botDlg.editingID, payload)
  else await createLarkBot(payload)
  ElMessage.success('已保存')
  botDlg.vis = false
  await loadBots()
}
async function onDeleteBot(b) {
  try { await ElMessageBox.confirm(`确认删除 Lark 机器人「${b.name}」？被项目环境引用时会失败。`, '删除确认', { type: 'warning' }) }
  catch { return }
  await deleteLarkBot(b.id)
  ElMessage.success('已删除')
  await loadBots()
}
async function onTestBot(b) {
  try {
    await testLarkBot(b.id)
    ElMessage.success(`已发送测试消息到「${b.name}」`)
  } catch (_) {}
}

watch(tab, (t) => {
  if (t === 'argocd') loadArgo()
  else if (t === 'accounts') loadUsers()
  else if (t === 'contacts') loadContacts()
  else if (t === 'larkbots') loadBots()
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

.webhook-cell { max-width: 360px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

.role-tag { display: inline-block; padding: 2px 8px; border-radius: 99px; font: 500 11px var(--mono); }
.role-tag.admin { background: #fef2f2; color: #dc2626; }
.role-tag.user { background: #eff6ff; color: #1d4ed8; }

.src-tag { display: inline-block; padding: 2px 8px; border-radius: 99px; font: 500 11px var(--mono); }
.src-tag.local { background: #f3f4f6; color: #4b5563; }
.src-tag.portal { background: #ecfdf5; color: #059669; }

.status-on { color: var(--success); font-size: 12px; font-weight: 500; }
.status-off { color: var(--text-3); font-size: 12px; }
</style>
