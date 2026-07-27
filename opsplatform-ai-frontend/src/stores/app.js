import { defineStore } from 'pinia'
import { ElMessageBox } from 'element-plus'

export const useAppStore = defineStore('app', {
  actions: {
    // 统一二次确认（禁用浏览器原生弹窗）
    showConfirm(message, title = '确认') {
      return ElMessageBox.confirm(message, title, { type: 'warning' })
    },
  },
})
