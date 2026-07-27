<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { useAppStore } from '../stores/app'
import http from '../api/http'

const appStore = useAppStore()
const loading = ref(false)
const saving = ref(false)
const cur = ref({ has_key: false, auth: '', base_url: '', model: '', models: [] })

const form = ref({ credType: 'api_key', credValue: '', base_url: '', model: '' })

async function load() {
  loading.value = true
  try {
    const { data } = await http.get('/config')
    cur.value = data
    form.value.base_url = data.base_url || ''
    form.value.model = data.model || 'claude-opus-4-8'
    form.value.credType = data.auth === 'oauth' ? 'oauth' : 'api_key'
  } catch (e) {
    ElMessage.error('读取配置失败: ' + (e.response?.data || e.message))
  } finally {
    loading.value = false
  }
}

async function save() {
  saving.value = true
  try {
    const body = { base_url: form.value.base_url.trim(), model: form.value.model }
    const v = form.value.credValue.trim()
    if (v) {
      if (form.value.credType === 'oauth') body.auth_token = v
      else body.api_key = v
    }
    const { data } = await http.post('/config', body)
    ElMessage.success(data.has_key ? '已保存，凭证已生效' : '已保存（尚未配置凭证）')
    form.value.credValue = ''
    await load()
  } catch (e) {
    ElMessage.error('保存失败: ' + (e.response?.data || e.message))
  } finally {
    saving.value = false
  }
}

async function clearKey() {
  try {
    await appStore.showConfirm('确定清空当前凭证吗？清空后模型调用会失败。', '清空凭证')
  } catch { return }
  saving.value = true
  try {
    await http.post('/config', { clear_key: true, base_url: form.value.base_url.trim(), model: form.value.model })
    ElMessage.success('凭证已清空')
    form.value.credValue = ''
    await load()
  } catch (e) {
    ElMessage.error('操作失败: ' + (e.response?.data || e.message))
  } finally {
    saving.value = false
  }
}

// ===== MCP 接入管理 =====
const servers = ref([])
const srvLoading = ref(false)
const dlg = ref(false)
const dlgSaving = ref(false)
const isEdit = ref(false)
const srvForm = ref({ name: '', url: '', token: '', enabled: true, old_name: '' })

async function loadServers() {
  srvLoading.value = true
  try {
    const { data } = await http.get('/mcp-servers')
    servers.value = data.servers || []
  } catch (e) {
    ElMessage.error('读取 MCP 列表失败: ' + (e.response?.data || e.message))
  } finally {
    srvLoading.value = false
  }
}

function openAdd() {
  isEdit.value = false
  srvForm.value = { name: '', url: '', token: '', enabled: true, old_name: '' }
  dlg.value = true
}
function openEdit(row) {
  isEdit.value = true
  srvForm.value = { name: row.name, url: row.url, token: '', enabled: row.enabled, old_name: row.name }
  dlg.value = true
}

async function saveServer() {
  const f = srvForm.value
  if (!f.name.trim() || !f.url.trim()) {
    ElMessage.warning('名称和地址必填')
    return
  }
  dlgSaving.value = true
  try {
    await http.post('/mcp-servers', {
      name: f.name.trim(),
      url: f.url.trim(),
      token: f.token.trim(),
      enabled: f.enabled,
      old_name: f.old_name,
    })
    ElMessage.success('已保存')
    dlg.value = false
    await loadServers()
  } catch (e) {
    ElMessage.error('保存失败: ' + (e.response?.data || e.message))
  } finally {
    dlgSaving.value = false
  }
}

async function toggleEnabled(row) {
  try {
    await http.post('/mcp-servers', { name: row.name, url: row.url, enabled: row.enabled, old_name: row.name })
    await loadServers()
  } catch (e) {
    ElMessage.error('操作失败: ' + (e.response?.data || e.message))
    loadServers()
  }
}

async function delServer(row) {
  try {
    await appStore.showConfirm(`确定删除 MCP 接入「${row.name}」吗？`, '删除接入')
  } catch { return }
  try {
    await http.post('/mcp-servers/delete', { name: row.name })
    ElMessage.success('已删除')
    await loadServers()
  } catch (e) {
    ElMessage.error('删除失败: ' + (e.response?.data || e.message))
  }
}

onMounted(() => { load(); loadServers() })
</script>

<template>
  <div class="top"><span class="title">设置</span></div>

  <div class="page" v-loading="loading">
    <div class="card">
      <div class="cardhd">模型接入</div>

      <div class="statusbar">
        <span class="k">当前凭证</span>
        <el-tag v-if="!cur.has_key" type="danger" size="small">未配置</el-tag>
        <el-tag v-else type="success" size="small">
          已配置 · {{ cur.auth === 'oauth' ? '订阅 OAuth Token' : 'API Key' }}
        </el-tag>
        <span class="k" style="margin-left:20px">当前模型</span>
        <el-tag size="small" effect="plain">{{ cur.model }}</el-tag>
      </div>

      <el-form label-width="120px" label-position="left" style="margin-top:16px;max-width:640px">
        <el-form-item label="凭证类型">
          <el-radio-group v-model="form.credType">
            <el-radio value="api_key">API Key</el-radio>
            <el-radio value="oauth">订阅 OAuth Token</el-radio>
          </el-radio-group>
        </el-form-item>

        <el-form-item :label="form.credType === 'oauth' ? 'OAuth Token' : 'API Key'">
          <el-input
            v-model="form.credValue"
            type="password"
            show-password
            clearable
            :placeholder="form.credType === 'oauth'
              ? 'sk-ant-oat...（claude setup-token 生成）'
              : 'sk-ant-api...（console.anthropic.com 生成）'"
          />
          <div class="hint">
            留空不改动现有凭证。
            <template v-if="form.credType === 'api_key'">
              去 console.anthropic.com 充值后生成，生产环境用它。
            </template>
            <template v-else>
              用你的 Claude 订阅经 <code>claude setup-token</code> 生成（官方命令，~1年有效）；服务端按 Claude Code 身份校验，属灰色用法，生产建议用 API Key。
            </template>
          </div>
        </el-form-item>

        <el-form-item label="Base URL">
          <el-input v-model="form.base_url" clearable placeholder="留空=官方 api.anthropic.com；走代理/网关时填" />
        </el-form-item>

        <el-form-item label="模型">
          <el-select v-model="form.model" filterable allow-create default-first-option style="width:100%">
            <el-option v-for="m in cur.models" :key="m" :label="m" :value="m" />
          </el-select>
        </el-form-item>

        <el-form-item>
          <el-button type="primary" :loading="saving" @click="save">保存并生效</el-button>
          <el-button v-if="cur.has_key" :loading="saving" @click="clearKey">清空凭证</el-button>
        </el-form-item>
      </el-form>

      <div class="note">
        ⚠ 调试版：设置为<b>内存生效</b>，后端 Pod 重启会回落到 k8s Secret 里的默认值（做持久化那期再存库）。
      </div>
    </div>

    <div class="card" v-loading="srvLoading">
      <div class="cardhd">
        MCP 接入
        <el-button type="primary" size="small" style="float:right" @click="openAdd">+ 新增接入</el-button>
      </div>
      <el-table :data="servers" size="small" style="width:100%">
        <el-table-column label="名称" width="140">
          <template #default="{ row }">
            <span class="dot" :class="row.ok ? 'up' : 'off'"></span>
            <b>{{ row.name }}</b>
          </template>
        </el-table-column>
        <el-table-column prop="url" label="地址" min-width="240" show-overflow-tooltip />
        <el-table-column label="状态" width="130">
          <template #default="{ row }">
            <el-tag v-if="!row.enabled" size="small" type="info">已停用</el-tag>
            <el-tag v-else-if="row.ok" size="small" type="success">{{ row.tools }} 工具</el-tag>
            <el-tooltip v-else :content="row.error || '未连'" placement="top">
              <el-tag size="small" type="danger">未连</el-tag>
            </el-tooltip>
          </template>
        </el-table-column>
        <el-table-column label="启用" width="70">
          <template #default="{ row }">
            <el-switch v-model="row.enabled" size="small" @change="toggleEnabled(row)" />
          </template>
        </el-table-column>
        <el-table-column label="操作" width="120">
          <template #default="{ row }">
            <el-button link type="primary" size="small" @click="openEdit(row)">编辑</el-button>
            <el-button link type="danger" size="small" @click="delServer(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
      <div class="note">
        AI 会自动聚合所有<b>启用</b>接入的工具；工具名冲突时以先注册的为准。后续可接入 发布/告警/k8sinsight 等系统的 MCP。
      </div>
    </div>
  </div>

  <el-dialog
    v-model="dlg"
    :title="isEdit ? '编辑 MCP 接入' : '新增 MCP 接入'"
    width="520px"
    :close-on-click-modal="false"
  >
    <el-form label-width="80px" label-position="left">
      <el-form-item label="名称">
        <el-input v-model="srvForm.name" placeholder="如 CMDB / 发布中心" />
      </el-form-item>
      <el-form-item label="地址">
        <el-input v-model="srvForm.url" placeholder="http://xxx-backend:8080/api/mcp" />
      </el-form-item>
      <el-form-item label="Token">
        <el-input v-model="srvForm.token" type="password" show-password clearable
          :placeholder="isEdit ? '留空=不改动现有 token' : 'MCP 访问 token（可空）'" />
      </el-form-item>
      <el-form-item label="启用">
        <el-switch v-model="srvForm.enabled" />
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="dlg = false">关闭</el-button>
      <el-button type="primary" :loading="dlgSaving" @click="saveServer">保存</el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.top { display: flex; align-items: center; padding: 12px 20px; border-bottom: 1px solid var(--line); background: var(--panel); }
.top .title { font-weight: 600; }
.page { flex: 1; overflow: auto; padding: 24px 20px; }
.card { max-width: 720px; margin: 0 auto 18px; background: #fff; border: 1px solid var(--line); border-radius: 14px; padding: 20px 22px; }
.dot { display: inline-block; width: 7px; height: 7px; border-radius: 50%; margin-right: 6px; vertical-align: middle; }
.dot.up { background: var(--ok); }
.dot.off { background: var(--bad); }
.cardhd { font-weight: 600; font-size: 15px; margin-bottom: 16px; }
.statusbar { display: flex; align-items: center; gap: 8px; font-size: 13px; }
.k { color: var(--ink3); font-size: 12px; }
.hint { font-size: 11.5px; color: var(--ink3); margin-top: 4px; line-height: 1.6; }
.hint code, .note code { font-family: var(--mono); background: #f0ebe2; padding: 1px 5px; border-radius: 4px; }
.note { margin-top: 18px; font-size: 12px; color: var(--ink2); background: var(--gold-soft); border: 1px solid #ecd9a8; border-radius: 9px; padding: 10px 12px; line-height: 1.6; }
</style>
