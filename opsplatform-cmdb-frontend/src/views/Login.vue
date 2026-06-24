<template>
  <div class="min-h-screen flex font-sans text-zinc-900 text-[14px]">
    <!-- 左：品牌区 -->
    <div class="hidden lg:flex w-[46%] bg-zinc-900 text-white flex-col justify-between p-12 relative overflow-hidden">
      <div class="absolute -top-24 -right-24 w-96 h-96 rounded-full bg-brand/20 blur-3xl"></div>
      <div class="absolute -bottom-32 -left-16 w-80 h-80 rounded-full bg-brand/10 blur-3xl"></div>

      <div class="relative flex items-center gap-2.5">
        <div class="w-8 h-8 rounded-lg bg-brand flex items-center justify-center"><el-icon :size="20" color="#fff"><Files /></el-icon></div>
        <span class="font-semibold text-lg tracking-tight">CMDB</span>
      </div>

      <div class="relative">
        <h1 class="text-3xl font-semibold tracking-tight leading-snug">运维配置管理库<br><span class="text-zinc-400 text-2xl font-normal">统一纳管资产与关系</span></h1>
        <p class="text-zinc-400 mt-4 leading-relaxed max-w-sm">把域名、证书、主机、应用等配置项（CI）与它们之间的关系集中管理，资产全景与依赖链路一图掌握。</p>
        <div class="mt-8 space-y-3 text-zinc-300">
          <div class="flex items-center gap-3"><div class="w-7 h-7 rounded-md bg-white/10 flex items-center justify-center"><el-icon><Coin /></el-icon></div>配置项（CI）建模，多类资产统一纳管</div>
          <div class="flex items-center gap-3"><div class="w-7 h-7 rounded-md bg-white/10 flex items-center justify-center"><el-icon><Share /></el-icon></div>资产关系图谱 + 依赖链路追溯</div>
          <div class="flex items-center gap-3"><div class="w-7 h-7 rounded-md bg-white/10 flex items-center justify-center"><el-icon><Aim /></el-icon></div>自动发现 + 证书自动签发续期</div>
          <div class="flex items-center gap-3"><div class="w-7 h-7 rounded-md bg-white/10 flex items-center justify-center"><el-icon><Bell /></el-icon></div>到期 / 变更 Lark 实时告警</div>
        </div>
      </div>

      <div class="relative text-[12px] text-zinc-500 mono">{{ version }} · © 2026 Ops Platform</div>
    </div>

    <!-- 右：登录表单 -->
    <div class="flex-1 flex items-center justify-center bg-zinc-50 px-6">
      <div class="w-full max-w-[360px]">
        <div class="lg:hidden flex items-center gap-2.5 mb-8 justify-center">
          <div class="w-8 h-8 rounded-lg bg-brand flex items-center justify-center"><el-icon :size="20" color="#fff"><Files /></el-icon></div>
          <span class="font-semibold text-lg">CMDB</span>
        </div>

        <h2 class="text-xl font-semibold tracking-tight">登录到 CMDB</h2>
        <p class="text-zinc-500 mt-1 text-[13px]">输入账号密码以继续</p>

        <form class="mt-7 space-y-4" @submit.prevent="doLogin">
          <div>
            <label class="block text-[13px] font-medium text-zinc-700 mb-1.5">用户名</label>
            <div class="relative">
              <el-icon class="absolute left-3 top-1/2 -translate-y-1/2 text-zinc-400"><User /></el-icon>
              <input v-model="username" class="w-full pl-9 pr-3 py-2.5 rounded-lg border border-zinc-200 bg-white focus:border-brand focus:ring-2 focus:ring-brand/15 outline-none transition text-[14px]">
            </div>
          </div>
          <div>
            <div class="flex items-center justify-between mb-1.5">
              <label class="block text-[13px] font-medium text-zinc-700">密码</label>
              <a class="text-[12px] text-brand hover:text-brand-hover cursor-pointer" @click="forgotTip">忘记密码？</a>
            </div>
            <div class="relative">
              <el-icon class="absolute left-3 top-1/2 -translate-y-1/2 text-zinc-400"><Lock /></el-icon>
              <input v-model="password" :type="showPwd ? 'text' : 'password'" @keyup.enter="doLogin"
                     class="w-full pl-9 pr-9 py-2.5 rounded-lg border border-zinc-200 bg-white focus:border-brand focus:ring-2 focus:ring-brand/15 outline-none transition text-[14px]">
              <el-icon class="absolute right-3 top-1/2 -translate-y-1/2 text-zinc-400 hover:text-zinc-600 cursor-pointer" @click="showPwd = !showPwd"><View v-if="showPwd" /><Hide v-else /></el-icon>
            </div>
          </div>
          <label class="flex items-center gap-2 text-[13px] text-zinc-600 cursor-pointer select-none" @click="remember = !remember">
            <span class="w-4 h-4 rounded border flex items-center justify-center transition-colors" :class="remember ? 'bg-brand border-brand' : 'border-zinc-300 bg-white'">
              <el-icon v-show="remember" :size="12" color="#fff"><Check /></el-icon>
            </span>
            7 天内免登录
          </label>
          <button type="submit" :disabled="loading"
                  class="w-full bg-brand hover:bg-brand-hover text-white py-2.5 rounded-lg font-medium cursor-pointer transition-colors flex items-center justify-center gap-2 disabled:opacity-60">
            <span>{{ loading ? '登录中…' : '登录' }}</span>
            <el-icon v-if="!loading"><Right /></el-icon>
          </button>
        </form>

        <p class="text-center text-[12px] text-zinc-400 mt-8">登录即代表同意平台使用规范</p>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { User, Lock, Files, Coin, Share, Aim, Bell, View, Hide, Right, Check } from '@element-plus/icons-vue'
import { useAuthStore } from '../stores/auth'

const router = useRouter()
const auth = useAuthStore()
const version = import.meta.env.VITE_APP_VERSION || 'dev'
const username = ref('admin')
const password = ref('')
const showPwd = ref(false)
const remember = ref(true)
const loading = ref(false)

function forgotTip() { ElMessage.info('请联系管理员重置密码') }

async function doLogin() {
  if (!username.value || !password.value) { ElMessage.warning('请输入用户名和密码'); return }
  loading.value = true
  try {
    await auth.login(username.value, password.value)
    router.replace('/')
  } catch (e) {
    ElMessage.error(e.response?.data?.error || '登录失败')
  } finally {
    loading.value = false
  }
}
</script>
