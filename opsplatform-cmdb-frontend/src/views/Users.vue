<template>
  <div class="page">
    <div class="page-head">
      <span class="page-title">用户管理</span>
      <div><el-button :icon="Refresh" @click="load">刷新</el-button></div>
    </div>

    <LoadError :error="error" title="用户列表加载失败" @retry="load" />

    <el-alert type="info" :closable="false" show-icon style="margin-bottom:12px">
      <template #title>两类账号，能做的事不一样</template>
      <div class="tip">
        <b>运维平台</b>：从运维平台单点登录进来的账号，CMDB 这边只是一条影子记录——
        身份、密码、权限都在运维平台管，这里改不了（改了也会在下次登录时被覆盖）。<br>
        <b>本地账号</b>：密码存在 CMDB 自己的库里，<b>不受运维平台权限约束</b>，
        是运维平台不可用时的兜底入口。请只保留必要的几个。
      </div>
    </el-alert>

    <el-table :data="rows" v-loading="loading" size="small" border style="width:100%">
      <el-table-column prop="username" label="用户名" min-width="150">
        <template #default="{ row }">
          <span class="mono">{{ row.username }}</span>
          <el-tag v-if="row.is_admin" size="small" type="danger" effect="plain" style="margin-left:6px">管理员</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="display_name" label="显示名" min-width="130">
        <template #default="{ row }">{{ row.display_name || '—' }}</template>
      </el-table-column>
      <el-table-column label="登录方式" width="130">
        <template #default="{ row }">
          <el-tag v-if="row.auth_source === 'portal'" size="small" type="success" effect="dark">运维平台 SSO</el-tag>
          <el-tag v-else size="small" type="warning" effect="dark">本地账号</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="在何处维护" width="110">
        <template #default="{ row }"><span class="muted">{{ row.editable_in }}</span></template>
      </el-table-column>
      <el-table-column label="在线会话" width="90" align="center">
        <template #default="{ row }">
          <el-tag v-if="row.active_sessions" size="small" effect="plain">{{ row.active_sessions }}</el-tag>
          <span v-else class="muted">—</span>
        </template>
      </el-table-column>
      <el-table-column label="最后登录" width="160">
        <template #default="{ row }">{{ row.last_login_at || '从未登录' }}</template>
      </el-table-column>
      <el-table-column prop="created_at" label="创建时间" width="160" />
      <el-table-column v-if="canManage" label="操作" width="200" fixed="right">
        <template #default="{ row }">
          <div style="display:flex;gap:8px;align-items:center">
            <el-tooltip v-if="row.can_change_password" content="改密码">
              <el-button link type="primary" :icon="Key" @click="openPwd(row)" />
            </el-tooltip>
            <el-tooltip v-else content="SSO 账号的密码在运维平台改，这里改了也会被覆盖">
              <el-button link :icon="Key" disabled />
            </el-tooltip>
            <el-tooltip :content="row.active_sessions ? '踢下线（作废其全部会话）' : '当前没有在线会话'">
              <el-button link type="warning" :icon="SwitchButton" :disabled="!row.active_sessions" @click="kick(row)" />
            </el-tooltip>
            <el-tooltip v-if="row.can_delete" content="删除">
              <el-button link type="danger" :icon="Delete" @click="del(row)" />
            </el-tooltip>
            <el-tooltip v-else content="本地 admin 是运维平台不可用时的兜底入口，不能删">
              <el-button link :icon="Delete" disabled />
            </el-tooltip>
          </div>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog v-model="pwd.show" title="修改密码" width="420px" :close-on-click-modal="false">
      <el-form label-width="80px">
        <el-form-item label="用户"><span class="mono">{{ pwd.username }}</span></el-form-item>
        <el-form-item label="新密码">
          <el-input v-model="pwd.value" type="password" show-password placeholder="至少 8 位" />
        </el-form-item>
      </el-form>
      <el-alert type="warning" :closable="false" show-icon
        title="改完密码，该用户当前所有登录会话会立即失效，需要重新登录" />
      <template #footer>
        <el-button @click="pwd.show = false">取消</el-button>
        <el-button type="primary" :loading="pwd.saving" @click="savePwd">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
// 用户管理。
//
// 这个页面存在的主要理由是**把两类账号区分开**：本地账号不受运维平台权限约束
// （perm.go 里对它一律放行），是兜底通道；SSO 账号则是影子记录，本体在运维平台。
// 混在一起看，很容易误以为"在这里删掉某人他就进不来了"——实际上 SSO 用户
// 只要还有运维平台权限，下次登录就会重建。
import { ref, reactive, computed, onMounted } from 'vue'
import { Refresh, Key, Delete, SwitchButton } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { listUsers, changeUserPassword, kickUser, deleteUser } from '../api/cmdb'
import { useLoadState } from '../composables/useLoadState'
import { useAppStore } from '../stores/app'
import { useAuthStore } from '../stores/auth'
import LoadError from '../components/LoadError.vue'

const app = useAppStore()
const auth = useAuthStore()
const canManage = computed(() => auth.hasButton('manage_users'))

const { loading, error, run } = useLoadState()
const rows = ref([])
const pwd = reactive({ show: false, id: null, username: '', value: '', saving: false })

async function load() {
  const r = await run(() => listUsers())
  if (r) rows.value = r.list || []
}
onMounted(load)

function openPwd(row) {
  Object.assign(pwd, { show: true, id: row.id, username: row.username, value: '', saving: false })
}

async function savePwd() {
  if ((pwd.value || '').length < 8) { ElMessage.warning('密码至少 8 位'); return }
  pwd.saving = true
  try {
    const r = await changeUserPassword(pwd.id, pwd.value)
    ElMessage.success(r.msg || '已更新')
    pwd.show = false
    load()
  } catch (e) {
    ElMessage.error(e?.raw?.response?.data?.error || e?.message || '修改失败')
  } finally { pwd.saving = false }
}

async function kick(row) {
  try {
    await app.showConfirm(
      `将作废 ${row.username} 的全部 ${row.active_sessions} 个登录会话，该用户需要重新登录。继续？`,
      '踢下线')
  } catch (_) { return }
  try {
    const r = await kickUser(row.id)
    ElMessage.success(r.msg || '已踢下线')
    load()
  } catch (e) {
    ElMessage.error(e?.raw?.response?.data?.error || '操作失败')
  }
}

async function del(row) {
  const extra = row.auth_source === 'portal'
    ? '\n\n注意：这只是删掉 CMDB 里的影子记录。此人若仍有运维平台权限，下次 SSO 登录会自动重建——要真正收回访问权，请到运维平台调整他的角色。'
    : ''
  try {
    await app.showConfirm(`确定删除用户 ${row.username}？${extra}`, '删除用户')
  } catch (_) { return }
  try {
    const r = await deleteUser(row.id)
    ElMessage.success(r.msg || '已删除')
    load()
  } catch (e) {
    const d = e?.raw?.response?.data
    ElMessage.error(d?.error || '删除失败')
    if (d?.hint) ElMessage.info(d.hint)
  }
}
</script>

<style scoped>
.tip { font-size: 12.5px; line-height: 1.8; color: #606266; }
.mono { font-family: ui-monospace, Menlo, monospace; }
.muted { color: #909399; font-size: 12px; }
</style>
