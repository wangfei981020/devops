<template>
  <div :data-theme="theme">
    <router-view />
    <div v-if="toast.show" :class="['toast', toast.type]">{{ toast.message }}</div>

    <!-- 全局确认弹窗 -->
    <Teleport to="body">
      <Transition name="confirm-fade">
        <div v-if="confirmState.visible" class="confirm-overlay" @click.self="handleConfirmCancel">
          <Transition name="confirm-pop">
            <div v-if="confirmState.visible" class="confirm-dialog">
              <div class="confirm-icon" :class="confirmState.type">
                <svg v-if="confirmState.type === 'warning'" width="28" height="28" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                  <path d="m21.73 18-8-14a2 2 0 0 0-3.48 0l-8 14A2 2 0 0 0 4 21h16a2 2 0 0 0 1.73-3Z"/>
                  <line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/>
                </svg>
                <svg v-else-if="confirmState.type === 'danger'" width="28" height="28" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                  <circle cx="12" cy="12" r="10"/><line x1="15" y1="9" x2="9" y2="15"/><line x1="9" y1="9" x2="15" y2="15"/>
                </svg>
                <svg v-else width="28" height="28" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                  <circle cx="12" cy="12" r="10"/><path d="M12 16v-4"/><path d="M12 8h.01"/>
                </svg>
              </div>
              <h3 class="confirm-title">{{ confirmState.title }}</h3>
              <p class="confirm-message">{{ confirmState.message }}</p>
              <div class="confirm-actions">
                <button class="confirm-btn confirm-btn-cancel" @click="handleConfirmCancel">取消</button>
                <button class="confirm-btn confirm-btn-ok" :class="'confirm-btn-' + confirmState.type" @click="handleConfirmOk">
                  {{ confirmState.okText || '确定' }}
                </button>
              </div>
            </div>
          </Transition>
        </div>
      </Transition>
    </Teleport>
  </div>
</template>

<script setup>
import { ref, onMounted, provide } from 'vue'

const theme = ref(localStorage.getItem('theme') || 'dark')
const toast = ref({ show: false, message: '', type: 'info' })

const confirmState = ref({
  visible: false,
  title: '',
  message: '',
  type: 'info',
  okText: '确定',
  resolve: null
})

function setTheme(t) {
  theme.value = t
  localStorage.setItem('theme', t)
}

function toggleTheme() {
  setTheme(theme.value === 'dark' ? 'light' : 'dark')
}

function showToast(message, type = 'info', duration = 3000) {
  toast.value = { show: true, message, type }
  setTimeout(() => { toast.value.show = false }, duration)
}

function showConfirm(options = {}) {
  return new Promise((resolve) => {
    confirmState.value = {
      visible: true,
      title: options.title || '确认操作',
      message: options.message || '确定要执行此操作吗？',
      type: options.type || 'info',
      okText: options.okText || '确定',
      resolve
    }
  })
}

function handleConfirmOk() {
  confirmState.value.resolve?.(true)
  confirmState.value.visible = false
}

function handleConfirmCancel() {
  confirmState.value.resolve?.(false)
  confirmState.value.visible = false
}

onMounted(() => {
  document.documentElement.setAttribute('data-theme', theme.value)
})

provide('theme', { current: theme, toggle: toggleTheme, set: setTheme })
provide('toast', showToast)
provide('confirm', showConfirm)
</script>

<style>
.confirm-overlay {
  position: fixed;
  inset: 0;
  z-index: 10000;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(0, 0, 0, 0.6);
  backdrop-filter: blur(4px);
}

.confirm-dialog {
  background: var(--bg-card, #1E293B);
  border: 1px solid var(--border, #334155);
  border-radius: var(--radius-lg, 12px);
  padding: 28px 32px 24px;
  width: 400px;
  max-width: 90vw;
  text-align: center;
  box-shadow: 0 20px 60px rgba(0, 0, 0, 0.4);
}

.confirm-icon {
  width: 52px;
  height: 52px;
  border-radius: 50%;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  margin-bottom: 16px;
}
.confirm-icon.info { background: rgba(76, 154, 255, 0.12); color: #4C9AFF; }
.confirm-icon.warning { background: rgba(245, 158, 11, 0.12); color: #F59E0B; }
.confirm-icon.danger { background: rgba(239, 68, 68, 0.12); color: #EF4444; }

.confirm-title { font-size: 17px; font-weight: 600; color: var(--text-primary, #F1F5F9); margin-bottom: 8px; }
.confirm-message { font-size: 14px; color: var(--text-secondary, #94A3B8); line-height: 1.6; margin-bottom: 24px; }
.confirm-actions { display: flex; gap: 12px; justify-content: center; }

.confirm-btn {
  padding: 9px 28px;
  border-radius: var(--radius, 8px);
  font-size: 14px;
  font-weight: 500;
  font-family: var(--font-sans);
  cursor: pointer;
  transition: all 200ms ease;
  border: 1px solid transparent;
}
.confirm-btn-cancel { background: var(--bg-tertiary, #334155); color: var(--text-secondary, #94A3B8); border-color: var(--border, #334155); }
.confirm-btn-cancel:hover { background: var(--bg-hover, #475569); color: var(--text-primary, #F1F5F9); }
.confirm-btn-ok { color: #fff; }
.confirm-btn-ok.confirm-btn-info { background: #4C9AFF; border-color: #4C9AFF; }
.confirm-btn-ok.confirm-btn-info:hover { background: #2684FF; }
.confirm-btn-ok.confirm-btn-warning { background: #F59E0B; border-color: #F59E0B; }
.confirm-btn-ok.confirm-btn-warning:hover { background: #D97706; }
.confirm-btn-ok.confirm-btn-danger { background: #EF4444; border-color: #EF4444; }
.confirm-btn-ok.confirm-btn-danger:hover { background: #DC2626; }

.confirm-fade-enter-active, .confirm-fade-leave-active { transition: opacity 200ms ease; }
.confirm-fade-enter-from, .confirm-fade-leave-to { opacity: 0; }
.confirm-pop-enter-active { transition: all 250ms cubic-bezier(0.34, 1.56, 0.64, 1); }
.confirm-pop-leave-active { transition: all 150ms ease; }
.confirm-pop-enter-from { opacity: 0; transform: scale(0.85); }
.confirm-pop-leave-to { opacity: 0; transform: scale(0.95); }
</style>
