<template>
  <div class="login-root">
    <div class="login-card">
      <div class="login-brand">
        <div class="brand-icon">
          <svg width="28" height="28" viewBox="0 0 24 24" fill="none" stroke="#fff" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="m12 15-3-3a22 22 0 0 1 2-3.95A12.88 12.88 0 0 1 22 2c0 2.72-.78 7.5-6 11a22.35 22.35 0 0 1-4 2z"/><path d="M9 12H4s.55-3.03 2-4c1.62-1.08 5 0 5 0"/><path d="M12 15v5s3.03-.55 4-2c1.08-1.62 0-5 0-5"/></svg>
        </div>
        <div>
          <div class="brand-title">Deploy Center</div>
          <div class="brand-sub">GitOps 发布控制台</div>
        </div>
      </div>

      <el-form :model="form" class="login-form" label-position="top" @submit.prevent="onLogin">
        <el-form-item label="用户名">
          <el-input v-model="form.username" placeholder="请输入用户名" size="large" autofocus />
        </el-form-item>
        <el-form-item label="密码">
          <el-input v-model="form.password" type="password" show-password placeholder="请输入密码" size="large" @keydown.enter="onLogin" />
        </el-form-item>
        <el-button type="primary" size="large" :loading="loading" @click="onLogin" style="width:100%">登录</el-button>
      </el-form>

      <div class="login-foot">
        <div>通过运维平台跳转可自动登录</div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { useAuthStore } from '../stores/auth'

const router = useRouter()
const auth = useAuthStore()
const form = reactive({ username: '', password: '' })
const loading = ref(false)

async function onLogin() {
  if (!form.username.trim() || !form.password) {
    ElMessage.warning('请输入用户名和密码')
    return
  }
  loading.value = true
  try {
    await auth.login(form.username.trim(), form.password)
    ElMessage.success('登录成功')
    router.replace('/')
  } catch (_) {
    /* 拦截器已经 toast 过 */
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.login-root {
  min-height: 100vh;
  display: flex; align-items: center; justify-content: center;
  background: linear-gradient(135deg, #1e293b 0%, #0f172a 100%);
  padding: 20px;
}
.login-card {
  width: 420px; background: #fff; border-radius: 12px;
  padding: 36px 36px 28px; box-shadow: 0 20px 60px rgba(0, 0, 0, 0.3);
}
.login-brand {
  display: flex; align-items: center; gap: 14px; margin-bottom: 28px;
}
.brand-icon {
  width: 48px; height: 48px; border-radius: 10px;
  background: linear-gradient(135deg, #1890ff, #36cfc9);
  display: flex; align-items: center; justify-content: center;
}
.brand-title { font: 700 20px/1.2 var(--body, sans-serif); color: #0f172a; }
.brand-sub { font: 500 12px var(--mono, monospace); color: #64748b; margin-top: 2px; }

.login-form { margin-bottom: 18px; }
.login-form :deep(.el-form-item__label) { font-size: 12.5px; color: #475569; padding-bottom: 4px; }

.login-foot {
  text-align: center; font-size: 11.5px; color: #94a3b8;
  padding-top: 16px; border-top: 1px solid #f1f5f9;
}
.login-foot code {
  font-family: var(--mono, monospace); font-size: 11.5px;
  background: #f1f5f9; padding: 1px 6px; border-radius: 3px; color: #334155;
}
</style>
