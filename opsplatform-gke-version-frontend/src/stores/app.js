import { defineStore } from 'pinia'
import { ElMessageBox } from 'element-plus'

export const useAppStore = defineStore('app', {
  actions: {
    async showConfirm(message, title = '确认') {
      try {
        await ElMessageBox.confirm(message, title, {
          confirmButtonText: '确定',
          cancelButtonText: '取消',
          type: 'warning',
        })
        return true
      } catch {
        return false
      }
    },
  },
})
