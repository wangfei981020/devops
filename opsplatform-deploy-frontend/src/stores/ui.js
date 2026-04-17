let toastRef = null
let confirmRef = null

export function setToastRef(ref) { toastRef = ref }
export function setConfirmRef(ref) { confirmRef = ref }

export function toast(message, type = 'info', duration = 3000) {
  if (toastRef) toastRef.show(message, type, duration)
}

export function success(message) { toast(message, 'success') }
export function error(message) { toast(message, 'error', 5000) }
export function info(message) { toast(message, 'info') }

export function confirm(options) {
  return new Promise((resolve) => {
    if (confirmRef) {
      confirmRef.show(options, resolve)
    } else {
      resolve(window.confirm(options.message || '确认?'))
    }
  })
}
