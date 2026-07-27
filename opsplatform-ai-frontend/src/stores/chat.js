import { defineStore } from 'pinia'

function rid() {
  return 'sess-' + Math.random().toString(36).slice(2) + Date.now().toString(36)
}

// 会话 id 放全局 store，侧栏「新建对话」改它，对话页 watch 到变化即清空。
export const useChatStore = defineStore('chat', {
  state: () => ({ sessionId: rid() }),
  actions: {
    newSession() {
      this.sessionId = rid()
    },
  },
})
