import { createApp } from 'vue'
import { createPinia } from 'pinia'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
import zhCn from 'element-plus/es/locale/lang/zh-cn'
import App from './App.vue'
import router from './router'
import './styles/global.css'

// 字体本地化（替代 Google Fonts CDN，生产内网也能用）
import '@fontsource/inter/400.css'
import '@fontsource/inter/500.css'
import '@fontsource/inter/600.css'
import '@fontsource/inter/700.css'
import '@fontsource/fira-code/400.css'
import '@fontsource/fira-code/500.css'
import '@fontsource/fira-code/600.css'

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
