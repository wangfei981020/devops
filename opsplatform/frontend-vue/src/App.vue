<script setup>
import { onMounted, watch } from 'vue'
import { useAppStore, useAuthStore } from '@/stores'

const appStore = useAppStore()
const authStore = useAuthStore()

onMounted(() => {
  appStore.initTheme()
  
  if (authStore.token) {
    authStore.fetchUser()
  }
})

watch(() => appStore.theme, (newTheme) => {
  document.body.classList.toggle('light-mode', newTheme === 'light')
})
</script>

<template>
  <router-view />
  
  <Transition name="toast">
    <div 
      v-if="appStore.showToastFlag" 
      class="toast"
      :class="'toast-' + appStore.toastType"
    >
      <svg v-if="appStore.toastType === 'success'" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
        <path d="M22 11.08V12a10 10 0 1 1-5.93-9.14"></path>
        <polyline points="22 4 12 14.01 9 11.01"></polyline>
      </svg>
      <svg v-else-if="appStore.toastType === 'error'" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
        <circle cx="12" cy="12" r="10"></circle>
        <line x1="15" y1="9" x2="9" y2="15"></line>
        <line x1="9" y1="9" x2="15" y2="15"></line>
      </svg>
      <svg v-else-if="appStore.toastType === 'warning'" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
        <path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"></path>
        <line x1="12" y1="9" x2="12" y2="13"></line>
        <line x1="12" y1="17" x2="12.01" y2="17"></line>
      </svg>
      <svg v-else viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
        <circle cx="12" cy="12" r="10"></circle>
        <line x1="12" y1="16" x2="12" y2="12"></line>
        <line x1="12" y1="8" x2="12.01" y2="8"></line>
      </svg>
      <span>{{ appStore.toastMessage }}</span>
    </div>
  </Transition>
</template>

<style>
.toast {
  position: fixed;
  top: 24px;
  right: 24px;
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 16px 24px;
  background: #1f2937;
  border: 1px solid rgba(255, 255, 255, 0.1);
  border-radius: 12px;
  box-shadow: 0 20px 50px rgba(0, 0, 0, 0.5);
  z-index: 99999;
  font-size: 14px;
  font-weight: 500;
  color: #f9fafb;
  max-width: 420px;
  word-break: break-word;
  backdrop-filter: blur(10px);
}

.light-mode .toast {
  background: #ffffff;
  border: 1px solid #e2e8f0;
  color: #1e293b;
  box-shadow: 0 20px 50px rgba(0, 0, 0, 0.15);
}

.toast svg {
  width: 22px;
  height: 22px;
  flex-shrink: 0;
}

.toast-success {
  border-left: 4px solid #10b981;
  background: #1f2937;
}

.toast-success svg {
  color: #10b981;
}

.light-mode .toast-success {
  background: #ffffff;
  border-left: 4px solid #10b981;
}

.toast-error {
  border-left: 4px solid #ef4444;
  background: #1f2937;
}

.toast-error svg {
  color: #ef4444;
}

.light-mode .toast-error {
  background: #ffffff;
  border-left: 4px solid #ef4444;
}

.toast-warning {
  border-left: 4px solid #f59e0b;
  background: #1f2937;
}

.toast-warning svg {
  color: #f59e0b;
}

.light-mode .toast-warning {
  background: #ffffff;
  border-left: 4px solid #f59e0b;
}

.toast-info {
  border-left: 4px solid #06b6d4;
  background: #1f2937;
}

.toast-info svg {
  color: #06b6d4;
}

.light-mode .toast-info {
  background: #ffffff;
  border-left: 4px solid #06b6d4;
}

.toast-enter-active,
.toast-leave-active {
  transition: all 0.3s ease;
}

.toast-enter-from {
  opacity: 0;
  transform: translateX(100px);
}

.toast-leave-to {
  opacity: 0;
  transform: translateY(-20px);
}
</style>
