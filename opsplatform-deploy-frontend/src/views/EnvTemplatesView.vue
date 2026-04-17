<template>
  <div>
    <div class="kpi-bar">
      <div class="kpi kpi-blue">
        <div class="kpi-icon"><Variable :size="18" /></div>
        <div class="kpi-body">
          <div class="kpi-label">模板总数</div>
          <div class="kpi-value">{{ list.length }}</div>
          <div class="kpi-foot">可复用的环境变量集</div>
        </div>
      </div>
      <div class="kpi kpi-green">
        <div class="kpi-icon"><Hash :size="18" /></div>
        <div class="kpi-body">
          <div class="kpi-label">变量总数</div>
          <div class="kpi-value">{{ totalVars }}</div>
          <div class="kpi-foot">跨所有模板</div>
        </div>
      </div>
      <div class="kpi kpi-gray" style="background: white; border: 1px solid #e2e8f0">
        <div class="kpi-icon" style="background: #f1f5f9; color: #64748b"><Info :size="18" /></div>
        <div class="kpi-body">
          <div class="kpi-label">用途</div>
          <div class="kpi-value" style="font-size: 14px; font-weight: 500">新建模块时复用</div>
          <div class="kpi-foot">避免重复填写环境变量</div>
        </div>
      </div>
    </div>

    <div class="toolbar">
      <div class="toolbar-left">
        <span class="toolbar-title">环境变量模板</span>
      </div>
      <div class="toolbar-right">
        <button class="btn btn-primary" @click="openCreate"><Plus :size="13" /> 新增模板</button>
      </div>
    </div>

    <div class="card" style="padding: 0; overflow: hidden">
      <GhostEmpty v-if="!list.length"
        :headers="[{label:'ID',width:'60px'},{label:'模板名',width:'200px'},{label:'变量数',width:'100px'},{label:'描述'},{label:'更新时间',width:'140px'},{label:'操作',width:'180px'}]"
        :icon="Variable"
        title="暂无环境变量模板"
        description="创建可复用的环境变量集, 新建模块时直接引用"
        cta-label="新增模板"
        @cta="openCreate" />
      <table v-else class="table">
        <thead>
          <tr>
            <th style="width: 60px">ID</th>
            <th style="width: 200px">模板名</th>
            <th style="width: 100px; text-align: center">变量数</th>
            <th>描述</th>
            <th style="width: 140px">更新时间</th>
            <th style="width: 180px; text-align: right">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="t in list" :key="t.id">
            <td class="mono text-muted text-xs">#{{ t.id }}</td>
            <td><strong class="mono">{{ t.name }}</strong></td>
            <td class="text-center"><span class="chip chip-gray">{{ varCount(t.env_vars) }}</span></td>
            <td class="text-sm">{{ t.description || '—' }}</td>
            <td class="text-xs text-gray mono">{{ formatTime(t.updated_at) }}</td>
            <td style="text-align: right">
              <div class="actions" style="justify-content: flex-end">
                <button class="btn btn-sm btn-outline" @click="openEdit(t)">编辑</button>
                <button class="btn btn-sm btn-danger-light" @click="onDelete(t)">删除</button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <div v-if="dialogOpen" class="dialog-mask" @click.self="dialogOpen=false">
      <div class="dialog">
        <div class="dialog-title">{{ form.id ? '编辑模板' : '新增环境变量模板' }}</div>
        <div class="dialog-content">
          <div class="form-group">
            <label class="form-label">模板名 <span class="text-danger">*</span></label>
            <input v-model="form.name" class="form-input" :disabled="!!form.id" placeholder="如 uat-common" />
          </div>
          <div class="form-group">
            <label class="form-label">环境变量 (JSON 数组)</label>
            <textarea v-model="form.env_vars" class="form-textarea" rows="10" placeholder='[{"key":"LOG_LEVEL","value":"info"}]'></textarea>
          </div>
          <div class="form-group">
            <label class="form-label">描述</label>
            <input v-model="form.description" class="form-input" />
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
import { Plus, Variable, Hash, Info } from 'lucide-vue-next'
import { envTemplatesApi } from '../api'
import { success, error, confirm } from '../stores/ui'
import GhostEmpty from '../components/GhostEmpty.vue'

const list = ref([])
const dialogOpen = ref(false)
const form = ref({ id: null, name: '', env_vars: '[]', description: '' })

const totalVars = computed(() => list.value.reduce((sum, t) => sum + varCount(t.env_vars), 0))

function varCount(s) { try { return JSON.parse(s || '[]').length } catch { return 0 } }
function formatTime(t) { return t ? t.slice(0, 16).replace('T', ' ') : '-' }

async function load() {
  try { list.value = (await envTemplatesApi.list()).data || [] }
  catch (e) { error('加载失败: ' + e.message) }
}

function openCreate() { form.value = { id: null, name: '', env_vars: '[]', description: '' }; dialogOpen.value = true }
function openEdit(t) { form.value = { ...t }; dialogOpen.value = true }

async function onSave() {
  if (!form.value.name) return error('模板名必填')
  try { JSON.parse(form.value.env_vars || '[]') } catch { return error('env_vars 不是合法 JSON') }
  try {
    if (form.value.id) await envTemplatesApi.update(form.value.id, form.value)
    else await envTemplatesApi.create(form.value)
    success('保存成功'); dialogOpen.value = false; load()
  } catch (e) { error('保存失败: ' + (e.response?.data?.message || e.message)) }
}

async function onDelete(t) {
  if (!await confirm({ title: '删除模板', message: `删除 "${t.name}"?`, danger: true })) return
  try { await envTemplatesApi.delete(t.id); success('已删除'); load() }
  catch (e) { error(e.message) }
}

onMounted(load)
</script>

<style scoped>
.text-center { text-align: center; }
</style>
