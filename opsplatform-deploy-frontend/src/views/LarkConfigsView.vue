<template>
  <div>
    <div class="kpi-bar">
      <div class="kpi kpi-blue">
        <div class="kpi-icon"><Send :size="18" /></div>
        <div class="kpi-body">
          <div class="kpi-label">Lark 群</div>
          <div class="kpi-value">{{ list.length }}</div>
          <div class="kpi-foot">{{ enabledCount }} 启用</div>
        </div>
      </div>
      <div class="kpi kpi-cyan">
        <div class="kpi-icon"><Globe :size="18" /></div>
        <div class="kpi-body">
          <div class="kpi-label">国内版 (Feishu)</div>
          <div class="kpi-value">{{ feishuCount }}</div>
          <div class="kpi-foot">飞书 Webhook</div>
        </div>
      </div>
      <div class="kpi kpi-purple">
        <div class="kpi-icon"><Globe :size="18" /></div>
        <div class="kpi-body">
          <div class="kpi-label">国际版 (Lark)</div>
          <div class="kpi-value">{{ larkCount }}</div>
          <div class="kpi-foot">Lark Webhook</div>
        </div>
      </div>
    </div>

    <div class="toolbar">
      <div class="toolbar-left">
        <span class="toolbar-title">Lark 群配置</span>
      </div>
      <div class="toolbar-right">
        <button class="btn btn-primary" @click="openCreate"><Plus :size="13" /> 新增群</button>
      </div>
    </div>

    <div class="card" style="padding: 0; overflow: hidden">
      <GhostEmpty v-if="!list.length"
        :headers="[{label:'ID',width:'60px'},{label:'名称',width:'200px'},{label:'类型',width:'110px'},{label:'Webhook URL'},{label:'签名',width:'100px'},{label:'状态',width:'100px'},{label:'操作',width:'220px'}]"
        :icon="Send"
        title="暂无 Lark 群"
        description="配置 Lark 机器人 Webhook, 发布时推送通知"
        cta-label="新增群"
        @cta="openCreate" />
      <table v-else class="table">
        <thead>
          <tr>
            <th style="width: 60px">ID</th>
            <th style="width: 200px">名称</th>
            <th style="width: 110px">类型</th>
            <th>Webhook URL</th>
            <th style="width: 100px">签名</th>
            <th style="width: 100px">状态</th>
            <th style="width: 220px; text-align: right">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="c in list" :key="c.id">
            <td class="mono text-muted text-xs">#{{ c.id }}</td>
            <td>
              <div class="lark-cell">
                <div class="lark-icon"><Send :size="12" /></div>
                <strong>{{ c.name }}</strong>
              </div>
            </td>
            <td>
              <span class="chip" :class="c.lark_type === 'feishu' ? 'chip-cyan' : 'chip'">
                {{ c.lark_type === 'feishu' ? '飞书' : 'Lark' }}
              </span>
            </td>
            <td>
              <code class="mono text-xs" style="color: #475569; word-break: break-all">
                {{ c.webhook_url.length > 48 ? c.webhook_url.slice(0, 48) + '...' : c.webhook_url }}
              </code>
            </td>
            <td>
              <span class="chip" :class="c.secret ? 'chip-green' : 'chip-gray'">
                {{ c.secret ? '已启用' : '无签名' }}
              </span>
            </td>
            <td>
              <span class="chip" :class="c.status ? 'chip-green' : 'chip-gray'">
                <span class="dot" :class="c.status ? 'dot-success' : 'dot-gray'"></span>
                {{ c.status ? '启用' : '禁用' }}
              </span>
            </td>
            <td style="text-align: right">
              <div class="actions" style="justify-content: flex-end">
                <button class="btn btn-sm btn-outline" @click="onTest(c)">测试</button>
                <button class="btn btn-sm btn-outline" @click="openEdit(c)">编辑</button>
                <button class="btn btn-sm btn-danger-light" @click="onDelete(c)">删除</button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <div v-if="dialogOpen" class="dialog-mask" @click.self="dialogOpen=false">
      <div class="dialog">
        <div class="dialog-title">{{ form.id ? '编辑 Lark 群' : '新增 Lark 群' }}</div>
        <div class="dialog-content">
          <div class="form-group">
            <label class="form-label">名称 <span class="text-danger">*</span></label>
            <input v-model="form.name" class="form-input" placeholder="如 发布通知群" />
          </div>
          <div class="form-group">
            <label class="form-label">Webhook URL <span class="text-danger">*</span></label>
            <input v-model="form.webhook_url" class="form-input" placeholder="https://open.larksuite.com/open-apis/bot/v2/hook/xxx" />
          </div>
          <div class="form-group">
            <label class="form-label">签名密钥</label>
            <input v-model="form.secret" class="form-input" placeholder="启用签名时填写" />
          </div>
          <div class="form-group">
            <label class="form-label">类型</label>
            <select v-model="form.lark_type" class="form-select">
              <option value="feishu">飞书 (国内)</option>
              <option value="larksuite">Lark (国际)</option>
            </select>
          </div>
          <div class="form-group">
            <label class="form-label">描述</label>
            <input v-model="form.description" class="form-input" />
          </div>
          <div v-if="form.id" class="form-group">
            <label class="form-label flex items-center gap-2">
              <input type="checkbox" v-model="form.status" :true-value="1" :false-value="0" />
              <span>启用</span>
            </label>
          </div>
        </div>
        <div class="dialog-actions">
          <button class="btn btn-outline" @click="dialogOpen=false">取消</button>
          <button class="btn btn-primary" @click="onSave">保存</button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted, computed } from 'vue'
import { Plus, Send, Globe } from 'lucide-vue-next'
import { larkConfigsApi } from '../api'
import { success, error, info, confirm } from '../stores/ui'
import GhostEmpty from '../components/GhostEmpty.vue'

const list = ref([])
const dialogOpen = ref(false)
const form = ref({ id: null, name: '', webhook_url: '', secret: '', lark_type: 'feishu', description: '', status: 1 })

const enabledCount = computed(() => list.value.filter(c => c.status).length)
const feishuCount = computed(() => list.value.filter(c => c.lark_type === 'feishu').length)
const larkCount = computed(() => list.value.filter(c => c.lark_type === 'larksuite').length)

async function load() {
  try { list.value = (await larkConfigsApi.list()).data || [] }
  catch (e) { error('加载失败: ' + e.message) }
}

function openCreate() { form.value = { id: null, name: '', webhook_url: '', secret: '', lark_type: 'feishu', description: '', status: 1 }; dialogOpen.value = true }
function openEdit(c) { form.value = { ...c }; dialogOpen.value = true }

async function onSave() {
  if (!form.value.name || !form.value.webhook_url) return error('名称和 webhook 必填')
  try {
    if (form.value.id) await larkConfigsApi.update(form.value.id, form.value)
    else await larkConfigsApi.create(form.value)
    success('保存成功'); dialogOpen.value = false; load()
  } catch (e) { error('保存失败: ' + (e.response?.data?.message || e.message)) }
}

async function onDelete(c) {
  if (!await confirm({ title: '删除', message: `删除 "${c.name}"?`, danger: true })) return
  try { await larkConfigsApi.delete(c.id); success('已删除'); load() }
  catch (e) { error(e.message) }
}

async function onTest(c) {
  try { const r = await larkConfigsApi.test(c.id); info(r.data?.msg || '测试已发送') }
  catch (e) { error('测试失败: ' + e.message) }
}

onMounted(load)
</script>

<style scoped>
.lark-cell { display: flex; align-items: center; gap: 8px; }
.lark-icon {
  width: 24px; height: 24px; border-radius: 5px;
  background: #dbeafe; color: #1e40af;
  display: flex; align-items: center; justify-content: center;
  flex-shrink: 0;
}
</style>
