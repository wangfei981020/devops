<template>
  <div>
    <div class="tip">
      Cloudflare <b>只读</b>接入：采 Zone / DNS 解析 / SSL 配置，用于全链路排查与 DNS 一致性校验。
      <br>
      <span class="warn">与「域名注册商」里的 cloudflare 不是一回事</span> —— 那份用于证书 DNS-01 验证、需要<b>写</b> TXT 记录的权限；
      这里只需只读 Token（Zone·Read + DNS·Read + Zone Settings·Read，作用域选「所有域名」）。
    </div>

    <div style="margin-bottom:10px">
      <el-button type="primary" size="small" @click="open()">+ 添加 CDN 账号</el-button>
      <el-button size="small" :icon="Refresh" :loading="loading" @click="load">刷新</el-button>
    </div>

    <el-table :data="rows" size="small" v-loading="loading">
      <el-table-column prop="name" label="账号名" min-width="150" />
      <el-table-column prop="cdn" label="厂商" width="120"><template #default="{ row }">
        <el-tag size="small">{{ row.cdn || '—' }}</el-tag>
      </template></el-table-column>
      <el-table-column label="Token" width="90"><template #default="{ row }">
        <el-tag size="small" :type="row.has_credential ? 'success' : 'danger'">{{ row.has_credential ? '已配' : '未配' }}</el-tag>
      </template></el-table-column>
      <el-table-column label="启用" width="80"><template #default="{ row }">
        <el-tag size="small" :type="row.enabled ? 'success' : 'info'">{{ row.enabled ? '是' : '否' }}</el-tag>
      </template></el-table-column>
      <el-table-column prop="last_sync_at" label="最近同步" width="160"><template #default="{ row }">
        {{ row.last_sync_at || '从未同步' }}
      </template></el-table-column>
      <el-table-column label="同步结果" min-width="200"><template #default="{ row }">
        <span v-if="!row.last_result" class="muted">—</span>
        <el-tag v-else-if="row.last_result === '成功'" size="small" type="success">成功</el-tag>
        <el-tooltip v-else :content="row.last_result"><el-tag size="small" type="danger">失败</el-tag></el-tooltip>
      </template></el-table-column>
      <el-table-column label="操作" width="220" fixed="right"><template #default="{ row }">
        <el-button link type="warning" size="small" :loading="busy['v' + row.id]" @click="verify(row)">验证</el-button>
        <el-button link type="success" size="small" :loading="busy['s' + row.id]" @click="sync(row)">同步</el-button>
        <el-button link type="primary" size="small" @click="open(row)">编辑</el-button>
        <el-button link type="danger" size="small" @click="del(row)">删除</el-button>
      </template></el-table-column>
    </el-table>
    <el-empty v-if="!loading && !rows.length" description="还没配 CDN 账号，点上面「添加」并填只读 Token" :image-size="60" />

    <el-dialog :close-on-click-modal="false" :close-on-press-escape="false" v-model="dlg"
      :title="editing ? '编辑 CDN 账号' : '添加 CDN 账号'" width="560px">
      <el-form :model="form" label-width="110px">
        <el-form-item label="厂商">
          <el-select v-model="form.cdn_id" style="width:260px">
            <el-option v-for="c in cdns" :key="c.id"
              :label="isSupported(c.name) ? c.name : `${c.name}（暂未接入）`"
              :value="c.id" :disabled="!isSupported(c.name)" />
          </el-select>
        </el-form-item>
        <el-form-item label="账号名"><el-input v-model="form.name" placeholder="如 cloudflare-main" /></el-form-item>
        <el-form-item label="API Token">
          <el-input v-model="form.token" type="password" show-password
            :placeholder="editing ? '留空 = 保留原 Token 不变' : '粘贴 Cloudflare 只读 API Token'" />
          <div class="muted">出于安全，已保存的 Token 不会回显。</div>
        </el-form-item>
        <el-form-item label="Account ID"><el-input v-model="form.account_tag" placeholder="可选" /></el-form-item>
        <el-form-item label="启用"><el-switch v-model="form.enabled" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dlg = false">取消</el-button>
        <el-button type="primary" @click="save">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Refresh } from '@element-plus/icons-vue'
import { listCdnAccounts, saveCdnAccount, deleteCdnAccount, verifyCdnAccount, syncCdnAccount, listCdns } from '../../api/cmdb'
import { useAppStore } from '../../stores/app'

const app = useAppStore()
const rows = ref([]); const cdns = ref([]); const loading = ref(false); const busy = reactive({})
const dlg = ref(false); const editing = ref(false)
const form = reactive({ id: 0, cdn_id: null, name: '', token: '', account_tag: '', enabled: true })

async function load() {
  loading.value = true
  try {
    rows.value = await listCdnAccounts()
    if (!cdns.value.length) cdns.value = await listCdns()
  } catch (e) { ElMessage.error('加载失败') } finally { loading.value = false }
}

// 后端 cdnsource 目前只实现了 Cloudflare 适配。其余厂商在下拉里禁选并标注，
// 否则用户选了阿里云、后端却拿这份 token 去调 Cloudflare API，报错完全指不到真正原因。
const supported = ['cloudflare']
function isSupported(name) { return supported.includes(String(name || '').toLowerCase()) }

function open(row) {
  editing.value = !!row
  // 编辑时 token 一律留空：后端约定「留空=不修改」，避免把已存的 Token 误清空
  Object.assign(form, row
    ? { id: row.id, cdn_id: row.cdn_id, name: row.name, token: '', account_tag: row.account_tag, enabled: !!row.enabled }
    : { id: 0, cdn_id: cdns.value.find(c => isSupported(c.name))?.id ?? null, name: '', token: '', account_tag: '', enabled: true })
  dlg.value = true
}

async function save() {
  if (!form.cdn_id || !form.name) { ElMessage.warning('厂商和账号名必填'); return }
  if (!editing.value && !form.token) { ElMessage.warning('新增账号必须填 Token'); return }
  try {
    await saveCdnAccount({ ...form })
    ElMessage.success('已保存'); dlg.value = false; load()
  } catch (e) { ElMessage.error(e.response?.data?.error || '保存失败') }
}

async function del(row) {
  try {
    await app.showConfirm(`删除 CDN 账号「${row.name}」？该账号同步来的 Zone / DNS / 配置会一并清除。`)
    await deleteCdnAccount(row.id); ElMessage.success('已删除'); load()
  } catch (e) { if (e !== 'cancel') ElMessage.error('删除失败') }
}

// Cloudflare 的 verify 接口只验「Token 是否有效」，不验权限够不够——
// 权限缺失要到同步时才暴露，所以这里成功后仍提示去同步确认。
async function verify(row) {
  busy['v' + row.id] = true
  try {
    const r = await verifyCdnAccount(row.id)
    if (r.ok) ElMessage.success('Token 有效；权限是否齐全需点「同步」确认')
    else ElMessage.error(r.error || 'Token 无效')
  } catch (e) { ElMessage.error('验证失败') } finally { busy['v' + row.id] = false }
}

async function sync(row) {
  busy['s' + row.id] = true
  try {
    const r = await syncCdnAccount(row.id)
    if (r.ok) ElMessage.success(`同步完成：${r.zones} 个站点、${r.dns_records} 条解析记录`)
    else ElMessage.error(r.error || '同步失败')
    load()
  } catch (e) { ElMessage.error('同步失败') } finally { busy['s' + row.id] = false }
}

onMounted(load)
</script>

<style scoped>
.tip { background: #f4f4f5; border-left: 3px solid #909399; padding: 8px 12px; margin-bottom: 12px; font-size: 12px; line-height: 1.7; color: #606266; }
.warn { color: #e6a23c; font-weight: 600; }
.muted { color: #909399; font-size: 12px; }
</style>
