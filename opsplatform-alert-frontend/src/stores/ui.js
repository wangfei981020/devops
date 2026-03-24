import { ref } from 'vue'

// Global refs to Toast and ConfirmDialog instances (set by App.vue)
const toastRef = ref(null)
const confirmRef = ref(null)

export function setToastRef(ref) { toastRef.value = ref }
export function setConfirmRef(ref) { confirmRef.value = ref }

export function useToast() {
  return {
    success: (msg) => toastRef.value?.show(msg, 'success'),
    error: (msg) => toastRef.value?.show(msg, 'error', 5000),
    warning: (msg) => toastRef.value?.show(msg, 'warning'),
    info: (msg) => toastRef.value?.show(msg, 'info'),
  }
}

export function useConfirm() {
  return {
    confirm: (opts) => confirmRef.value?.confirm(typeof opts === 'string' ? { message: opts } : opts),
    prompt: (opts) => confirmRef.value?.prompt(typeof opts === 'string' ? { message: opts } : opts),
    danger: (opts) => confirmRef.value?.confirm({ type: 'danger', confirmText: '删除', ...(typeof opts === 'string' ? { message: opts } : opts) }),
  }
}
