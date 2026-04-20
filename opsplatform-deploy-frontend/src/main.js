import { createApp } from 'vue'
import { createPinia } from 'pinia'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
import zhCn from 'element-plus/es/locale/lang/zh-cn'
import App from './App.vue'
import router from './router'
import './styles/global.css'

const app = createApp(App)
app.use(createPinia())
app.use(router)
app.use(ElementPlus, { locale: zhCn })

// 全局错误捕获
app.config.errorHandler = (err, _, info) => {
  console.error('Vue error:', err, info)
}
window.addEventListener('unhandledrejection', (e) => {
  console.error('Unhandled promise:', e.reason)
})

app.mount('#app')
