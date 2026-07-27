<script setup>
import { ref, onMounted, onUnmounted } from 'vue'
import { useRouter } from 'vue-router'
import { useChatStore } from './stores/chat'
import http from './api/http'

const router = useRouter()
const chat = useChatStore()
const st = ref({ model: '—', mcp_ok: false, tools: 0, has_key: false, mcp_error: '' })
let timer = null

async function loadStatus() {
  try {
    const { data } = await http.get('/status')
    st.value = data
  } catch (e) {
    st.value = { ...st.value, backend_down: true }
  }
}
function newChat() {
  chat.newSession()
  router.push('/')
}
onMounted(() => {
  loadStatus()
  timer = setInterval(loadStatus, 15000)
})
onUnmounted(() => clearInterval(timer))
</script>

<template>
  <div class="app">
    <aside class="side">
      <div class="brand">
        <span class="logo">运</span>
        <div>
          <div class="t">运维 AI 助手</div>
          <div class="s">调试版 · 无 SSO</div>
        </div>
      </div>
      <button class="newbtn" @click="newChat">+ 新建对话</button>

      <div class="sec">已接入系统 (MCP)</div>
      <div class="mcp">
        <div v-if="!(st.systems && st.systems.length)" class="row empty">暂无接入</div>
        <div v-for="sys in st.systems" :key="sys.name" class="row">
          <span class="dot" :class="sys.ok ? 'up' : 'off'"></span>
          <span :class="{ dim: !sys.enabled }">{{ sys.name }}</span>
          <span class="cnt">{{ sys.ok ? sys.tools + ' 工具' : (sys.enabled ? '未连' : '停用') }}</span>
        </div>
      </div>

      <div class="navlinks">
        <router-link to="/" class="nav" active-class="on" exact>对话</router-link>
        <router-link to="/settings" class="nav" active-class="on">设置</router-link>
      </div>

      <div class="foot">
        <div v-if="st.backend_down" class="warn">后端未响应</div>
        <template v-else>
          <div v-if="!st.has_key" class="warn">⚠ 未配置 API Key/Token</div>
          <div v-else class="okline">● 凭证已配置</div>
          <div>{{ st.mcp_ok ? 'MCP 已连' : 'MCP: ' + (st.mcp_error || '未连') }}</div>
          <div class="mdl">{{ st.model }}</div>
        </template>
      </div>
    </aside>

    <main class="main">
      <router-view />
    </main>
  </div>
</template>

<style scoped>
.app { display: grid; grid-template-columns: 240px 1fr; height: 100vh; }
.side {
  background: #fbfaf7; border-right: 1px solid var(--line);
  display: flex; flex-direction: column; padding: 14px 12px; gap: 14px;
}
.brand { display: flex; align-items: center; gap: 9px; }
.logo {
  width: 30px; height: 30px; border-radius: 9px;
  background: linear-gradient(135deg, var(--accent), #0f766e);
  display: grid; place-items: center; color: #fff; font-weight: 700;
}
.brand .t { font-weight: 700; font-size: 15px; }
.brand .s { font-size: 10px; color: var(--ink3); }
.newbtn {
  padding: 9px; border: 0; border-radius: 9px; background: var(--accent);
  color: #fff; font-weight: 600; cursor: pointer; font-size: 14px;
}
.newbtn:hover { background: #0f766e; }
.sec {
  font-size: 10.5px; letter-spacing: .09em; text-transform: uppercase;
  color: var(--ink3); font-weight: 600; margin-top: 4px;
}
.mcp .row { display: flex; align-items: center; gap: 8px; font-size: 12.5px; color: var(--ink2); padding: 3px 4px; }
.cnt { margin-left: auto; color: var(--ink3); font-size: 11px; }
.row.empty { color: var(--ink3); font-style: italic; }
.dim { color: var(--ink3); }
.dot { width: 7px; height: 7px; border-radius: 50%; }
.dot.up { background: var(--ok); box-shadow: 0 0 0 3px #e4f3ea; }
.dot.off { background: var(--bad); box-shadow: 0 0 0 3px var(--bad-soft); }
.navlinks { display: flex; flex-direction: column; gap: 2px; margin-top: 4px; }
.nav { padding: 8px 10px; border-radius: 8px; font-size: 13px; color: var(--ink2); cursor: pointer; }
.nav:hover { background: #f1ece3; }
.nav.on { background: var(--accent-soft); color: #0f766e; font-weight: 600; }
.foot {
  margin-top: auto; border-top: 1px solid var(--line); padding-top: 10px;
  font-size: 11px; color: var(--ink3); display: flex; flex-direction: column; gap: 3px;
}
.warn { color: var(--bad); }
.okline { color: var(--ok); }
.mdl { font-family: var(--mono); }
.main { display: flex; flex-direction: column; min-width: 0; }
</style>
