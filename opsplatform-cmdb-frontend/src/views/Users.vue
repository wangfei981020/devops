<template>
  <div class="page">
    <div class="page-head">
      <span class="page-title">用户管理</span>
      <div>
        <el-button v-if="canManage" type="primary" :icon="Plus" @click="openCreate">新建本地账号</el-button>
        <el-button :icon="Refresh" @click="load">刷新</el-button>
      </div>
    </div>

    <LoadError :error="error" title="用户列表加载失败" @retry="load" />

    <el-alert type="info" :closable="false" show-icon style="margin-bottom:12px">
      <template #title>两类账号，能做的事不一样</template>
      <div class="tip">
        <b>运维平台</b>：从运维平台单点登录进来的账号，CMDB 这边只是一条影子记录——
        身份、密码、权限都在运维平台管，这里改不了（改了也会在下次登录时被覆盖）。<br>
        <b>本地账号</b>：密码存在 CMDB 自己的库里，<b>不受运维平台权限约束</b>，
        是运维平台不可用时的兜底入口。可以按角色收窄权限（比如只读），
        但至少要保留一个<b>不受限</b>的账号——否则运维平台一挂就没人进得来了。
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
      <el-table-column label="角色 / 权限范围" min-width="170">
        <template #default="{ row }">
          <!-- 「不受限」必须显眼：它是权限最大的一档，看成"还没配"就危险了 -->
          <el-tag v-if="row.auth_source === 'local' && !row.role_code" type="danger" effect="plain" size="small">
            不受限（管理员）
          </el-tag>
          <el-tag v-else-if="row.auth_source === 'local' && row.role_code === 'cmdb_admin'"
                  type="danger" effect="plain" size="small">{{ row.role_name }}</el-tag>
          <span v-else-if="row.auth_source !== 'local'" class="muted">{{ row.role_name }}</span>
          <el-tag v-else size="small" effect="plain">{{ row.role_name }}</el-tag>
          <el-button v-if="canManage && row.can_change_role" link type="primary" size="small"
                     style="margin-left:6px" @click="openRole(row)">改</el-button>
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

    <el-dialog v-model="create.show" title="新建本地账号" width="480px" :close-on-click-modal="false">
      <el-form label-width="90px">
        <el-form-item label="用户名">
          <el-input v-model="create.username" placeholder="登录用的账号名" />
        </el-form-item>
        <el-form-item label="显示名">
          <el-input v-model="create.display_name" placeholder="留空则同用户名" />
        </el-form-item>
        <el-form-item label="密码">
          <el-input v-model="create.password" type="password" show-password placeholder="至少 8 位" />
        </el-form-item>
        <el-form-item label="角色">
          <el-select v-model="create.role_code" placeholder="必选" style="width:100%">
            <el-option v-for="r in roles" :key="r.code" :value="r.code" :label="r.name">
              <div class="ropt">
                <span>{{ r.name }}</span>
                <span class="muted">{{ r.unrestricted ? '不受权限约束' : r.perm_count + ' 项权限' }}</span>
              </div>
            </el-option>
          </el-select>
          <div v-if="pickedRole" class="muted rdesc">{{ pickedRole.description }}</div>
        </el-form-item>
      </el-form>
      <el-alert v-if="pickedRole?.unrestricted" type="warning" :closable="false" show-icon
        title="这个角色不受任何权限约束，等同于超级管理员。只在需要兜底入口时使用。" />
      <template #footer>
        <el-button @click="create.show = false">取消</el-button>
        <el-button type="primary" :loading="create.saving" @click="saveCreate">创建</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="role.show" title="修改角色" width="480px" :close-on-click-modal="false">
      <el-form label-width="90px">
        <el-form-item label="用户"><span class="mono">{{ role.username }}</span></el-form-item>
        <el-form-item label="角色">
          <el-select v-model="role.value" style="width:100%">
            <el-option v-for="r in roles" :key="r.code" :value="r.code" :label="r.name">
              <div class="ropt">
                <span>{{ r.name }}</span>
                <span class="muted">{{ r.unrestricted ? '不受权限约束' : r.perm_count + ' 项权限' }}</span>
              </div>
            </el-option>
          </el-select>
          <div v-if="rolePicked" class="muted rdesc">{{ rolePicked.description }}</div>
        </el-form-item>
      </el-form>
      <!-- 说清楚会踢人。改完不生效才是更糟的结果：降权没生效等于没降 -->
      <el-alert type="warning" :closable="false" show-icon
        title="改角色会立即作废该用户的所有登录会话，需要重新登录后新权限才生效" />
      <template #footer>
        <el-button @click="role.show = false">取消</el-button>
        <el-button type="primary" :loading="role.saving" @click="saveRole">保存</el-button>
      </template>
    </el-dialog>

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
import { Refresh, Key, Delete, SwitchButton, Plus } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { listUsers, changeUserPassword, kickUser, deleteUser,
         listLocalRoles, createUser, changeUserRole } from '../api/cmdb'
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

// 本地账号的角色。SSO 账号不用这个——他们的权限在运维平台配。
const roles = ref([])
const create = reactive({ show: false, username: '', display_name: '', password: '', role_code: '', saving: false })
const role = reactive({ show: false, id: null, username: '', value: '', saving: false })
const pickedRole = computed(() => roles.value.find(r => r.code === create.role_code))
const rolePicked = computed(() => roles.value.find(r => r.code === role.value))

function openCreate() {
  Object.assign(create, { show: true, username: '', display_name: '', password: '', role_code: '', saving: false })
}
async function saveCreate() {
  if (!create.username.trim() || create.password.length < 8) {
    ElMessage.warning('用户名不能为空，密码至少 8 位'); return
  }
  if (!create.role_code) { ElMessage.warning('请选择角色'); return }
  create.saving = true
  try {
    await createUser({ username: create.username.trim(), password: create.password,
                       display_name: create.display_name.trim(), role_code: create.role_code })
    ElMessage.success('已创建')
    create.show = false
    load()
  } catch (e) {
    ElMessage.error(e?.raw?.response?.data?.error || e?.message || '创建失败')
  } finally { create.saving = false }
}

function openRole(row) {
  Object.assign(role, { show: true, id: row.id, username: row.username,
                        value: row.role_code || 'cmdb_admin', saving: false })
}
async function saveRole() {
  role.saving = true
  try {
    const r = await changeUserRole(role.id, role.value)
    ElMessage.success(r?.msg || '已修改')
    role.show = false
    load()
  } catch (e) {
    ElMessage.error(e?.raw?.response?.data?.error || e?.message || '修改失败')
  } finally { role.saving = false }
}

async function load() {
  const r = await run(() => listUsers())
  if (r) rows.value = r.list || []
  // 角色列表拉失败不阻断页面：用户列表本身还是有用的，
  // 只是「新建/改角色」的下拉会是空的
  try { roles.value = (await listLocalRoles()).list || [] } catch (_) { roles.value = [] }
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
.ropt { display: flex; justify-content: space-between; gap: 16px; }
.rdesc { font-size: 12px; line-height: 1.6; margin-top: 4px; }
.tip { font-size: 12.5px; line-height: 1.8; color: #606266; }
.mono { font-family: ui-monospace, Menlo, monospace; }
.muted { color: #909399; font-size: 12px; }
</style>
