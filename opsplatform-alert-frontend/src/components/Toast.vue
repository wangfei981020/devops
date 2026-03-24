<template>
  <Teleport to="body">
    <Transition name="toast-fade">
      <div v-if="visible" class="toast-container" :class="'toast-' + type">
        <div class="toast-icon">
          <CheckCircle v-if="type === 'success'" :size="20" />
          <AlertCircle v-else-if="type === 'error'" :size="20" />
          <AlertTriangle v-else-if="type === 'warning'" :size="20" />
          <Info v-else :size="20" />
        </div>
        <div class="toast-message">{{ message }}</div>
        <button class="toast-close" @click="close">
          <X :size="16" />
        </button>
      </div>
    </Transition>
  </Teleport>
</template>

<script setup>
import { ref } from 'vue'
import { CheckCircle, AlertCircle, AlertTriangle, Info, X } from 'lucide-vue-next'

const visible = ref(false)
const message = ref('')
const type = ref('info')
let timer = null

function show(msg, t = 'info', duration = 3000) {
  message.value = msg
  type.value = t
  visible.value = true
  clearTimeout(timer)
  if (duration > 0) {
    timer = setTimeout(() => { visible.value = false }, duration)
  }
}

function close() {
  visible.value = false
}

defineExpose({ show, close })
</script>

<style scoped>
.toast-container {
  position: fixed;
  top: 20px;
  left: 50%;
  transform: translateX(-50%);
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 12px 20px;
  border-radius: 8px;
  box-shadow: 0 8px 30px rgba(0, 0, 0, 0.15);
  z-index: 9999;
  font-size: 14px;
  min-width: 280px;
  max-width: 500px;
}
.toast-success { background: #ecfdf5; color: #065f46; border: 1px solid #a7f3d0; }
.toast-error { background: #fef2f2; color: #991b1b; border: 1px solid #fecaca; }
.toast-warning { background: #fffbeb; color: #92400e; border: 1px solid #fde68a; }
.toast-info { background: #eff6ff; color: #1e40af; border: 1px solid #bfdbfe; }
.toast-icon { display: flex; flex-shrink: 0; }
.toast-message { flex: 1; }
.toast-close {
  background: none; border: none; cursor: pointer; opacity: 0.5; padding: 2px;
  color: inherit; display: flex;
}
.toast-close:hover { opacity: 1; }
.toast-fade-enter-active, .toast-fade-leave-active { transition: all 0.3s ease; }
.toast-fade-enter-from, .toast-fade-leave-to { opacity: 0; transform: translateX(-50%) translateY(-20px); }
</style>
