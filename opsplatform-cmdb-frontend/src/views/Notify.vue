<template>
  <div class="page">
    <div class="page-head"><span class="page-title">通知</span></div>

    <!-- Lark 群 -->
    <el-card shadow="never" style="margin-bottom:14px">
      <template #header><b>Lark 群</b><span class="muted" style="margin-left:8px">定时任务按群发通知，可加多个</span>
        <el-button v-if="canManage" type="primary" size="small" style="float:right" @click="openGroup()">+ 添加群</el-button></template>
      <el-table :data="gPaged" size="small">
        <el-table-column prop="name" label="群名" width="180" />
        <el-table-column prop="webhook" label="Webhook" min-width="320" show-overflow-tooltip><template #default="{ row }"><span class="mono">{{ row.webhook }}</span></template></el-table-column>
        <el-table-column label="操作" width="170" fixed="right"><template #default="{ row }">
          <div style="display:flex;gap:8px;align-items:center">
            <el-tooltip v-if="canManage" content="编辑"><el-button link type="primary" :icon="Edit" @click="openGroup(row)" /></el-tooltip>
            <el-tooltip v-if="canManage" content="删除"><el-button link type="danger" :icon="Delete" @click="delGroup(row)" /></el-tooltip>
            <el-button v-if="canManage" link type="primary" :icon="Promotion" :loading="testingG[row.id]" @click="testGroup(row)">测试</el-button>
          </div>
        </template></el-table-column>
      </el-table>
      <el-pagination v-if="groups.length > gSize" v-model:current-page="gPage" v-model:page-size="gSize" :page-sizes="[10,20,50,100]"
        :total="groups.length" layout="total, sizes, prev, pager, next" style="margin-top:12px; justify-content:flex-end" />
      <el-empty v-if="!groups.length" description="还没有 Lark 群，点右上添加" :image-size="50" />
    </el-card>

    <el-dialog :close-on-click-modal="false" :close-on-press-escape="false" v-model="gDlg" :title="gEdit?'编辑群':'添加群'" width="560px">
      <el-form :model="gForm" label-width="90px">
        <el-form-item label="群名"><el-input v-model="gForm.name" placeholder="如 运维群 / 证书群" style="width:240px" /></el-form-item>
        <el-form-item label="Webhook"><el-input v-model="gForm.webhook" placeholder="飞书自定义机器人 webhook 地址" /></el-form-item>
      </el-form>
      <template #footer><el-button @click="gDlg=false">取消</el-button><el-button type="primary" @click="saveGroup">保存</el-button></template>
    </el-dialog>

    <!-- 通知人 -->
    <el-card shadow="never" style="margin-bottom:14px">
      <template #header><b>通知人</b><span class="muted" style="margin-left:8px">告警时 @ 这些人（飞书 open_id，ou_ 开头）</span></template>
      <el-form inline @submit.prevent="addUser" style="margin-bottom:12px">
        <el-form-item label="姓名"><el-input v-model="nu.name" style="width:140px" /></el-form-item>
        <el-form-item label="open_id"><el-input v-model="nu.open_id" placeholder="ou_xxxxxxxx" style="width:260px" /></el-form-item>
        <el-button v-if="canManage" type="primary" :icon="Plus" @click="addUser">添加</el-button>
      </el-form>
      <el-table :data="uPaged" size="small">
        <el-table-column prop="name" label="姓名" width="160" />
        <el-table-column prop="open_id" label="open_id" min-width="260" />
        <el-table-column label="操作" width="80" fixed="right"><template #default="{ row }">
          <el-tooltip v-if="canManage" content="删除"><el-button link type="danger" :icon="Delete" @click="delUser(row)" /></el-tooltip>
        </template></el-table-column>
      </el-table>
      <el-pagination v-if="users.length > uSize" v-model:current-page="uPage" v-model:page-size="uSize" :page-sizes="[10,20,50,100]"
        :total="users.length" layout="total, sizes, prev, pager, next" style="margin-top:12px; justify-content:flex-end" />
      <el-empty v-if="!users.length" description="还没有通知人，告警将不 @ 人" :image-size="50" />
    </el-card>

    <!-- 通知规则 -->
    <el-card shadow="never">
      <template #header><b>通知规则</b></template>
      <!-- 没有群 = 所有开关都是摆设。这一条必须显眼：规则页看起来配置齐全、开关全开，
           但一个 Lark 群都没配时，飞书那一侧收不到任何东西——磁盘打满、节点 NotReady、
           证书过期全都发不出去，而页面上没有任何迹象表明这一点。
           这是典型的"沉默失效"：配置看着完整，实际不工作。 -->
      <el-alert v-if="!groups.length" type="error" :closable="false" show-icon style="margin-bottom:14px"
        title="下面的规则当前一条也发不出去">
        <template #default>
          还没有配置任何 Lark 群，所有告警投递都会落空（规则开关只控制"要不要发"，不解决"发给谁"）。
          请先在本页最上方「Lark 群」添加至少一个群并点「测试」验证连通。
        </template>
      </el-alert>
      <el-alert v-else-if="!users.length" type="warning" :closable="false" show-icon style="margin-bottom:14px"
        title="告警能发出，但不会 @ 到任何人">
        <template #default>
          已配置 {{ groups.length }} 个群，但通知人为空——消息会进群，不会 @ 具体的人，紧急告警容易被漏看。
        </template>
      </el-alert>
      <el-form label-width="120px" style="max-width:620px">
        <el-form-item label="到期提醒天数">
          <el-input v-model="cfg.remind_days" placeholder="30,15,7,1" style="width:240px" />
          <span class="muted" style="margin-left:8px">逗号分隔，剩余天数命中即提醒</span>
        </el-form-item>
        <el-divider content-position="left"><span class="muted">证书 / 域名</span></el-divider>
        <el-form-item label="证书即将到期"><el-switch v-model="ev.notify_cert_expiring" active-value="1" inactive-value="0" /></el-form-item>
        <el-form-item label="续期成功通知"><el-switch v-model="ev.notify_renew_success" active-value="1" inactive-value="0" /></el-form-item>
        <el-form-item label="续期失败告警"><el-switch v-model="ev.notify_renew_fail" active-value="1" inactive-value="0" /></el-form-item>
        <el-form-item label="域名即将到期"><el-switch v-model="ev.notify_domain_expiring" active-value="1" inactive-value="0" /></el-form-item>

        <!-- 这三类此前没有开关，想静音只能去「定时任务」把整个任务停掉，
             而停任务连数据采集也一起没了。关开关只停投递，任务照跑（CMDB-026） -->
        <el-divider content-position="left"><span class="muted">集群</span></el-divider>
        <el-form-item label="GKE 升级预警">
          <el-switch v-model="ev.notify_gke_upgrade" active-value="1" inactive-value="0" />
          <span class="muted" style="margin-left:8px">强制升级倒计时与版本偏斜</span>
        </el-form-item>
        <el-form-item label="磁盘水位告警">
          <el-switch v-model="ev.notify_disk_watch" active-value="1" inactive-value="0" />
          <span class="muted" style="margin-left:8px">盘满会直接打垮全站，建议保持开启</span>
        </el-form-item>
        <el-form-item label="节点健康预警">
          <el-switch v-model="ev.notify_node_health" active-value="1" inactive-value="0" />
          <span class="muted" style="margin-left:8px">节点 NotReady / 卡死，90 秒一轮</span>
        </el-form-item>

        <el-alert type="info" :closable="false" show-icon style="margin:4px 0 14px">
          关掉开关只停止<b>飞书投递</b>，对应的定时任务照常执行，数据仍写库、看板照常可见。
          要连采集一起停，去「定时任务」页关任务。
        </el-alert>

        <el-button v-if="canManage" type="primary" @click="saveCfg">保存规则</el-button>
      </el-form>
    </el-card>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Plus, Delete, Promotion, Edit } from '@element-plus/icons-vue'
import { getSettings, updateSettings, listNotifyUsers, createNotifyUser, deleteNotifyUser,
  listLarkGroups, createLarkGroup, updateLarkGroup, deleteLarkGroup, testLarkGroup } from '../api/cmdb'
import { useAppStore } from '../stores/app'
import { useAuthStore } from '../stores/auth'
import { usePaged } from '../composables/usePaged'

const app = useAppStore()
const auth = useAuthStore()
const canManage = computed(() => auth.hasButton('manage_notify'))
const cfg = ref({}), users = ref([])
const nu = ref({ name: '', open_id: '' })
const groups = ref([]), testingG = ref({})
const { page: gPage, size: gSize, paged: gPaged } = usePaged(groups)
const { page: uPage, size: uSize, paged: uPaged } = usePaged(users)
const gDlg = ref(false), gEdit = ref(false), gForm = ref({})
// 事件开关，settings 没存按默认（续期成功默认关，其余开）
// 默认值必须与后端 alertEnabled() 的默认一致：settings 里没存该 key 时后端按「开」处理，
// 所以这里除了「续期成功」这种噪音类，其余都默认 '1'，避免界面显示关、实际却在发
const ev = ref({
  notify_cert_expiring: '1', notify_renew_success: '0', notify_renew_fail: '1', notify_domain_expiring: '1',
  notify_gke_upgrade: '1', notify_disk_watch: '1', notify_node_health: '1',
})

async function load() {
  cfg.value = await getSettings()
  for (const k in ev.value) if (cfg.value[k] !== undefined && cfg.value[k] !== '') ev.value[k] = cfg.value[k]
  users.value = await listNotifyUsers()
  groups.value = await listLarkGroups()
}
async function saveCfg() {
  try {
    await updateSettings({ remind_days: cfg.value.remind_days || '', ...ev.value })
    ElMessage.success('已保存')
  } catch (e) { ElMessage.error('保存失败') }
}
// Lark 群
function openGroup(row) { gEdit.value = !!row; gForm.value = row ? { ...row } : { name: '', webhook: '' }; gDlg.value = true }
async function saveGroup() {
  if (!gForm.value.name || !gForm.value.webhook) { ElMessage.warning('群名和 webhook 必填'); return }
  try {
    gEdit.value ? await updateLarkGroup(gForm.value.id, gForm.value) : await createLarkGroup(gForm.value)
    ElMessage.success('已保存'); gDlg.value = false; groups.value = await listLarkGroups()
  } catch (e) { ElMessage.error(e.response?.data?.error || '失败') }
}
async function delGroup(row) {
  try { await app.showConfirm(`删除群 ${row.name}？引用它的任务将不发通知`); await deleteLarkGroup(row.id); groups.value = await listLarkGroups() }
  catch (e) { if (e !== 'cancel') ElMessage.error('失败') }
}
async function testGroup(row) {
  testingG.value = { ...testingG.value, [row.id]: true }
  try { const r = await testLarkGroup(row.id); ElMessage.success(r.msg || '已发送') }
  catch (e) { ElMessage.error(e.response?.data?.error || '发送失败') }
  finally { testingG.value = { ...testingG.value, [row.id]: false } }
}
async function addUser() {
  if (!nu.value.name || !nu.value.open_id) { ElMessage.warning('姓名和 open_id 必填'); return }
  try { await createNotifyUser(nu.value); nu.value = { name: '', open_id: '' }; users.value = await listNotifyUsers(); ElMessage.success('已添加') }
  catch (e) { ElMessage.error(e.response?.data?.error || '失败') }
}
async function delUser(row) {
  try { await app.showConfirm(`删除通知人 ${row.name}？`); await deleteNotifyUser(row.id); users.value = await listNotifyUsers() }
  catch (e) { if (e !== 'cancel') ElMessage.error('失败') }
}
onMounted(load)
</script>

<style scoped>
.mono { font-family: ui-monospace, Menlo, Consolas, monospace; font-size: 12px; }
</style>
