<template>
  <div>
    <div class="tip">
      Harbor <b>只读</b>接入：查镜像仓库的健康、存储配额、GC 状态与镜像列表。
      <br>
      补的是发布链路上「推送到 Harbor」和「拉取镜像」两个环节 ——
      配额快满时推送会失败、Harbor 组件异常会导致 <code>ImagePullBackOff</code>，此前都要登控制台才看得到。
      <br>
      <span class="warn">只需只读权限</span>：建机器人账号，勾「项目只读 + 系统只读」即可，
      <b>不要</b>给推送/删除权限。用户名要带 <code>robot$</code> 前缀。
    </div>

    <div style="margin-bottom:10px">
      <el-button type="primary" size="small" @click="open()">+ 添加 Harbor</el-button>
      <el-button size="small" :icon="Refresh" :loading="loading" @click="load">刷新</el-button>
    </div>

    <el-table :data="rows" size="small" v-loading="loading">
      <el-table-column prop="name" label="名称" min-width="140" />
      <el-table-column prop="url" label="地址" min-width="220" show-overflow-tooltip />
      <el-table-column prop="username" label="账号" width="150" show-overflow-tooltip>
        <template #default="{ row }">{{ row.username || '匿名' }}</template>
      </el-table-column>
      <el-table-column label="密码" width="80"><template #default="{ row }">
        <el-tag size="small" :type="row.has_secret ? 'success' : 'info'">{{ row.has_secret ? '已配' : '未配' }}</el-tag>
      </template></el-table-column>
      <el-table-column prop="env" label="环境" width="80"><template #default="{ row }">
        {{ row.env || '通用' }}
      </template></el-table-column>
      <el-table-column label="启用" width="70"><template #default="{ row }">
        <el-tag size="small" :type="row.enabled ? 'success' : 'info'">{{ row.enabled ? '是' : '否' }}</el-tag>
      </template></el-table-column>
      <el-table-column label="操作" width="200" fixed="right"><template #default="{ row }">
        <el-button link type="warning" size="small" :loading="busy['t' + row.id]" @click="test(row)">测试</el-button>
        <el-button link type="success" size="small" :loading="busy['s' + row.id]" @click="showStatus(row)">状态</el-button>
        <el-button link type="primary" size="small" @click="open(row)">编辑</el-button>
        <el-button link type="danger" size="small" @click="del(row)">删除</el-button>
      </template></el-table-column>
    </el-table>
    <el-empty v-if="!loading && !rows.length" description="还没接入 Harbor，点上面「添加」" :image-size="60" />

    <el-dialog :close-on-click-modal="false" :close-on-press-escape="false" v-model="dlg"
      :title="editing ? '编辑 Harbor 接入' : '添加 Harbor 接入'" width="560px">
      <el-form :model="form" label-width="110px">
        <el-form-item label="名称"><el-input v-model="form.name" placeholder="如 生产Harbor" /></el-form-item>
        <el-form-item label="地址">
          <el-input v-model="form.url" placeholder="https://harbor.example.com" />
          <div class="muted">不用带 /api/v2.0，只填到域名</div>
        </el-form-item>
        <el-form-item label="用户名"><el-input v-model="form.username" placeholder="robot$cmdb-readonly" /></el-form-item>
        <el-form-item label="密码/令牌">
          <el-input v-model="form.password" type="password" show-password
            :placeholder="editing ? '留空 = 保留原密码不变' : '机器人账号的令牌'" />
          <div class="muted">出于安全，已保存的密码不会回显。</div>
        </el-form-item>
        <el-form-item label="适用环境">
          <el-select v-model="form.env" clearable placeholder="留空=通用" style="width:200px">
            <el-option v-for="e in ['PROD', 'UAT', 'DEV']" :key="e" :label="e" :value="e" />
          </el-select>
        </el-form-item>
        <el-form-item label="跳过证书校验">
          <el-switch v-model="form.skip_verify" />
          <span class="muted" style="margin-left:8px">内网自签证书的 Harbor 才需要开</span>
        </el-form-item>
        <el-form-item label="启用"><el-switch v-model="form.enabled" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dlg = false">取消</el-button>
        <el-button type="primary" @click="save">保存</el-button>
      </template>
    </el-dialog>

    <el-dialog :close-on-click-modal="false" :close-on-press-escape="false" v-model="statusDlg"
      title="Harbor 状态" width="620px">
      <div v-if="st">
        <el-descriptions :column="2" border size="small">
          <el-descriptions-item label="健康">
            <el-tag size="small" :type="st.health === 'healthy' ? 'success' : 'danger'">{{ st.health || '未知' }}</el-tag>
          </el-descriptions-item>
          <el-descriptions-item label="组件数">{{ st.component_count ?? '—' }}</el-descriptions-item>
          <el-descriptions-item label="项目数">{{ st.projects ?? '—' }}</el-descriptions-item>
          <el-descriptions-item label="仓库数">{{ st.repositories ?? '—' }}</el-descriptions-item>
          <el-descriptions-item label="已用存储">{{ st.storage_used_gb ?? '—' }} GB</el-descriptions-item>
          <el-descriptions-item label="上次 GC">
            <span v-if="!st.gc">—</span>
            <el-tag v-else-if="st.gc.last_status === 'never'" size="small" type="danger">从未执行</el-tag>
            <span v-else>{{ st.gc.last_status }}<span v-if="st.gc.days_ago != null" class="muted">（{{ st.gc.days_ago }} 天前）</span></span>
          </el-descriptions-item>
        </el-descriptions>
        <el-alert v-if="st.gc?.issue" :title="st.gc.issue" type="warning" :closable="false" show-icon style="margin-top:10px" />
        <el-alert v-if="st.issue" :title="st.issue" type="error" :closable="false" show-icon style="margin-top:10px" />

        <div v-if="projects.length" style="margin-top:14px">
          <div class="sub">项目配额（按用量比排序）</div>
          <el-table :data="projects" size="small" max-height="240">
            <el-table-column prop="name" label="项目" min-width="140" />
            <el-table-column prop="repo_count" label="仓库" width="70" />
            <el-table-column label="已用" width="100"><template #default="{ row }">{{ row.used_gb }} GB</template></el-table-column>
            <el-table-column label="配额" width="110"><template #default="{ row }">
              <span v-if="row.quota_gb < 0" class="muted">未设限</span>
              <span v-else>{{ row.quota_gb }} GB</span>
            </template></el-table-column>
            <el-table-column label="用量" min-width="150"><template #default="{ row }">
              <span v-if="row.used_pct < 0" class="muted">—</span>
              <el-progress v-else :percentage="Math.min(row.used_pct, 100)" :stroke-width="10"
                :status="row.severity === 'high' ? 'exception' : row.severity === 'medium' ? 'warning' : undefined" />
            </template></el-table-column>
          </el-table>
        </div>
      </div>
      <template #footer><el-button @click="statusDlg = false">关闭</el-button></template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive } from 'vue'
import { ElMessage } from 'element-plus'
import { Refresh } from '@element-plus/icons-vue'
import {
  listHarborRegistries, saveHarborRegistry, deleteHarborRegistry,
  testHarborRegistry, harborStatus, harborProjects,
} from '../../api/cmdb'
import { useAppStore } from '../../stores/app'

const app = useAppStore()
const rows = ref([]); const loading = ref(false); const busy = reactive({})
const dlg = ref(false); const editing = ref(false)
const statusDlg = ref(false); const st = ref(null); const projects = ref([])
const form = reactive({ id: 0, name: '', url: '', username: '', password: '', env: '', skip_verify: false, enabled: true })

async function load() {
  loading.value = true
  try { rows.value = await listHarborRegistries() }
  catch (e) { ElMessage.error('加载失败') } finally { loading.value = false }
}

function open(row) {
  editing.value = !!row
  // 编辑时密码一律留空：后端约定「留空=不修改」，避免把已存的密码误清空
  Object.assign(form, row
    ? { id: row.id, name: row.name, url: row.url, username: row.username, password: '', env: row.env, skip_verify: !!row.skip_verify, enabled: !!row.enabled }
    : { id: 0, name: '', url: '', username: '', password: '', env: '', skip_verify: false, enabled: true })
  dlg.value = true
}

async function save() {
  if (!form.name || !form.url) { ElMessage.warning('名称和地址必填'); return }
  try {
    await saveHarborRegistry({ ...form })
    ElMessage.success('已保存'); dlg.value = false; load()
  } catch (e) { ElMessage.error(e.response?.data?.error || '保存失败') }
}

async function del(row) {
  try {
    await app.showConfirm(`删除 Harbor 接入「${row.name}」？只删除 CMDB 里的接入配置，不影响 Harbor 本身。`)
    await deleteHarborRegistry(row.id); ElMessage.success('已删除'); load()
  } catch (e) { if (e !== 'cancel') ElMessage.error('删除失败') }
}

// 后端的测试不只验凭证，还会多探一次 /projects：机器人账号权限是分项的，
// 只给系统权限时凭证有效但读不到任何项目——那种情况这里要明确提示出来。
async function test(row) {
  busy['t' + row.id] = true
  try {
    const r = await testHarborRegistry(row.id)
    if (!r.ok) ElMessage.error(r.error || '连接失败')
    else if (r.warn) ElMessage.warning(r.warn)
    else ElMessage.success(`连接正常：Harbor ${r.harbor_version || ''}，项目可读`)
  } catch (e) { ElMessage.error('测试失败') } finally { busy['t' + row.id] = false }
}

async function showStatus(row) {
  busy['s' + row.id] = true
  st.value = null; projects.value = []
  try {
    const r = await harborStatus({ registry_id: row.id })
    if (!r.ok) { ElMessage.error(r.error || '取状态失败'); return }
    st.value = r
    const p = await harborProjects({ registry_id: row.id })
    if (p.ok) projects.value = p.projects || []
    statusDlg.value = true
  } catch (e) { ElMessage.error('取状态失败') } finally { busy['s' + row.id] = false }
}

load()
</script>

<style scoped>
.tip { background: #f4f4f5; border-left: 3px solid #909399; padding: 8px 12px; margin-bottom: 12px; font-size: 12px; line-height: 1.7; color: #606266; }
.tip .warn { color: #e6a23c; font-weight: 600; }
.tip code { background: #e9e9eb; padding: 1px 5px; border-radius: 3px; }
.muted { color: #909399; font-size: 12px; }
.sub { font-size: 13px; font-weight: 600; margin-bottom: 6px; color: #303133; }
</style>
