<template>
  <div class="up-panel">
    <div class="p-hd">
      <h3>
        <el-icon><Upload /></el-icon>
        批量更新镜像
      </h3>
      <div class="mode-toggle">
        <button :class="['mt-btn', mode === 'select' && 'on']" @click="switchMode('select')">自动模式</button>
        <button :class="['mt-btn', mode === 'manual' && 'on']" @click="switchMode('manual')">手输模式</button>
      </div>
    </div>

    <!-- ============== 选择模式：多选模块 + 每模块 tag 下拉 ============== -->
    <div v-if="mode === 'select'" class="select-mode">
      <div class="sm-toolbar">
        <span class="sm-tip">从 Harbor 拉最近 100 个 tag · 按推送时间倒序 · 不够可点「加载更早」翻页</span>
        <div class="sm-cache">
          <span class="hint">{{ argocdCacheHint }}</span>
          <button class="btn ghost sm" @click="onRefreshCache" :disabled="refreshing">
            {{ refreshing ? '刷新中...' : '🔄 刷新校验数据' }}
          </button>
          <button v-if="selectedModules.length" class="btn ghost sm danger-hover" @click="onClearAll">
            ✕ 清空已选 ({{ selectedModules.length }})
          </button>
        </div>
      </div>
      <div class="sm-row">
        <label>选模块（多选 · 可搜索）</label>
        <el-select v-model="selectedModules" multiple filterable collapse-tags collapse-tags-tooltip
          placeholder="点击下拉选择 / 输入关键字搜索" style="width:100%;"
          @change="onSelectedChanged">
          <el-option v-for="m in props.modules" :key="m.name"
            :label="m.name" :value="m.name" />
        </el-select>
      </div>
      <div v-if="selectedModules.length" class="sm-table-wrap">
        <table class="sm-table">
          <thead>
            <tr><th style="width:36%;">模块</th><th style="width:24%;">当前 tag</th><th>新 tag</th></tr>
          </thead>
          <tbody>
            <tr v-for="m in selectedModules" :key="m">
              <td class="pv-mod">{{ m }}</td>
              <td class="mono mute-text">{{ getModule(m).current_tag || '-' }}</td>
              <td>
                <el-select v-model="modulePicks[m]" filterable :loading="loadingTags[m]"
                  placeholder="选择 tag"
                  @click.once="fetchTags(m)"
                  style="width:100%;">
                  <el-option v-for="t in mergedTags(m)" :key="t.name"
                    :label="t.name + (t.pinned ? ' · ↩ 回滚目标' : ' · ' + relTime(t.pushed_at))"
                    :value="t.name" />
                  <template #footer>
                    <button v-if="tagsByModule[m]?.length" class="load-more-btn"
                      :disabled="loadingTags[m] || tagsAllLoaded[m]"
                      @click.stop="loadMoreTags(m)">
                      <span v-if="loadingTags[m]">加载中...</span>
                      <span v-else-if="tagsAllLoaded[m]">已无更多 · 共 {{ tagsByModule[m].length }} 条</span>
                      <span v-else>📥 加载更早 (已 {{ tagsByModule[m].length }} 条)</span>
                    </button>
                  </template>
                </el-select>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <div class="sm-actions">
        <button class="btn ghost" @click="onPreviewSelect" :disabled="!hasAnyPick || previewing">
          {{ previewing ? '分析中...' : '预览变更' }}
        </button>
        <span class="hint" v-if="!hasAnyPick">每个已选模块都要先选一个 tag</span>
      </div>
    </div>

    <!-- ============== 手输模式（保留原 textarea 工作流） ============== -->
    <div v-else class="ws-grid">
      <!-- 输入 -->
      <div class="ws-col in">
        <div class="ws-sub">输入</div>
        <textarea
          v-model="text"
          class="ta"
          spellcheck="false"
          placeholder="atmosphere-frontend:20260416014126-83
base-client-backend:20260416020000-99"
          @keydown.ctrl.enter="onPreview"
        ></textarea>
        <div class="ta-ft">
          <button class="btn ghost" @click="onPreview" :disabled="!text.trim() || previewing">
            <span v-if="!previewing">预览变更</span>
            <span v-else>分析中...</span>
          </button>
          <span class="hint">
            支持 <kbd>Ctrl</kbd>+<kbd>↵</kbd> · 粘贴 <code>模块:tag</code>
          </span>
        </div>
      </div>

    </div>

    <!-- ============== 通用预览区（两种模式共享） ============== -->
    <div v-if="diff.length" class="preview-block">
      <div class="ws-sub">变更预览</div>
      <div class="pv-hd">
        <span class="pv-total">{{ diff.length }} modules</span>
        <span class="pv-sum">
          <span class="ok">✓ {{ validCount }} ready</span>
          <span v-if="skipCount" class="mute"> · {{ skipCount }} 无变化</span>
          <span v-if="newCount" class="warn"> · {{ newCount }} 未登记</span>
        </span>
      </div>
      <div class="pv-table-wrap">
        <table class="pv-table">
          <thead>
            <tr>
              <th style="width:38%;">模块</th>
              <th style="width:28%;">当前 TAG</th>
              <th style="width:28%;">新 TAG</th>
              <th style="width:6%;text-align:center;">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="d in sortedDiff" :key="d.module" :class="[d.skip && 'is-skip', d.is_new && 'is-new']">
              <td class="pv-mod">
                {{ d.module }}
                <span v-if="d.is_new" class="warn-text">· 未登记</span>
                <span v-else-if="d.skip" class="mute-text">· 无变化</span>
              </td>
              <td>
                <span v-if="d.from_tag" class="tag-curr">{{ d.from_tag }}</span>
                <span v-else class="mute-text">—</span>
              </td>
              <td>
                <span v-if="d.is_new" class="mute-text">—</span>
                <span v-else-if="d.skip" class="mute-text">{{ d.to_tag }}</span>
                <span v-else class="tag-new">{{ d.to_tag }}</span>
              </td>
              <td style="text-align:center;">
                <button class="row-del-btn" @click="removeFromPreview(d.module)" title="从本次发布移除">✕</button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <!-- ============== 预检结果（Harbor + ArgoCD + GitLab 三方一致性） ============== -->
    <div v-if="precheckBundle && (precheckBundle.passed?.length || precheckBundle.failed?.length)"
         class="precheck-block">
      <div class="pc-head">
        <div>
          <span class="pc-title">预检结果</span>
          <span class="pc-stat ok">通过 {{ precheckBundle.passed?.length || 0 }}</span>
          <span v-if="precheckBundle.failed?.length" class="pc-stat fail">
            未通过 {{ precheckBundle.failed.length }}
          </span>
        </div>
        <div class="pc-cache">
          <span class="hint">{{ argocdCacheHint }}</span>
          <button class="btn ghost sm" @click="onRefreshCache" :disabled="refreshing">
            {{ refreshing ? '刷新中...' : '🔄 刷新校验' }}
          </button>
        </div>
      </div>

      <!-- 未通过组：永远展开 -->
      <div v-if="precheckBundle.failed?.length" class="pc-group fail-group">
        <div class="pc-group-head">
          ⚠️ 未通过 ({{ precheckBundle.failed.length }})
          <span class="hint">— 提交时将自动跳过</span>
          <button class="btn ghost sm" style="margin-left:auto;"
            @click="copyPrecheck(precheckBundle.failed)">📋 复制为 module:tag</button>
        </div>
        <ul class="pc-list">
          <li v-for="f in precheckBundle.failed" :key="f.module">
            <div class="pc-mod">
              <b>{{ f.module }}</b>
              <span v-if="f.new_tag" class="mono mute-text">
                {{ f.old_tag || '-' }} → <span style="color:var(--warning);">{{ f.new_tag }}</span>
              </span>
            </div>
            <div class="pc-reason">{{ f.reason }}</div>
          </li>
        </ul>
      </div>

      <!-- 通过组：可折叠 + 搜索过滤 -->
      <details v-if="precheckBundle.passed?.length" class="pc-group pass-group" :open="passedOpen">
        <summary @click.prevent="passedOpen = !passedOpen">
          ✅ 通过 ({{ precheckBundle.passed.length }})
          <span class="hint">— 提交后会进 git</span>
          <span class="chev">{{ passedOpen ? '▼' : '▶' }}</span>
        </summary>
        <div v-if="passedOpen" class="pc-pass-body">
          <div class="pc-filter-row">
            <input v-model="passedFilter" class="filter-inp" placeholder="🔍 筛选模块名" />
            <button class="btn ghost sm" @click="copyPrecheck(precheckBundle.passed)">📋 复制为 module:tag</button>
          </div>
          <ul class="pc-list">
            <li v-for="p in filteredPassed" :key="p.module">
              <div class="pc-mod">
                <b>{{ p.module }}</b>
                <span v-if="p.new_tag" class="mono">
                  {{ p.old_tag || '-' }} → <span style="color:var(--success);">{{ p.new_tag }}</span>
                </span>
              </div>
            </li>
          </ul>
        </div>
      </details>
    </div>

    <!-- 提交条 -->
    <div class="exec" v-if="diff.length">
      <div class="exec-info">
        <!-- 额外艾特（发布才有；回滚不带）-->
        <div v-if="!isRollback" class="extra-at">
          <span class="ea-label">额外艾特</span>
          <el-select v-model="extraAt" multiple filterable placeholder="选通知人（可留空）" size="small" style="min-width:280px">
            <el-option v-for="c in atContactsWithLark" :key="c.lark_id" :label="c.name" :value="c.lark_id" />
          </el-select>
          <span v-if="envFixedAtNames.length" class="ea-fixed">已固定艾特：{{ envFixedAtNames.join('、') }}（自动带）</span>
        </div>
      </div>
      <button
        v-if="canSubmit"
        :class="['cta', isProd ? 'danger' : (isRollback ? 'warn' : 'success')]"
        :disabled="!validCount || submitting"
        @click="onSubmit">
        <span v-if="submitting">提交中...</span>
        <span v-else-if="isRollback && isProd">回滚 PROD · {{ projectEnv.name }} · 需二次确认</span>
        <span v-else-if="isRollback">回滚到 {{ projectEnv.name }} · {{ validCount }} 个</span>
        <span v-else-if="isProd">提交到 {{ projectEnv.name }} · 需二次确认</span>
        <span v-else>提交到 {{ projectEnv.name }} · {{ validCount }} 个</span>
        <el-icon v-if="!submitting"><ArrowRight /></el-icon>
      </button>
      <span v-else class="no-perm-hint">⚠ PROD 发布仅限管理员</span>
    </div>
  </div>
</template>

<script setup>
import { ref, reactive, computed, watch, onMounted } from 'vue'
import { ElMessage, ElMessageBox, ElNotification } from 'element-plus'
import { Upload, ArrowRight } from '@element-plus/icons-vue'
import { previewImage, updateImage, listHarborTags, refreshArgocdAppCache, listContacts } from '../api'
import { useAuthStore } from '../stores/auth'
import { useDeploymentsStore } from '../stores/deployments'

const props = defineProps(['projectEnv', 'modules', 'rollbackMode'])

// 额外艾特（发布临时）+ 环境固定艾特（项目参数配的，只读展示）
const extraAt = ref([])
const atContacts = ref([])
const atContactsWithLark = computed(() => atContacts.value.filter(c => c.lark_id))
const envFixedAtNames = computed(() => {
  const ids = (props.projectEnv?.at_lark_ids || '').split(/[\n,\s]+/).map(s => s.trim()).filter(Boolean)
  return ids.map(id => atContacts.value.find(c => c.lark_id === id)?.name || id)
})
const emit = defineEmits(['done', 'rollback-consumed'])
const auth = useAuthStore()
const deployments = useDeploymentsStore()

// ---- 模式：选择 (默认) / 手输 ----
const mode = ref('select')
function switchMode(m) {
  if (mode.value === m) return
  // 切到手输：把当前选择的模块 tag 序列化进 textarea
  if (m === 'manual' && selectedModules.value.length) {
    const lines = selectedModules.value
      .filter(name => modulePicks[name])
      .map(name => `${name}:${modulePicks[name]}`)
    if (lines.length) text.value = lines.join('\n')
  }
  // 切到选择：把 textarea 解析回多选 + 每行下拉
  if (m === 'select' && text.value.trim()) {
    const picks = {}
    const sel = []
    for (const line of text.value.split(/\r?\n/)) {
      const trimmed = line.trim()
      if (!trimmed || trimmed.startsWith('#')) continue
      const idx = trimmed.indexOf(':')
      if (idx <= 0) continue
      const mod = trimmed.slice(0, idx).trim()
      const tag = trimmed.slice(idx + 1).trim()
      if (mod && tag) {
        sel.push(mod)
        picks[mod] = tag
      }
    }
    selectedModules.value = sel
    Object.keys(modulePicks).forEach(k => delete modulePicks[k])
    Object.assign(modulePicks, picks)
  }
  mode.value = m
}

// ---- 选择模式状态 ----
const selectedModules = ref([])
const modulePicks = reactive({})       // { module → tag }
const tagsByModule = reactive({})      // { module → 累积 [{name, pushed_at, ...}] }
const loadingTags = reactive({})       // { module → bool }
const tagPageByModule = reactive({})   // { module → 当前已加载到第几页 }
const tagsAllLoaded = reactive({})     // { module → bool 已无更多 }
const refreshing = ref(false)
const argocdCacheAt = ref(0)           // unix seconds
const rollbackPinned = reactive({})    // { module → 回滚目标 tag }；即便 Harbor 最近 100 没拉到也要让下拉能显示

const hasAnyPick = computed(() => selectedModules.value.length > 0
  && selectedModules.value.every(m => modulePicks[m]))

const argocdCacheHint = computed(() => {
  if (!argocdCacheAt.value) return '校验数据未加载'
  const sec = Math.max(0, Math.floor(Date.now() / 1000 - argocdCacheAt.value))
  if (sec < 60) return `校验数据 ${sec} 秒前同步`
  return `校验数据 ${Math.floor(sec / 60)} 分钟前同步`
})

function getModule(name) {
  return (props.modules || []).find(m => m.name === name) || { name, current_tag: '' }
}

function relTime(ts) {
  if (!ts) return ''
  const t = new Date(ts).getTime()
  const sec = Math.max(0, Math.floor((Date.now() - t) / 1000))
  if (sec < 60) return sec + 's 前'
  if (sec < 3600) return Math.floor(sec / 60) + 'm 前'
  if (sec < 86400) return Math.floor(sec / 3600) + 'h 前'
  return Math.floor(sec / 86400) + 'd 前'
}

function onSelectedChanged(newList) {
  // 自动给每个新选的模块拉 tag 列表
  for (const name of newList) {
    if (!tagsByModule[name] && !loadingTags[name]) {
      fetchTags(name)
    }
  }
}

async function fetchTags(name) {
  // 不读 tagsByModule 当 cache（永远拉 Harbor 拿最新 tag）；保留 loadingTags 防并发去重
  if (loadingTags[name]) return
  loadingTags[name] = true
  try {
    const r = await listHarborTags(props.projectEnv.id, name, 1, 100)
    tagsByModule[name] = r.tags || []
    tagPageByModule[name] = 1
    tagsAllLoaded[name] = !r.has_more
    // 默认填最新（仅当用户还没手动选过）
    if (!modulePicks[name] && tagsByModule[name].length > 0) {
      modulePicks[name] = tagsByModule[name][0].name
    }
  } catch (e) {
    ElMessage.error(`${name} · ${e?.response?.data?.message || e.message || '拉 tag 失败'}`)
    tagsByModule[name] = []
  } finally {
    loadingTags[name] = false
  }
}

async function loadMoreTags(name) {
  if (loadingTags[name] || tagsAllLoaded[name]) return
  const nextPage = (tagPageByModule[name] || 1) + 1
  loadingTags[name] = true
  try {
    const r = await listHarborTags(props.projectEnv.id, name, nextPage, 100)
    const more = r.tags || []
    tagsByModule[name] = [...(tagsByModule[name] || []), ...more]
    tagPageByModule[name] = nextPage
    tagsAllLoaded[name] = !r.has_more
  } catch (e) {
    ElMessage.error(`${name} · ${e?.response?.data?.message || e.message || '加载更早失败'}`)
  } finally {
    loadingTags[name] = false
  }
}

// 一键清空已选模块（永远二次确认）
async function onClearAll() {
  const n = selectedModules.value.length
  if (n === 0) return
  try {
    await ElMessageBox.confirm(
      `确认清空 ${n} 个已选模块？所选 tag、预览结果都会清掉，但 Harbor tag 缓存保留。`,
      '清空已选',
      { type: 'warning', confirmButtonText: '确认清空', cancelButtonText: '取消', autofocus: false }
    )
  } catch (_) { return }
  selectedModules.value = []
  Object.keys(modulePicks).forEach(k => delete modulePicks[k])
  diff.value = []
  precheckBundle.value = null
}

function onPreviewSelect() {
  // 序列化选择 → text，触发统一的 onPreview
  text.value = selectedModules.value
    .filter(m => modulePicks[m])
    .map(m => `${m}:${modulePicks[m]}`)
    .join('\n')
  if (text.value) onPreview()
}

async function onRefreshCache() {
  if (!props.projectEnv?.id) return
  refreshing.value = true
  try {
    const r = await refreshArgocdAppCache(props.projectEnv.id)
    argocdCacheAt.value = r.refresh_at || Math.floor(Date.now() / 1000)
    // 同时清掉已加载的 Harbor tag 缓存（前端层），让用户重新看到最新 tag
    Object.keys(tagsByModule).forEach(k => delete tagsByModule[k])
    Object.keys(tagPageByModule).forEach(k => delete tagPageByModule[k])
    Object.keys(tagsAllLoaded).forEach(k => delete tagsAllLoaded[k])
    // 重新拉已选模块的 tag
    for (const name of selectedModules.value) {
      fetchTags(name)
    }
    ElMessage.success(`校验数据已刷新 · ArgoCD 应用 ${r.app_count} 个`)
  } catch (e) {
    ElMessage.error(e?.response?.data?.message || e.message || '刷新失败')
  } finally {
    refreshing.value = false
  }
}

// ---- 预检 ----
const precheckBundle = ref(null)
const passedFilter = ref('')
const passedOpen = ref(true)
const filteredPassed = computed(() => {
  if (!precheckBundle.value?.passed) return []
  const q = passedFilter.value.trim().toLowerCase()
  if (!q) return precheckBundle.value.passed
  return precheckBundle.value.passed.filter(p => p.module.toLowerCase().includes(q))
})

function copyPrecheck(items) {
  if (!items?.length) return
  const text = items.map(it => `${it.module}:${it.new_tag || ''}`).join('\n')
  if (navigator.clipboard) {
    navigator.clipboard.writeText(text).then(
      () => ElMessage.success(`已复制 ${items.length} 行到剪贴板`),
      () => ElMessage.error('复制失败，请手动选中')
    )
  }
}

onMounted(async () => {
  // 进面板时拉一次 ArgoCD 校验缓存（异步，不阻塞）
  if (props.projectEnv?.id) {
    refreshArgocdAppCache(props.projectEnv.id).then(r => {
      argocdCacheAt.value = r.refresh_at || Math.floor(Date.now() / 1000)
    }).catch(() => { /* 静默失败 */ })
  }
  atContacts.value = (await listContacts().catch(() => [])) || []
})

const isRollback = computed(() => !!props.rollbackMode)

// 发布按钮显示规则：admin 放行；否则按操作类型检查对应权限
//   - 回滚 → rollback 权限
//   - PROD 发布 → submit_prod
//   - UAT 发布 → submit_uat
const canSubmit = computed(() => {
  if (auth.isAdmin) return true
  if (isRollback.value) return auth.hasButton('rollback')
  if (isProd.value) return auth.hasButton('submit_prod')
  return auth.hasButton('submit_uat')
})
const text = ref('')
const diff = ref([])

// rollbackMode 进入时自动预填 textarea + 选择模式状态
//
//   关键：默认模式是 select，用户切到回滚后看到的是下拉，必须把 selectedModules + modulePicks
//   也填上，否则 fetchTags 会用 Harbor 最新 tag 覆盖回滚目标。
//   rollbackPinned 记下"回滚目标"，让 dropdown 即使 Harbor 最近 100 tag 没包含也能显示。
watch(() => props.rollbackMode, (m) => {
  if (!m?.prefillText) return
  text.value = m.prefillText
  diff.value = []
  precheckBundle.value = null

  // 解析 "module:tag" 行 → 同步进 selectedModules + modulePicks + rollbackPinned
  const sel = []
  Object.keys(modulePicks).forEach(k => delete modulePicks[k])
  Object.keys(rollbackPinned).forEach(k => delete rollbackPinned[k])
  for (const line of m.prefillText.split(/\r?\n/)) {
    const trimmed = line.trim()
    if (!trimmed) continue
    const idx = trimmed.indexOf(':')
    if (idx <= 0) continue
    const mod = trimmed.slice(0, idx).trim()
    const tag = trimmed.slice(idx + 1).trim()
    if (mod && tag) {
      sel.push(mod)
      modulePicks[mod] = tag
      rollbackPinned[mod] = tag
    }
  }
  selectedModules.value = sel
}, { immediate: true })

// mergedTags 合并 Harbor 拉到的 tag 列表 + 回滚目标 tag（Harbor 没返回也要置顶展示）
function mergedTags(name) {
  const list = tagsByModule[name] || []
  const pinned = rollbackPinned[name]
  if (!pinned) return list
  if (list.some(t => t.name === pinned)) return list
  return [{ name: pinned, pinned: true }, ...list]
}
const previewing = ref(false)
const submitting = ref(false)

const isProd = computed(() => props.projectEnv?.env_type === 'prod')
const validCount = computed(() => diff.value.filter(d => !d.skip && !d.is_new).length)
const skipCount = computed(() => diff.value.filter(d => d.skip).length)
const newCount = computed(() => diff.value.filter(d => d.is_new).length)
const repoShort = computed(() => (props.projectEnv?.git_repo || '').split('/').pop() || '?')

const sortedDiff = computed(() => {
  return [...diff.value].sort((a, b) => {
    const order = d => (d.is_new ? 2 : d.skip ? 1 : 0)
    return order(a) - order(b)
  })
})

function shortTag(t) {
  if (!t) return '—'
  return t.length > 12 ? '…' + t.slice(-10) : t
}

// removeFromPreview 行级 ✕：把这个模块从 diff/select/text/precheck 全部清掉，再次预览也不会带回来
//   不做二次确认 —— 删错了上面下拉再选回来即可
function removeFromPreview(name) {
  diff.value = diff.value.filter(d => d.module !== name)
  // 自动模式：从已选 + tag 选择中剔除
  selectedModules.value = selectedModules.value.filter(m => m !== name)
  delete modulePicks[name]
  delete rollbackPinned[name]
  // 手输模式：从 textarea 拿掉对应行（保留其他行的格式）
  if (text.value) {
    const kept = text.value.split(/\r?\n/).filter(line => {
      const t = line.trim()
      if (!t || t.startsWith('#')) return true
      const idx = t.indexOf(':')
      if (idx <= 0) return true
      return t.slice(0, idx).trim() !== name
    })
    text.value = kept.join('\n')
  }
  // 同步 precheck 结果分组
  if (precheckBundle.value) {
    const filterOut = arr => (arr || []).filter(x => x.module !== name)
    precheckBundle.value = {
      ...precheckBundle.value,
      passed: filterOut(precheckBundle.value.passed),
      failed: filterOut(precheckBundle.value.failed),
    }
  }
}

async function onPreview() {
  if (!text.value.trim()) return
  previewing.value = true
  try {
    const r = await previewImage({ project_env_id: props.projectEnv.id, text: text.value })
    diff.value = r.diff || []
    precheckBundle.value = r.precheck || null
    if (r.precheck?.argocd_cache_at) argocdCacheAt.value = r.precheck.argocd_cache_at
  } finally { previewing.value = false }
}

async function onSubmit() {
  const changes = diff.value.filter(d => !d.skip && !d.is_new).map(d => ({ module: d.module, tag: d.to_tag }))
  if (!changes.length) return
  const env = props.projectEnv.name
  const rbMode = isRollback.value

  // 二次确认：PROD 任何操作 必确认；回滚操作（UAT/PROD）也必确认
  if (isProd.value || rbMode) {
    const title = rbMode ? '⚠ 回滚二次确认' : '⚠ 生产环境二次确认'
    const headerText = rbMode
      ? `即将回滚 <b>${changes.length}</b> 个模块到 <code style="background:#f3f4f6;padding:1px 6px;border-radius:3px;">#${props.rollbackMode.refDeploymentID}</code> 版本：`
      : `你正在向 <b>${env}</b> 提交 <b>${changes.length}</b> 个模块到 GitLab，操作不可撤销：`
    const items = changes.map(c => `
      <li style="padding:6px 0;border-bottom:1px solid #f1f5f9;">
        <div style="font-weight:600;color:#1f2937;">${c.module}</div>
        <div style="margin-top:2px;font-family:'Fira Code',monospace;font-size:12px;color:#10b981;">→ ${c.tag}</div>
      </li>`).join('')
    const html = `
      <div style="font-size:13px;color:#374151;">${headerText}</div>
      <ul style="list-style:none;padding:0;margin:10px 0 0;max-height:240px;overflow-y:auto;border:1px solid #e5e7eb;border-radius:6px;background:#fafbfc;padding:0 12px;">${items}</ul>`
    try {
      await ElMessageBox.confirm(html, title, {
        type: 'warning',
        dangerouslyUseHTMLString: true,
        customClass: 'deploy-confirm-modal',
        confirmButtonText: rbMode ? `确认回滚到 ${env}` : `确认提交到 ${env}`,
        cancelButtonText: '取消',
        confirmButtonClass: 'el-button--danger',
        closeOnClickModal: false,
        closeOnPressEscape: false,
      })
    } catch (_) { return }
  }

  submitting.value = true
  try {
    const payload = { project_env_id: props.projectEnv.id, changes }
    if (rbMode) payload.ref_deployment_id = props.rollbackMode.refDeploymentID
    else payload.at_lark_ids = extraAt.value // 发布：本次临时额外艾特人（回滚不带）
    let r
    try {
      r = await updateImage(payload)
    } catch (err) {
      handleLockConflict(err)
      throw err // 让 finally 跑
    }
    ElMessage.success(`${rbMode ? '回滚' : '发布'}已提交 · #${r.deployment_id} · 进度看右下角浮动条`)
    deployments.startTracking(r.deployment_id, {
      action: rbMode ? 'rollback' : 'update_image',
      envName: props.projectEnv.name,
      envType: props.projectEnv.env_type,
      modules: changes.length,
      operator: auth.user?.username || '',
    })
    emit('done', r.deployment_id)
    if (rbMode) emit('rollback-consumed')
    text.value = ''
    diff.value = []
  } catch (_) { /* 已经处理过了 */ }
  finally { submitting.value = false }
}

// 同 (env, module) 互斥锁冲突 → 后端返回 409 + conflicts 数组，前端右上角 ElNotification
function handleLockConflict(err) {
  const status = err?.response?.status
  const data = err?.response?.data
  if (status !== 409 || !data?.data?.conflicts?.length) {
    // 非锁冲突 → 走默认 toast
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
    title: '⚠ 发布被拒绝',
    dangerouslyUseHTMLString: true,
    message: `<div style="line-height:1.7">${lines}<br><span style="color:#94a3b8;font-size:11px">请等候完成后再试</span></div>`,
    type: 'warning',
    duration: 8000,
    position: 'top-right',
  })
}
</script>

<style scoped>
.up-panel { display: flex; flex-direction: column; }

.p-hd { padding: 14px 20px; border-bottom: 1px solid var(--border-soft); display: flex; justify-content: space-between; align-items: center; }
.p-hd h3 { font: 600 14px/1 var(--body); color: var(--text); display: flex; align-items: center; gap: 8px; }
.p-hd h3 .el-icon { color: var(--primary); font-size: 16px; }
.p-hd .sub { font: 500 11.5px var(--mono); color: var(--text-3); }

.ws-grid { display: grid; grid-template-columns: 1.3fr 1fr; gap: 0; }
.ws-col { padding: 16px 20px; }
.ws-col.in { border-right: 1px solid var(--border-soft); }
.ws-sub { font-size: 11px; color: var(--text-3); text-transform: uppercase; letter-spacing: .8px; font-weight: 600; margin-bottom: 10px; }

.ta {
  width: 100%; min-height: 180px;
  background: var(--bg-input); border: 1px solid var(--border); border-radius: 5px;
  padding: 12px 14px; color: var(--text);
  font: 500 13px/1.85 var(--mono);
  resize: vertical; transition: all .15s;
}
.ta:focus {
  outline: none; border-color: var(--primary);
  box-shadow: 0 0 0 3px rgba(59, 130, 246, .12);
  background: #fff;
}
.ta::placeholder { color: var(--text-3); }

.ta-ft { display: flex; justify-content: space-between; align-items: center; margin-top: 12px; }
.btn {
  background: #fff; border: 1px solid var(--border); color: var(--text);
  padding: 6px 14px; border-radius: 5px;
  font: 500 12.5px var(--body); cursor: pointer;
}
.btn.ghost:hover { border-color: var(--primary); color: var(--primary); }
.btn:disabled { opacity: .4; cursor: not-allowed; }

.hint { font-size: 11.5px; color: var(--text-3); }
.hint code { font-family: var(--mono); background: var(--bg-hover); padding: 1px 5px; border-radius: 3px; color: var(--text-2); font-size: 11px; }
.hint kbd { font-family: var(--mono); font-size: 10.5px; background: var(--bg-hover); border: 1px solid var(--border); padding: 0 5px; border-radius: 3px; color: var(--text-2); }

.empty-pv { padding: 40px 0; text-align: center; color: var(--text-3); }
.empty-pv .ep-t { font-size: 13px; color: var(--text-2); font-weight: 500; margin-bottom: 4px; }
.empty-pv .ep-d { font-size: 11.5px; }

.pv-hd { display: flex; justify-content: space-between; align-items: center; margin-bottom: 8px; margin-top: -4px; }
.pv-total { font-size: 11.5px; color: var(--text-3); font-family: var(--mono); }
.pv-sum { font-size: 11.5px; font-family: var(--mono); }
.pv-sum .ok { color: var(--success); font-weight: 600; }
.pv-sum .mute { color: var(--text-3); }
.pv-sum .warn { color: var(--warning); }

.pv-table-wrap { border: 1px solid var(--border); border-radius: 5px; overflow: auto; max-height: 380px; background: #fff; }
.pv-table { width: 100%; border-collapse: collapse; font-size: 12.5px; }
.pv-table thead { position: sticky; top: 0; z-index: 1; }
.pv-table th { background: #f9fafb; text-align: left; padding: 8px 10px; border-bottom: 1px solid var(--border); color: var(--text-3); font: 600 10.5px var(--body); text-transform: uppercase; letter-spacing: .5px; }
.pv-table td { padding: 9px 10px; border-bottom: 1px solid var(--border-soft); vertical-align: middle; }
.pv-table tr:last-child td { border-bottom: none; }
.pv-table tr:hover td { background: #fafbfc; }
.pv-table tr.is-skip .pv-mod, .pv-table tr.is-new .pv-mod { color: var(--text-3); }
.pv-mod { color: var(--text); font-size: 12.5px; font-weight: 500; font-family: var(--mono); }
.pv-path { font-family: var(--mono); font-size: 10.5px; color: var(--text-3); word-break: break-all; }
.pv-chg { font-family: var(--mono); font-size: 11.5px; display: inline-flex; gap: 6px; align-items: center; }
.from { color: var(--text-3); text-decoration: line-through; }
.arr { color: var(--text-3); }
.to { color: var(--success); font-weight: 600; }
.tag-curr { font-family: var(--mono); font-size: 11.5px; color: var(--text-2); background: #f3f4f6; padding: 2px 8px; border-radius: 3px; }
.tag-new { font-family: var(--mono); font-size: 11.5px; color: #059669; background: #ecfdf5; padding: 2px 8px; border-radius: 3px; font-weight: 600; }
.row-del-btn {
  background: transparent; border: 1px solid transparent;
  color: var(--text-3); font-size: 13px; line-height: 1;
  padding: 3px 7px; border-radius: 4px; cursor: pointer; transition: all .12s;
}
.row-del-btn:hover { color: var(--danger); border-color: var(--danger); background: #fef2f2; }
.mute-text { color: var(--text-3); font-size: 11.5px; }
.warn-text { color: var(--warning); font-size: 11.5px; }

/* 模式切换按钮 */
.mode-toggle { display: flex; gap: 0; border: 1px solid var(--border); border-radius: 5px; overflow: hidden; }
.mt-btn { padding: 5px 14px; font-size: 12px; background: #fff; color: var(--text-2); border: none; cursor: pointer; transition: all .12s; }
.mt-btn:hover { color: var(--primary); }
.mt-btn.on { background: var(--primary); color: #fff; }
.mt-btn + .mt-btn { border-left: 1px solid var(--border); }

/* 选择模式 */
.select-mode { padding: 16px 20px; }
.sm-toolbar { display: flex; justify-content: space-between; align-items: center; margin-bottom: 14px; flex-wrap: wrap; gap: 10px; }
.sm-tip { font-size: 11.5px; color: var(--text-3); }
.sm-cache { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.sm-row { margin-bottom: 12px; }
.sm-row label { display: block; font-size: 11.5px; color: var(--text-2); margin-bottom: 6px; font-weight: 500; }
.sm-table-wrap { border: 1px solid var(--border); border-radius: 5px; overflow: auto; max-height: 320px; background: #fff; margin-bottom: 12px; }
.sm-table { width: 100%; border-collapse: collapse; font-size: 12.5px; }
.sm-table thead { position: sticky; top: 0; z-index: 1; }
.sm-table th { background: #f9fafb; text-align: left; padding: 8px 10px; border-bottom: 1px solid var(--border); color: var(--text-3); font: 600 10.5px var(--body); text-transform: uppercase; letter-spacing: .5px; }
.sm-table td { padding: 8px 10px; border-bottom: 1px solid var(--border-soft); vertical-align: middle; }
.sm-table tr:last-child td { border-bottom: none; }
.sm-actions { display: flex; align-items: center; gap: 12px; }
.btn.sm { padding: 4px 10px; font-size: 11.5px; }
.btn.danger-hover:hover:not(:disabled) { color: var(--danger); border-color: var(--danger); background: #fef2f2; }

/* tag 下拉底部「加载更早」按钮 */
.load-more-btn {
  display: block; width: 100%;
  padding: 7px 12px; font-size: 12px;
  background: #fafbfc; color: var(--primary); border: none;
  border-top: 1px solid var(--border-soft); cursor: pointer; transition: all .12s;
}
.load-more-btn:hover:not(:disabled) { background: #eff6ff; color: #0f7ce6; }
.load-more-btn:disabled { color: var(--text-3); cursor: not-allowed; background: #f9fafb; }

/* 通用预览区 */
.preview-block { padding: 14px 20px; border-top: 1px solid var(--border-soft); }
.preview-block .ws-sub { margin-bottom: 8px; }

/* 预检结果 */
.precheck-block { padding: 14px 20px; border-top: 1px solid var(--border-soft); background: #fafbfc; }
.pc-head { display: flex; justify-content: space-between; align-items: center; margin-bottom: 12px; flex-wrap: wrap; gap: 10px; }
.pc-title { font-size: 13px; font-weight: 600; color: var(--text); margin-right: 14px; }
.pc-stat { display: inline-block; padding: 2px 9px; border-radius: 99px; font-size: 11px; font-family: var(--mono); font-weight: 600; margin-right: 6px; }
.pc-stat.ok { background: #ecfdf5; color: #059669; }
.pc-stat.fail { background: #fef2f2; color: #dc2626; }
.pc-cache { display: flex; align-items: center; gap: 10px; }

.pc-group { background: #fff; border: 1px solid var(--border); border-radius: 5px; margin-bottom: 10px; overflow: hidden; }
.pc-group-head { padding: 9px 12px; font-size: 12px; font-weight: 600; color: var(--text); border-bottom: 1px solid var(--border-soft); display: flex; align-items: center; gap: 8px; }
.fail-group .pc-group-head { background: #fef2f2; color: #b91c1c; }
.pc-group summary { padding: 9px 12px; font-size: 12px; font-weight: 600; cursor: pointer; user-select: none; display: flex; align-items: center; gap: 8px; background: #ecfdf5; color: #059669; list-style: none; }
.pc-group summary::-webkit-details-marker { display: none; }
.pc-group summary .chev { margin-left: auto; font-size: 9px; color: var(--text-3); }
.pc-pass-body { padding: 0; }
.pc-filter-row { display: flex; gap: 8px; padding: 8px 12px; border-bottom: 1px solid var(--border-soft); }
.filter-inp { flex: 1; padding: 5px 9px; border: 1px solid var(--border); border-radius: 4px; font-size: 12px; }
.filter-inp:focus { outline: none; border-color: var(--primary); }

.pc-list { list-style: none; padding: 0; margin: 0; max-height: 320px; overflow-y: auto; }
.pc-list li { padding: 8px 12px; border-bottom: 1px solid var(--border-soft); font-size: 12.5px; }
.pc-list li:last-child { border-bottom: none; }
.pc-mod { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.pc-mod b { color: var(--text); font-family: var(--mono); }
.pc-reason { font-size: 11.5px; color: #b45309; margin-top: 4px; }
.fail-group .pc-reason { color: #dc2626; }

.mono { font-family: var(--mono); font-size: 11.5px; }

.exec {
  border-top: 1px solid var(--border-soft);
  background: #f0fdf4; padding: 14px 22px;
  display: flex; justify-content: space-between; align-items: center;
  border-bottom-left-radius: var(--radius); border-bottom-right-radius: var(--radius);
}
.exec-info { font-size: 12.5px; color: #166534; }
.extra-at { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.ea-label { color: #606266; font-size: 12.5px; white-space: nowrap; }
.ea-fixed { color: #909399; font-size: 12px; }
.exec-info b { color: #14532d; font-family: var(--mono); font-weight: 600; }

.cta {
  background: var(--success); color: #fff; border: none;
  padding: 10px 22px; border-radius: 5px;
  font: 600 13.5px var(--body); cursor: pointer;
  display: flex; gap: 8px; align-items: center;
}
.cta:hover:not(:disabled) { background: var(--success-dark); }
.cta:disabled { opacity: .4; cursor: not-allowed; }
.cta.danger { background: var(--danger); }
.cta.danger:hover:not(:disabled) { background: var(--danger-dark); }
.cta.warn { background: #f59e0b; }
.cta.warn:hover:not(:disabled) { background: #d97706; }

.no-perm-hint {
  font-size: 12.5px; color: var(--warning);
  padding: 10px 16px; border: 1px dashed var(--warning);
  border-radius: 5px;
}
.cta .el-icon { font-size: 14px; }
</style>
