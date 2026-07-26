<template>
  <div class="page">
    <div class="page-head">
      <span class="page-title">AI 接入 (MCP)</span>
      <span class="muted" style="margin-left:10px">把 CMDB 只读能力暴露成 MCP 工具，供你的 AI 界面 / Claude Code 连接查询</span>
    </div>

    <el-card shadow="never" style="margin-bottom:14px">
      <template #header><b>连接信息</b></template>
      <el-form label-width="120px" style="max-width:900px">
        <el-form-item label="MCP 端点">
          <el-input :model-value="endpoint" readonly>
            <template #append><el-button @click="copy(endpoint)">复制</el-button></template>
          </el-input>
          <div class="muted">生产替换成对外域名，如 https://opsplatform-cmdb.slileisure.com/api/mcp</div>
        </el-form-item>
        <el-form-item label="访问 Token">
          <el-input v-model="info.token" readonly show-password>
            <template #append><el-button @click="copy(info.token)">复制</el-button></template>
          </el-input>
          <el-button size="small" type="warning" plain style="margin-top:8px" @click="regen">重新生成 Token</el-button>
          <span class="muted" style="margin-left:8px">重新生成后旧 token 立即失效</span>
        </el-form-item>
        <el-form-item label="工具数">
          <el-tag type="success">{{ info.tools }} 个只读工具</el-tag>
          <span class="muted" style="margin-left:8px">传输 http-jsonrpc · 只读（写操作二期带确认）</span>
        </el-form-item>
      </el-form>
    </el-card>

    <el-card shadow="never" style="margin-bottom:14px">
      <template #header><b>Claude Code 接入示例</b></template>
      <pre class="cfg">claude mcp add --transport http cmdb {{ endpoint }} \
  --header "Authorization: Bearer {{ info.token || '&lt;token&gt;' }}"</pre>
      <div class="muted">或在 AI 界面里配置：HTTP MCP，URL = 上面端点，Header 带 Authorization: Bearer &lt;token&gt;</div>
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
import { ElMessage } from 'element-plus'
import { mcpInfo, mcpRegenerate } from '../api/cmdb'
import { useAppStore } from '../stores/app'

const app = useAppStore()
const info = ref({ token: '', tools: 0 })
const endpoint = computed(() => location.origin + '/api/mcp')

async function load() { try { info.value = await mcpInfo() } catch (e) { ElMessage.error('加载失败') } }
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
