<script setup>
import { useAppStore } from '@/stores/app'

const appStore = useAppStore()
</script>

<template>
  <router-view />

  <Transition name="toast">
    <div v-if="appStore.showToastFlag" class="toast" :class="'toast-' + appStore.toastType">
      <span>{{ appStore.toastMessage }}</span>
    </div>
  </Transition>
</template>

<style>
.toast {
  position: fixed;
  top: 24px;
  right: 24px;
  padding: 14px 24px;
  background: #1f2937;
  border: 1px solid rgba(255,255,255,0.1);
  border-radius: 10px;
  box-shadow: 0 10px 40px rgba(0,0,0,0.4);
  z-index: 99999;
  font-size: 14px;
  color: #f9fafb;
  max-width: 400px;
}
.toast-success { border-left: 4px solid #10b981; }
.toast-error { border-left: 4px solid #ef4444; }
.toast-warning { border-left: 4px solid #f59e0b; }
.toast-info { border-left: 4px solid #06b6d4; }
.toast-enter-active, .toast-leave-active { transition: all 0.3s ease; }
.toast-enter-from { opacity: 0; transform: translateX(100px); }
.toast-leave-to { opacity: 0; transform: translateY(-20px); }
</style>
