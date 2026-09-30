<template>
  <div>
    <div class="card">
      <div class="card-header">
        <div>
          <div class="card-title">外部字典源</div>
          <div class="form-hint" style="margin-top: 4px;">
            把日志里的 room_id / site_id 翻译成房间号和站点名。名单来自运维平台，这里只读取、不维护副本。
          </div>
        </div>
        <button class="btn btn-primary" @click="showModal = true; resetForm()">
          <Plus :size="16" /> 新增字典源
        </button>
      </div>

      <div v-if="loading" class="loading"><div class="spinner"></div></div>

      <div v-else-if="list.length === 0" class="empty-state">
        <Database :size="48" style="opacity: 0.4;" />
        <p class="mt-4">暂无字典源。心跳告警需要它把 id 翻成名字，没有也能告警，只是消息里显示原始 id。</p>
      </div>

      <div v-else class="table-wrapper">
        <table>
          <thead>
            <tr>
              <th>状态</th>
              <th>名称</th>
              <th>地址</th>
              <th>环境</th>
              <th>API Key</th>
              <th>最近同步</th>
              <th>名单</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in list" :key="item.id">
              <td>
                <span class="badge" :class="item.status === 1 ? 'badge-success' : 'badge-secondary'">
                  {{ item.status === 1 ? '启用' : '停用' }}
                </span>
              </td>
              <td style="font-weight: 500;">{{ item.name }}</td>
              <td><span class="truncate mono" :title="item.base_url">{{ item.base_url }}</span></td>
              <td><span class="mono">{{ item.env || '-' }}</span></td>
              <td>
                <span v-if="item.has_api_key" class="badge badge-success">已配置</span>
                <span v-else class="badge badge-warning">未配置</span>
              </td>
              <td>
                <!-- 同步状态是这个页面最该一眼看到的东西：字典失败不会让告警停，
                     所以除了这里，没有别的地方会告诉你它已经坏了一整天。 -->
                <template v-if="item.last_sync_ok">
                  <span class="badge badge-success">正常</span>
                  <div class="text-sm text-secondary">{{ formatTime(item.last_sync_at) }}</div>
                </template>
                <template v-else>
                  <span class="badge badge-danger">失败</span>
                  <div class="text-sm" style="color: var(--danger);" :title="item.last_sync_error">
                    {{ truncate(item.last_sync_error, 40) || '尚未同步' }}
                  </div>
                </template>
              </td>
              <td class="text-sm">
                <template v-if="item.room_count || item.site_count">
                  {{ item.room_count }} 房间 / {{ item.site_count }} 站点
                  <div class="text-secondary mono" :title="item.last_version">{{ truncate(item.last_version, 10) }}</div>
                </template>
                <span v-else class="text-secondary">—</span>
              </td>
              <td>
                <div class="actions">
                  <button class="btn btn-sm btn-outline" @click="editItem(item)"><Pencil :size="14" /></button>
                  <button class="btn btn-sm btn-danger" @click="deleteItem(item)"><Trash2 :size="14" /></button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <Transition name="modal">
    <div v-if="showModal" class="modal-overlay">
      <div class="modal" style="min-width: 560px;">
        <div class="modal-header">
          <div class="modal-title">{{ editId ? '编辑字典源' : '新增字典源' }}</div>
          <button class="btn-icon" @click="showModal = false"><X :size="18" /></button>
        </div>
        <form @submit.prevent="handleSubmit">
          <div style="padding: 16px 24px;">
            <div class="form-group">
              <label class="form-label">名称 *</label>
              <input v-model="form.name" class="form-input" required placeholder="如：运维平台桌台字典" />
            </div>

            <div class="form-group">
              <label class="form-label">服务地址 *</label>
              <input v-model="form.base_url" class="form-input" required
                     placeholder="http://opsplatform-backend:8080" />
              <div class="form-hint">
                和运维平台同集群同命名空间时直接写 service 名，不用出集群：<code>http://opsplatform-backend:8080</code>
              </div>
            </div>

            <div class="form-group">
              <label class="form-label">环境 *</label>
              <input v-model="form.env" class="form-input" required placeholder="PROD" />
              <div class="form-hint">运维平台那边的环境名，填错会返回「环境不存在」</div>
            </div>

            <div class="form-group">
              <label class="form-label">API Key</label>
              <input v-model="form.api_key" type="password" class="form-input"
                     :placeholder="editId && hasKey ? '已配置，留空表示不修改' : 'opsk_...'" />
              <div class="form-hint">
                在运维平台「API Key 管理」新建，业务域选<b>桌台字典·只读 (table_alert)</b>。
                保存后不再回显，留空即保持原值。
              </div>
            </div>

            <div class="form-group">
              <label class="form-label">刷新间隔（秒）</label>
              <input v-model.number="form.refresh_sec" type="number" class="form-input" min="60" />
              <div class="form-hint">
                两次探测之间至少隔这么久。探的是几十字节的版本号，版本没变就不会拉全量名单。
              </div>
            </div>

            <div class="form-group">
              <label class="form-label">描述</label>
              <input v-model="form.description" class="form-input" />
            </div>

            <!-- 测试结果直接把名单规模摆出来：地址和 Key 都对、但环境名写错时
                 接口会正常返回一个空名单，只说「成功」会让人以为配好了。 -->
            <div v-if="testResult" class="card" style="padding: 10px 12px; background: #eff6ff; border-color: #bfdbfe;">
              <div class="text-sm" style="color: #1e40af;">
                环境 <b>{{ testResult.env || '-' }}</b> ·
                房间 <b>{{ testResult.room_count }}</b>（在用 {{ testResult.in_service }}）·
                站点 <b>{{ testResult.site_count }}</b>（关注 {{ testResult.watched }}）
                <div v-if="!testResult.collect_ok" style="color: var(--danger); margin-top: 4px;">
                  ⚠ 对方自报采集失败，名单可能不是最新的
                </div>
                <div v-else-if="testResult.room_count === 0" style="color: var(--danger); margin-top: 4px;">
                  ⚠ 名单是空的 —— 多半是环境名写错了
                </div>
              </div>
            </div>
          </div>
          <div class="modal-footer">
            <button type="button" class="btn btn-outline" @click="testConnection" :disabled="testing">
              {{ testing ? '测试中...' : '测试连接' }}
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
import { formatTime } from '../utils/datetime'
import { Plus, Database, Pencil, Trash2, X } from 'lucide-vue-next'

const toast = useToast()
const dialog = useConfirm()
const list = ref([])
const loading = ref(false)
const showModal = ref(false)
const editId = ref(null)
const hasKey = ref(false)
const submitting = ref(false)
const testing = ref(false)
const testResult = ref(null)

const form = ref({ name: '', base_url: '', api_key: '', env: 'PROD', refresh_sec: 600, description: '' })

function truncate(s, n) {
  if (!s) return ''
  return s.length > n ? s.slice(0, n) + '…' : s
}

function resetForm() {
  editId.value = null
  hasKey.value = false
  testResult.value = null
  form.value = { name: '', base_url: '', api_key: '', env: 'PROD', refresh_sec: 600, description: '' }
}

async function loadList() {
  loading.value = true
  try {
    const res = await api.get('/dict-sources')
    if (res.code === 0) list.value = res.data || []
  } catch (e) { /* ignore */ }
  loading.value = false
}

function editItem(item) {
  editId.value = item.id
  hasKey.value = item.has_api_key
  testResult.value = null
  // api_key 留空：后端从不回传它，空值表示"保持原样"
  form.value = {
    name: item.name, base_url: item.base_url, api_key: '',
    env: item.env, refresh_sec: item.refresh_sec, description: item.description,
  }
  showModal.value = true
}

async function handleSubmit() {
  submitting.value = true
  try {
    const res = editId.value
      ? await api.put(`/dict-sources/${editId.value}`, form.value)
      : await api.post('/dict-sources', form.value)
    if (res.code === 0) { showModal.value = false; loadList() }
    else toast.error(res.message)
  } catch (e) { toast.error(e.response?.data?.message || '操作失败') }
  submitting.value = false
}

async function deleteItem(item) {
  const ok = await dialog.danger({ title: '删除字典源', message: `确认删除「${item.name}」？用它的告警规则会失去名称翻译。` })
  if (!ok) return
  try {
    const res = await api.delete(`/dict-sources/${item.id}`)
    if (res.code === 0) loadList()
    else toast.error(res.message)
  } catch (e) { toast.error(e.response?.data?.message || '删除失败') }
}

async function testConnection() {
  testing.value = true
  testResult.value = null
  try {
    // 带上 id：编辑时没重填 Key 也要能测，后端会回退用已存的那把
    const path = editId.value ? `/dict-sources/${editId.value}/test` : '/dict-sources/test'
    const res = await api.post(path, form.value)
    if (res.code === 0) { testResult.value = res.data; toast.success('连接成功') }
    else toast.error(res.message)
  } catch (e) { toast.error(e.response?.data?.message || '连接失败') }
  testing.value = false
}

onMounted(loadList)
</script>

<style scoped>
.mono { font-family: ui-monospace, "SF Mono", Menlo, monospace; }
</style>
