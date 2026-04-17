<template>
  <transition name="toast-fade">
    <div v-if="visible" class="toast" :class="`toast-${type}`">{{ message }}</div>
  </transition>
</template>

<script setup>
import { ref } from 'vue'

const visible = ref(false)
const message = ref('')
const type = ref('info')
let timer = null

defineExpose({
  show(msg, t = 'info', duration = 3000) {
    message.value = msg
    type.value = t
    visible.value = true
    clearTimeout(timer)
    timer = setTimeout(() => { visible.value = false }, duration)
  }
})
</script>

<style scoped>
.toast {
  position: fixed;
  top: 20px;
  left: 50%;
  transform: translateX(-50%);
  padding: 10px 20px;
  border-radius: 6px;
  color: white;
  font-size: 14px;
  z-index: 9999;
  box-shadow: 0 4px 12px rgba(0,0,0,0.15);
}
.toast-info { background: #3b82f6; }
.toast-success { background: #10b981; }
.toast-error { background: #ef4444; }
.toast-fade-enter-from, .toast-fade-leave-to { opacity: 0; transform: translateX(-50%) translateY(-10px); }
.toast-fade-enter-active, .toast-fade-leave-active { transition: all .25s; }
</style>
