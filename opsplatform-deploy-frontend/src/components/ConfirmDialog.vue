<template>
  <div v-if="visible" class="dialog-mask" @click.self="onCancel">
    <div class="dialog">
      <div class="dialog-title">{{ opts.title || '提示' }}</div>
      <div class="dialog-content">{{ opts.message || '确认?' }}</div>
      <div class="dialog-actions">
        <button class="btn btn-outline" @click="onCancel">{{ opts.cancelText || '取消' }}</button>
        <button class="btn" :class="opts.danger ? 'btn-danger' : 'btn-primary'" @click="onConfirm">{{ opts.confirmText || '确认' }}</button>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref } from 'vue'

const visible = ref(false)
const opts = ref({})
let resolver = null

defineExpose({
  show(options, resolve) {
    opts.value = options || {}
    resolver = resolve
    visible.value = true
  }
})

function onConfirm() { visible.value = false; if (resolver) resolver(true) }
function onCancel() { visible.value = false; if (resolver) resolver(false) }
</script>

<style scoped>
.dialog-mask {
  position: fixed; inset: 0;
  background: rgba(0,0,0,0.4);
  z-index: 9000;
  display: flex; align-items: center; justify-content: center;
}
.dialog {
  background: white;
  border-radius: 8px;
  min-width: 360px; max-width: 480px;
  box-shadow: 0 10px 40px rgba(0,0,0,0.2);
  overflow: hidden;
}
.dialog-title { padding: 16px 20px; font-weight: 600; font-size: 16px; border-bottom: 1px solid #e5e7eb; }
.dialog-content { padding: 20px; color: #374151; line-height: 1.6; }
.dialog-actions { padding: 12px 20px; display: flex; gap: 8px; justify-content: flex-end; background: #f9fafb; }
</style>
