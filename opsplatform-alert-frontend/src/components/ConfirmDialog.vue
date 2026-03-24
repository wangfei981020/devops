<template>
  <Teleport to="body">
    <Transition name="modal-fade">
      <div v-if="visible" class="modal-overlay" @click.self="handleCancel">
        <div class="confirm-dialog">
          <div class="confirm-icon" :class="'icon-' + type">
            <AlertTriangle v-if="type === 'warning' || type === 'danger'" :size="28" />
            <Info v-else :size="28" />
          </div>
          <div class="confirm-title">{{ title }}</div>
          <div class="confirm-message">{{ message }}</div>
          <div v-if="showInput" class="confirm-input">
            <input v-model="inputValue" class="form-input" :placeholder="inputPlaceholder" ref="inputRef" @keyup.enter="handleConfirm" />
          </div>
          <div class="confirm-actions">
            <button class="btn btn-outline" @click="handleCancel">取消</button>
            <button class="btn" :class="type === 'danger' ? 'btn-danger' : 'btn-primary'" @click="handleConfirm">
              {{ confirmText }}
            </button>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<script setup>
import { ref, nextTick } from 'vue'
import { AlertTriangle, Info } from 'lucide-vue-next'

const visible = ref(false)
const title = ref('')
const message = ref('')
const type = ref('warning')
const confirmText = ref('确定')
const showInput = ref(false)
const inputValue = ref('')
const inputPlaceholder = ref('')
const inputRef = ref(null)

let resolvePromise = null

function confirm(opts) {
  title.value = opts.title || '确认'
  message.value = opts.message || ''
  type.value = opts.type || 'warning'
  confirmText.value = opts.confirmText || '确定'
  showInput.value = false
  visible.value = true
  return new Promise(resolve => { resolvePromise = resolve })
}

function prompt(opts) {
  title.value = opts.title || '请输入'
  message.value = opts.message || ''
  type.value = opts.type || 'info'
  confirmText.value = opts.confirmText || '确定'
  showInput.value = true
  inputValue.value = opts.defaultValue || ''
  inputPlaceholder.value = opts.placeholder || ''
  visible.value = true
  nextTick(() => { inputRef.value?.focus() })
  return new Promise(resolve => { resolvePromise = resolve })
}

function handleConfirm() {
  visible.value = false
  if (showInput.value) {
    resolvePromise?.(inputValue.value)
  } else {
    resolvePromise?.(true)
  }
}

function handleCancel() {
  visible.value = false
  if (showInput.value) {
    resolvePromise?.(null)
  } else {
    resolvePromise?.(false)
  }
}

defineExpose({ confirm, prompt })
</script>

<style scoped>
.confirm-dialog {
  background: var(--bg-card, #fff);
  border-radius: 12px;
  padding: 28px;
  width: 400px;
  max-width: 90vw;
  text-align: center;
  box-shadow: 0 20px 60px rgba(0, 0, 0, 0.3);
}
.confirm-icon {
  margin-bottom: 12px;
  display: flex;
  justify-content: center;
}
.icon-warning, .icon-danger { color: #f59e0b; }
.icon-danger { color: #ef4444; }
.icon-info { color: #3b82f6; }
.confirm-title {
  font-size: 17px;
  font-weight: 600;
  margin-bottom: 8px;
}
.confirm-message {
  font-size: 14px;
  color: var(--text-secondary, #64748b);
  margin-bottom: 20px;
  line-height: 1.5;
}
.confirm-input {
  margin-bottom: 20px;
}
.confirm-actions {
  display: flex;
  gap: 10px;
  justify-content: center;
}
.confirm-actions .btn {
  min-width: 80px;
  justify-content: center;
}
.modal-fade-enter-active, .modal-fade-leave-active { transition: all 0.2s ease; }
.modal-fade-enter-from, .modal-fade-leave-to { opacity: 0; }
.modal-fade-enter-from .confirm-dialog, .modal-fade-leave-to .confirm-dialog {
  transform: scale(0.95);
}
</style>
