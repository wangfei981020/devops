<template>
  <div :class="embedded ? '' : 'page'">
    <div v-if="!embedded" class="page-head">
      <span class="page-title">AI 接入 (MCP)</span>
      <span class="muted" style="margin-left:10px">把 CMDB 只读能力暴露成 MCP 工具，供你的 AI 界面 / Claude Code 连接查询</span>
    </div>
    <div v-else class="muted" style="margin-bottom:12px">
      把 CMDB 只读能力暴露成 MCP 工具，供你的 AI 界面 / Claude Code 连接查询
    </div>

    <LoadError :error="error" title="MCP 接入信息未加载" @retry="load" />

    <el-card shadow="never" style="margin-bottom:14px" :body-style="embedded ? {padding:'14px'} : {}">
      <template #header><b>连接信息</b></template>
      <el-form label-width="120px" style="max-width:900px">
        <el-form-item label="MCP 端点">
          <el-input :model-value="endpoint" readonly>
            <template #append><el-button @click="copy(endpoint)">复制</el-button></template>
          </el-input>
          <div class="muted">生产替换成对外域名，如 https://opsplatform-cmdb.slileisure.com/api/mcp</div>
        </el-form-item>
        <el-form-item label="访问 Token">
          <!-- 这里**不能**用 el-input 的 show-password：那个属性是"可切换显示"，
               点一下眼睛就变明文，截图就泄露。这个 token 是全部只读工具的通行证，
               所以做成永远只显示打码值，真实值只能走「复制」进剪贴板（CMDB-027）。 -->
          <el-input :model-value="tokenMasked" readonly>
            <template #append><el-button :disabled="!info.token" @click="copy(info.token)">复制</el-button></template>
          </el-input>
          <el-button v-if="canMcp" size="small" type="warning" plain style="margin-top:8px" @click="regen">重新生成 Token</el-button>
          <span class="muted" style="margin-left:8px">
            Token 不显示明文，只能复制；重新生成后旧 token 立即失效
          </span>
        </el-form-item>
        <el-form-item label="工具数">
          <el-tag type="success">{{ info.tools }} 个只读工具</el-tag>
          <span class="muted" style="margin-left:8px">传输 http-jsonrpc · 只读（写操作二期带确认）</span>
        </el-form-item>
      </el-form>
    </el-card>

    <el-card shadow="never" style="margin-bottom:14px">
      <template #header>
        <b>Claude Code 接入示例</b>
        <el-button size="small" style="float:right" @click="copy(cmdFull)">复制完整命令</el-button>
      </template>
      <!-- 示例里的 token 一律打码：这个页面经常被截图分享，明文 token 一截就泄露。
           要真正的值走「复制完整命令」，剪贴板不会进截图（CMDB-027） -->
      <pre class="cfg">claude mcp add --transport http cmdb {{ endpoint }} \
  --header "Authorization: Bearer {{ tokenMasked }}"</pre>
      <div class="muted">
        Token 已打码，点右上「复制完整命令」拿真实值。
        或在 AI 界面里配置：HTTP MCP，URL = 上面端点，Header 带 Authorization: Bearer &lt;token&gt;
      </div>
    </el-card>

    <el-card shadow="never">
      <template #header><b>能问什么（示例）</b></template>
      <ul class="ex">
        <li>"opsplatform 有哪些域名，哪些证书 30 天内到期"</li>
        <li>"g32-uat 集群哪些节点卡死 / 哪些 Pod 副本不足"</li>
        <li>"某个 Pod 为什么一直重启" → AI 调 diagnose_pod / pod_logs 给根因+方案</li>
        <li>"api.slileisure.com 跑在哪些 Pod/节点上"（全链路）</li>
        <li>"这个节点下线会影响哪些域名"（反向影响）</li>
        <li>"本月云成本多少，比上月贵在哪"（成本 + 环比归因）</li>
      </ul>
    </el-card>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useAuthStore } from '../stores/auth'
import { ElMessage } from 'element-plus'
import { useLoadState } from '../composables/useLoadState'
import LoadError from '../components/LoadError.vue'
import { mcpInfo, mcpRegenerate } from '../api/cmdb'
import { useAppStore } from '../stores/app'

defineProps({ embedded: { type: Boolean, default: false } })

// MCP Token 是机器身份、不受 RBAC 约束，拿到即可读全量数据，所以单独成码
const auth = useAuthStore()
const canMcp = computed(() => auth.hasButton('manage_mcp'))
const app = useAppStore()
const { loading, error, run } = useLoadState()
const info = ref({ token: '', tools: 0 })
const endpoint = computed(() => location.origin + '/api/mcp')

// 只露首尾各 4 位，中间固定长度的星号——不按真实长度打码，免得泄露 token 位数
const tokenMasked = computed(() => {
  const t = info.value.token
  if (!t) return '<token>'
  return t.length <= 12 ? '••••••••' : `${t.slice(0, 4)}••••••••••••${t.slice(-4)}`
})

// 复制用的是真实 token：剪贴板不进截图，这是「能用」与「不泄露」的平衡点
const cmdFull = computed(() =>
  `claude mcp add --transport http cmdb ${endpoint.value} \\\n  --header "Authorization: Bearer ${info.value.token || '<token>'}"`)

// 同上：拿不到接入信息要说出来，不能显示成一个空面板（CMDB-013）
async function load() { await run(async () => { info.value = await mcpInfo() }) }
async function regen() {
  try { await app.showConfirm('重新生成 MCP Token？旧 token 立即失效，已配置的 AI 客户端要更新。'); const r = await mcpRegenerate(); info.value.token = r.token; ElMessage.success('已重新生成') }
  catch (e) { if (e !== 'cancel') ElMessage.error('失败') }
}
function copy(t) { navigator.clipboard?.writeText(t); ElMessage.success('已复制') }
onMounted(load)
</script>

<style scoped>
.page-head { margin-bottom: 14px; }
.page-title { font-size: 18px; font-weight: 600; }
.muted { color: #909399; font-size: 12px; }
.cfg { background: #1e1e1e; color: #d4d4d4; padding: 12px; border-radius: 6px; font-size: 12px; overflow-x: auto; }
.ex { margin: 0; padding-left: 18px; font-size: 13px; color: #606266; line-height: 1.9; }
</style>
