<template>
  <div class="rs-panel">
    <div class="p-hd">
      <h3>
        <el-icon><RefreshRight /></el-icon>
        批量重启服务
      </h3>
      <div class="mode-toggle">
        <button :class="['mt-btn', mode === 'select' && 'on']" @click="switchMode('select')">自动模式</button>
        <button :class="['mt-btn', mode === 'manual' && 'on']" @click="switchMode('manual')">手输模式</button>
      </div>
    </div>

    <!-- ============== 自动模式：多选模块（不需要 tag） ============== -->
    <div v-if="mode === 'select'" class="select-mode">
      <div class="sm-toolbar">
        <span class="sm-tip">从已扫描模块列表选择 · 不需要 tag · 重启只动 ArgoCD 不写 git</span>
        <div class="sm-cache">
          <button v-if="selectedModules.length" class="btn ghost sm danger-hover" @click="onClearAll">
            ✕ 清空已选 ({{ selectedModules.length }})
          </button>
        </div>
      </div>
      <div class="sm-row">
        <label>选模块（多选 · 可搜索）</label>
        <el-select v-model="selectedModules" multiple filterable collapse-tags collapse-tags-tooltip
          placeholder="点击下拉选择 / 输入关键字搜索" style="width:100%;">
          <el-option v-for="m in props.modules" :key="m.name" :label="m.name" :value="m.name" />
        </el-select>
      </div>
      <div class="sm-actions">
        <button class="btn ghost" @click="onPreviewSelect" :disabled="!selectedModules.length">
          预览
        </button>
        <span class="hint" v-if="!selectedModules.length">至少选一个模块</span>
      </div>
    </div>

    <!-- ============== 手输模式：保留 textarea ============== -->
    <div v-else class="ws-grid">
      <div class="ws-col in">
        <div class="ws-sub">输入模块名</div>
        <textarea
          v-model="text"
          class="ta"
          spellcheck="false"
          placeholder="atmosphere-frontend
base-client-backend
bet-client-backend"
          @keydown.ctrl.enter="onPreview"
        ></textarea>
        <div class="ta-ft">
          <button class="btn ghost" @click="onPreview" :disabled="!text.trim()">预览</button>
          <span class="hint">
            支持 <kbd>Ctrl</kbd>+<kbd>↵</kbd> · 不带 tag · 每行一个模块名
          </span>
        </div>
      </div>
    </div>

    <!-- ============== 通用预览 ============== -->
    <div v-if="preview.length" class="preview-block">
      <div class="ws-sub">重启清单</div>
      <div class="pv-hd">
        <span class="pv-total">{{ preview.length }} modules</span>
        <span class="pv-sum">
          <span class="ok">✓ {{ validCount }} 存在</span>
          <span v-if="invalidCount" class="err"> · ✗ {{ invalidCount }} 找不到</span>
        </span>
      </div>
      <div class="pv-table-wrap">
        <table class="pv-table">
          <thead>
            <tr>
              <th style="width:32%;">模块</th>
              <th style="width:16%;">状态</th>
              <th>当前 tag</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="m in preview" :key="m.name" :class="!m.exists && 'is-missing'">
              <td>
                <span class="pv-mod">{{ m.name }}</span>
                <div v-if="m.fixedFrom" class="fix-note">由 <b>{{ m.fixedFrom }}</b> 识别</div>
              </td>
              <td>
                <span v-if="m.exists" class="pill ok">
                  <el-icon><Check /></el-icon>存在
                </span>
                <span v-else class="pill miss">
                  <el-icon><Close /></el-icon>未找到
                </span>
              </td>
              <td>
                <span v-if="m.exists" class="tag-curr">{{ m.currentTag }}</span>
                <span v-else class="mute-text">—</span>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <div class="exec" v-if="preview.length">
      <div class="exec-info"></div>
      <button
        v-if="auth.isAdmin || auth.hasButton('restart')"
        :class="['cta', isProd ? 'danger' : 'primary']"
        :disabled="!validCount || submitting"
        @click="onRestart">
        <span v-if="submitting">重启中...</span>
        <span v-else-if="isProd">重启 {{ projectEnv.name }} · {{ validCount }} 个 · 需二次确认</span>
        <span v-else>重启 {{ projectEnv.name }} · {{ validCount }} 个</span>
        <el-icon v-if="!submitting"><ArrowRight /></el-icon>
      </button>
      <span v-else class="no-perm-hint">⚠ PROD 重启仅限管理员</span>
    </div>
  </div>
</template>

<script setup>
import { ref, computed } from 'vue'
import { ElMessage, ElMessageBox, ElNotification } from 'element-plus'
import { RefreshRight, ArrowRight, Check, Close } from '@element-plus/icons-vue'
import { restartModules } from '../api'
import { useAuthStore } from '../stores/auth'
import { useDeploymentsStore } from '../stores/deployments'

const props = defineProps(['projectEnv', 'modules'])
const emit = defineEmits(['done'])
const auth = useAuthStore()
const deployments = useDeploymentsStore()

// ---- 模式 ----
const mode = ref('select')
const selectedModules = ref([])
const text = ref('')

function switchMode(m) {
  if (mode.value === m) return
  // 切到手输：把已选模块序列化进 textarea
  if (m === 'manual' && selectedModules.value.length) {
    text.value = selectedModules.value.join('\n')
  }
  // 切到自动：把 textarea 解析回多选
  if (m === 'select' && text.value.trim()) {
    const seen = new Set()
    const sel = []
    for (const line of text.value.split(/\r?\n/)) {
      const trimmed = line.trim()
      if (!trimmed || trimmed.startsWith('#')) continue
      const mod = trimmed.includes(':') ? trimmed.split(':')[0].trim() : trimmed
      if (mod && !seen.has(mod) && props.modules.some(x => x.name === mod)) {
        seen.add(mod)
        sel.push(mod)
      }
    }
    selectedModules.value = sel
  }
  mode.value = m
}

const preview = ref([])
const submitting = ref(false)

const isProd = computed(() => props.projectEnv?.env_type === 'prod')
const validCount = computed(() => preview.value.filter(p => p.exists).length)
const invalidCount = computed(() => preview.value.filter(p => !p.exists).length)

// 自动模式：直接拿 selectedModules 当列表预览
function onPreviewSelect() {
  const seen = new Set()
  const out = []
  for (const name of selectedModules.value) {
    if (!name || seen.has(name)) continue
    seen.add(name)
    const m = props.modules.find(x => x.name === name)
    if (m) {
      out.push({
        name, exists: true, currentTag: m.current_tag, id: m.id,
        argoApp: m.argocd_app_name,
      })
    } else {
      out.push({ name, exists: false })
    }
  }
  preview.value = out
}

// 手输模式：解析 textarea
function onPreview() {
  const seen = new Set()
  const out = []
  text.value.split('\n').forEach(line => {
    line = line.trim()
    if (!line || line.startsWith('#')) return
    const hasColon = line.includes(':')
    const mod = hasColon ? line.split(':')[0].trim() : line
    if (!mod || seen.has(mod)) return
    seen.add(mod)
    const m = props.modules.find(x => x.name === mod)
    if (m) {
      out.push({
        name: mod, exists: true, currentTag: m.current_tag, id: m.id,
        argoApp: m.argocd_app_name,
        fixedFrom: hasColon ? line : '',
      })
    } else {
      out.push({ name: mod, exists: false, fixedFrom: hasColon ? line : '' })
    }
  })
  preview.value = out
}

async function onClearAll() {
  const n = selectedModules.value.length
  if (n === 0) return
  try {
    await ElMessageBox.confirm(
      `确认清空 ${n} 个已选模块？预览结果也会清掉。`,
      '清空已选',
      { type: 'warning', confirmButtonText: '确认清空', cancelButtonText: '取消', autofocus: false }
    )
  } catch (_) { return }
  selectedModules.value = []
  preview.value = []
}

async function onRestart() {
  const names = preview.value.filter(p => p.exists).map(p => p.name)
  if (!names.length) return
  const env = props.projectEnv.name
  const isProdEnv = isProd.value
  const headerText = isProdEnv
    ? `你正在向 <b>${env}</b>（生产）重启 <b>${names.length}</b> 个模块：`
    : `将在 <b>${env}</b> 重启 <b>${names.length}</b> 个模块：`
  const itemsHtml = names.map(n => `
    <li style="padding:6px 0;border-bottom:1px solid #f1f5f9;font-weight:500;color:#1f2937;">${n}</li>`).join('')
  const html = `
    <div style="font-size:13px;color:#374151;">${headerText}</div>
    <ul style="list-style:none;padding:0;margin:10px 0 0;max-height:240px;overflow-y:auto;border:1px solid #e5e7eb;border-radius:6px;background:#fafbfc;padding:0 12px;">${itemsHtml}</ul>`
  try {
    await ElMessageBox.confirm(
      html,
      isProdEnv ? '⚠ 生产环境二次确认' : '重启确认',
      {
        type: isProdEnv ? 'warning' : 'info',
        dangerouslyUseHTMLString: true,
        customClass: 'deploy-confirm-modal',
        confirmButtonText: isProdEnv ? `确认重启 ${env}` : '确认重启',
        cancelButtonText: '取消',
        closeOnClickModal: false,
        closeOnPressEscape: false,
        ...(isProdEnv ? { confirmButtonClass: 'el-button--danger' } : {}),
      }
    )
  } catch (_) { return }
  submitting.value = true
  try {
    let r
    try {
      r = await restartModules({ project_env_id: props.projectEnv.id, module_names: names })
    } catch (err) {
      handleLockConflict(err)
      throw err
    }
    ElMessage.success(`已触发 · #${r.deployment_id} · 进度看右下角浮动条`)
    deployments.startTracking(r.deployment_id, {
      action: 'restart',
      envName: props.projectEnv.name,
      envType: props.projectEnv.env_type,
      modules: names.length,
      operator: auth.user?.username || '',
    })
    emit('done', r.deployment_id)
    text.value = ''
    selectedModules.value = []
    preview.value = []
  } catch (_) { /* 已处理 */ }
  finally { submitting.value = false }
}

function handleLockConflict(err) {
  const status = err?.response?.status
  const data = err?.response?.data
  if (status !== 409 || !data?.data?.conflicts?.length) {
    ElMessage.error(data?.message || err?.message || '提交失败')
    return
  }
  const conflicts = data.data.conflicts
  const lines = conflicts.map(c => {
    const sec = c.elapsed_sec || 0
    const elapsed = sec < 60 ? `${sec}s` : `${Math.floor(sec/60)}m ${sec%60}s`
    return `<b>${c.module}</b>：${c.operator || '其他人'} 正在发布（已耗时 ${elapsed}）`
  }).join('<br>')
  ElNotification({
    title: '⚠ 重启被拒绝',
    dangerouslyUseHTMLString: true,
    message: `<div style="line-height:1.7">${lines}<br><span style="color:#94a3b8;font-size:11px">请等候完成后再试</span></div>`,
    type: 'warning',
    duration: 8000,
    position: 'top-right',
  })
}
</script>

<style scoped>
.rs-panel { display: flex; flex-direction: column; }

.p-hd { padding: 14px 20px; border-bottom: 1px solid var(--border-soft); display: flex; justify-content: space-between; align-items: center; }
.p-hd h3 { font: 600 14px/1 var(--body); color: var(--text); display: flex; align-items: center; gap: 8px; }
.p-hd h3 .el-icon { color: var(--primary); font-size: 16px; }

/* 模式切换 */
.mode-toggle { display: flex; gap: 0; border: 1px solid var(--border); border-radius: 5px; overflow: hidden; }
.mt-btn { padding: 5px 14px; font-size: 12px; background: #fff; color: var(--text-2); border: none; cursor: pointer; transition: all .12s; }
.mt-btn:hover { color: var(--primary); }
.mt-btn.on { background: var(--primary); color: #fff; }
.mt-btn + .mt-btn { border-left: 1px solid var(--border); }

/* 自动模式 */
.select-mode { padding: 16px 20px; }
.sm-toolbar { display: flex; justify-content: space-between; align-items: center; margin-bottom: 14px; flex-wrap: wrap; gap: 10px; }
.sm-tip { font-size: 11.5px; color: var(--text-3); }
.sm-cache { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.sm-row { margin-bottom: 12px; }
.sm-row label { display: block; font-size: 11.5px; color: var(--text-2); margin-bottom: 6px; font-weight: 500; }
.sm-actions { display: flex; align-items: center; gap: 12px; }
.btn.sm { padding: 4px 10px; font-size: 11.5px; }
.btn.danger-hover:hover:not(:disabled) { color: var(--danger); border-color: var(--danger); background: #fef2f2; }

/* 手输模式 (保留原样) */
.ws-grid { display: grid; grid-template-columns: 1fr; }
.ws-col { padding: 16px 20px; }
.ws-col.in { border-right: none; }
.ws-sub { font-size: 11px; color: var(--text-3); text-transform: uppercase; letter-spacing: .8px; font-weight: 600; margin-bottom: 10px; }

.ta {
  width: 100%; min-height: 180px;
  background: var(--bg-input); border: 1px solid var(--border); border-radius: 5px;
  padding: 12px 14px; color: var(--text);
  font: 500 13px/1.85 var(--mono);
  resize: vertical; transition: all .15s;
}
.ta:focus { outline: none; border-color: var(--primary); box-shadow: 0 0 0 3px rgba(59,130,246,.12); background: #fff; }
.ta::placeholder { color: var(--text-3); }

.ta-ft { display: flex; justify-content: space-between; align-items: center; margin-top: 12px; }
.btn { background: #fff; border: 1px solid var(--border); color: var(--text); padding: 6px 14px; border-radius: 5px; font: 500 12.5px var(--body); cursor: pointer; }
.btn.ghost:hover { border-color: var(--primary); color: var(--primary); }
.btn:disabled { opacity: .4; cursor: not-allowed; }

.hint { font-size: 11.5px; color: var(--text-3); }
.hint kbd { font-family: var(--mono); font-size: 10.5px; background: var(--bg-hover); border: 1px solid var(--border); padding: 0 5px; border-radius: 3px; color: var(--text-2); }

/* 通用预览区 */
.preview-block { padding: 14px 20px; border-top: 1px solid var(--border-soft); }
.preview-block .ws-sub { margin-bottom: 8px; }

.pv-hd { display: flex; justify-content: space-between; align-items: center; margin-bottom: 8px; margin-top: -4px; }
.pv-total { font-size: 11.5px; color: var(--text-3); font-family: var(--mono); }
.pv-sum { font-size: 11.5px; font-family: var(--mono); }
.pv-sum .ok { color: var(--success); font-weight: 600; }
.pv-sum .err { color: var(--danger); }

.pv-table-wrap { border: 1px solid var(--border); border-radius: 5px; overflow: auto; max-height: 380px; background: #fff; }
.pv-table { width: 100%; border-collapse: collapse; font-size: 12.5px; }
.pv-table thead { position: sticky; top: 0; z-index: 1; }
.pv-table th { background: #f9fafb; text-align: left; padding: 8px 10px; border-bottom: 1px solid var(--border); color: var(--text-3); font: 600 10.5px var(--body); text-transform: uppercase; letter-spacing: .5px; }
.pv-table td { padding: 9px 10px; border-bottom: 1px solid var(--border-soft); vertical-align: middle; }
.pv-table tr:last-child td { border-bottom: none; }
.pv-table tr:hover td { background: #fafbfc; }
.pv-table tr.is-missing td { background: #fef8f8; }
.pv-table tr.is-missing .pv-mod { color: var(--danger); }
.pv-mod { color: var(--text); font-size: 12.5px; font-weight: 500; font-family: var(--mono); }
.mute-text { color: var(--text-3); }
.tag-curr { color: var(--text-2); background: var(--bg-hover); padding: 1px 6px; border-radius: 3px; font-family: var(--mono); font-size: 11px; }
.fix-note { font-size: 10.5px; color: var(--warning); margin-top: 3px; font-family: var(--body); }
.fix-note b { font-family: var(--mono); }

.pill {
  display: inline-flex; align-items: center; gap: 4px;
  padding: 2px 8px; border-radius: 99px; font-size: 11px; font-weight: 500;
}
.pill .el-icon { font-size: 10px; }
.pill.ok { background: #ecfdf5; color: var(--success); }
.pill.miss { background: #fef2f2; color: var(--danger); }

.exec {
  border-top: 1px solid var(--border-soft);
  background: var(--primary-bg); padding: 14px 22px;
  display: flex; justify-content: space-between; align-items: center;
  border-bottom-left-radius: var(--radius); border-bottom-right-radius: var(--radius);
}
.exec-info { font-size: 12.5px; color: #1e40af; }
.exec-info b { color: #1e3a8a; font-family: var(--mono); font-weight: 600; }

.cta {
  background: var(--primary); color: #fff; border: none;
  padding: 10px 22px; border-radius: 5px;
  font: 600 13.5px var(--body); cursor: pointer;
  display: flex; gap: 8px; align-items: center;
}
.cta:hover:not(:disabled) { background: var(--primary-dark); }
.cta:disabled { opacity: .4; cursor: not-allowed; }
.cta .el-icon { font-size: 14px; }
.cta.danger { background: var(--danger); }
.cta.danger:hover:not(:disabled) { background: var(--danger-dark, #dc2626); }

.no-perm-hint {
  font-size: 12.5px; color: var(--warning);
  padding: 10px 16px; border: 1px dashed var(--warning);
  border-radius: 5px;
}
</style>
