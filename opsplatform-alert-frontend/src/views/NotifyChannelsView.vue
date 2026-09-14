<template>
  <div>
    <div class="card">
      <div class="card-header">
        <div class="card-title">通知渠道</div>
        <button class="btn btn-primary" @click="showModal = true; resetForm()">
          <Plus :size="16" /> 新增渠道
        </button>
      </div>

      <div v-if="loading" class="loading"><div class="spinner"></div></div>

      <div v-else-if="list.length === 0" class="empty-state">
        <Send :size="48" style="opacity: 0.4;" />
        <p class="mt-4">暂无通知渠道，请先添加</p>
      </div>

      <div v-else class="table-wrapper">
        <table>
          <thead>
            <tr>
              <th>状态</th>
              <th>类型</th>
              <th>名称</th>
              <th>目标</th>
              <th>描述</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in list" :key="item.id">
              <td>
                <label class="switch">
                  <input type="checkbox" :checked="item.status === 1" @change="toggle(item)">
                  <span class="slider"></span>
                </label>
              </td>
              <td>
                <span class="badge" :class="item.channel_type === 'telegram' ? 'badge-info' : 'badge-gray'">
                  {{ item.channel_type === 'telegram' ? 'Telegram' : 'Lark' }}
                </span>
              </td>
              <td style="font-weight: 500;">{{ item.name }}</td>
              <td><span class="truncate" :title="targetOf(item)">{{ targetOf(item) }}</span></td>
              <td class="text-sm text-secondary">{{ item.description || '-' }}</td>
              <td>
                <div class="actions">
                  <button class="btn btn-sm btn-outline" @click="editItem(item)"><Pencil :size="14" /></button>
                  <button class="btn btn-sm btn-outline" style="color: var(--danger);" @click="deleteItem(item)"><Trash2 :size="14" /></button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <!-- Modal -->
    <Transition name="modal">
    <div v-if="showModal" class="modal-overlay" @click.self="showModal = false">
      <div class="modal">
        <div class="modal-header">
          <div class="modal-title">{{ editId ? '编辑' : '新增' }}通知渠道</div>
          <button class="btn-icon" @click="showModal = false"><X :size="18" /></button>
        </div>
        <form @submit.prevent="handleSubmit">
          <div class="form-group">
            <label class="form-label">渠道类型 *</label>
            <select v-model="form.channel_type" class="form-select" :disabled="!!editId">
              <option value="lark">Lark / 飞书</option>
              <option value="telegram">Telegram</option>
            </select>
            <div v-if="editId" class="form-hint">已保存的渠道不允许改类型，请新建</div>
          </div>

          <div class="form-group">
            <label class="form-label">名称 *</label>
            <input v-model="form.name" class="form-input" placeholder="如: G32 告警群" required />
          </div>

          <!-- Lark -->
          <template v-if="form.channel_type === 'lark'">
            <div class="form-group">
              <label class="form-label">Webhook URL *</label>
              <input v-model="form.webhook_url" class="form-input"
                placeholder="https://open.feishu.cn/open-apis/bot/v2/hook/xxx" required />
            </div>
            <div class="form-group">
              <label class="form-label">版本</label>
              <select v-model="form.lark_type" class="form-select">
                <option value="feishu">feishu（国内版）</option>
                <option value="larksuite">larksuite（国际版）</option>
              </select>
            </div>
            <div class="form-group">
              <label class="form-label">签名密钥 (可选)</label>
              <input v-model="form.secret" class="form-input"
                :placeholder="editId ? '留空表示不修改' : '启用签名验证时填写'" />
            </div>
          </template>

          <!-- Telegram -->
          <template v-else>
            <div class="form-group">
              <label class="form-label">Bot Token *</label>
              <input v-model="form.bot_token" class="form-input"
                :placeholder="editId ? '留空表示不修改' : '123456789:AAxxxxxxxxxxxxxxxxxxxxx'" :required="!editId" />
              <div class="form-hint">找 @BotFather 创建 bot 后获得；保存后只回显前半段</div>
            </div>
            <div class="form-group">
              <label class="form-label">Chat ID *</label>
              <input v-model="form.chat_id" class="form-input" placeholder="-1001234567890" required />
              <div class="form-hint">群组为负数。把 bot 拉进群后，访问 https://api.telegram.org/bot&lt;token&gt;/getUpdates 可查到</div>
            </div>
            <div class="form-group">
              <label class="form-label">话题 ID (可选)</label>
              <input v-model.number="form.thread_id" type="number" class="form-input" placeholder="0" />
              <div class="form-hint">仅论坛模式的超级群需要，普通群留 0</div>
            </div>
            <div class="form-group">
              <label class="form-label">代理 (可选)</label>
              <input v-model="form.proxy_url" class="form-input" placeholder="http://proxy.internal:8080" />
              <div class="form-hint">集群无法直连 api.telegram.org 时填写，留空直连</div>
            </div>
          </template>

          <div class="form-group">
            <label class="form-label">描述</label>
            <input v-model="form.description" class="form-input" />
          </div>

          <div class="modal-footer">
            <button type="button" class="btn btn-outline" @click="testChannel" :disabled="testing">
              {{ testing ? '测试中...' : '发送测试消息' }}
            </button>
            <button type="button" class="btn btn-outline" @click="showModal = false">取消</button>
            <button type="submit" class="btn btn-primary" :disabled="submitting">
              {{ submitting ? '保存中...' : '保存' }}
            </button>
          </div>
        </form>
      </div>
    </div>
    </Transition>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import api from '../api'
import { useToast, useConfirm } from '../stores/ui'
import { Plus, Send, Pencil, Trash2, X } from 'lucide-vue-next'

const toast = useToast()
const dialog = useConfirm()

const list = ref([])
const loading = ref(false)
const showModal = ref(false)
const editId = ref(null)
const submitting = ref(false)
const testing = ref(false)

function emptyForm() {
  return {
    channel_type: 'lark',
    name: '',
    webhook_url: '',
    secret: '',
    lark_type: 'larksuite',
    bot_token: '',
    chat_id: '',
    thread_id: 0,
    proxy_url: '',
    description: ''
  }
}

const form = ref(emptyForm())

function targetOf(item) {
  return item.channel_type === 'telegram'
    ? `群组 ${item.chat_id}${item.thread_id ? ' / 话题 ' + item.thread_id : ''}`
    : item.webhook_url
}

function resetForm() {
  editId.value = null
  form.value = emptyForm()
}

async function loadList() {
  loading.value = true
  try {
    const res = await api.get('/notify-channels')
    if (res.code === 0) list.value = res.data
  } catch (e) { /* ignore */ }
  loading.value = false
}

function editItem(item) {
  editId.value = item.id
  form.value = {
    channel_type: item.channel_type || 'lark',
    name: item.name,
    webhook_url: item.webhook_url,
    secret: '',            // never echoed back; empty means "keep stored"
    lark_type: item.lark_type || 'larksuite',
    bot_token: '',         // ditto
    chat_id: item.chat_id,
    thread_id: item.thread_id || 0,
    proxy_url: item.proxy_url || '',
    description: item.description
  }
  showModal.value = true
}

async function handleSubmit() {
  submitting.value = true
  try {
    const res = editId.value
      ? await api.put(`/notify-channels/${editId.value}`, form.value)
      : await api.post('/notify-channels', form.value)
    if (res.code === 0) {
      showModal.value = false
      loadList()
    } else {
      toast.error(res.message)
    }
  } catch (e) {
    toast.error('保存失败: ' + (e.response?.data?.message || e.message))
  }
  submitting.value = false
}

async function deleteItem(item) {
  const ok = await dialog.danger({ title: '删除渠道', message: `确认删除「${item.name}」？` })
  if (!ok) return
  try {
    const res = await api.delete(`/notify-channels/${item.id}`)
    if (res.code === 0) loadList()
    else toast.error(res.message)
  } catch (e) {
    toast.error(e.response?.data?.message || '删除失败')
  }
}

async function toggle(item) {
  await api.put(`/notify-channels/${item.id}/toggle`)
  item.status = item.status === 1 ? 0 : 1
}

async function testChannel() {
  testing.value = true
  try {
    // The test endpoint's ?id= param loads the STORED channel as the base and only
    // overlays non-credential fields from the body (this closed a hole where a caller
    // could pair someone else's id with their own body to read their token out of an
    // error message). That means if we always pass ?id= while editing, a credential
    // the user just typed would be silently ignored in favor of the old stored one.
    // So: pass ?id= only when the relevant credential field is empty or still masked
    // (i.e. "use what's stored"); if the user typed a fresh credential, omit ?id= so
    // their typed value is what actually gets tested.
    const credentialField = form.value.channel_type === 'telegram' ? 'bot_token' : 'secret'
    const credential = form.value[credentialField]
    const hasFreshCredential = !!credential && !credential.endsWith('****')
    const useStoredCredential = !!editId.value && !hasFreshCredential
    const url = useStoredCredential ? `/notify-channels/test?id=${editId.value}` : '/notify-channels/test'
    const res = await api.post(url, form.value)
    if (res.code === 0) toast.success('测试消息发送成功!')
    else toast.error('发送失败: ' + res.message)
  } catch (e) {
    toast.error('发送失败: ' + (e.response?.data?.message || e.message))
  }
  testing.value = false
}

onMounted(loadList)
</script>
