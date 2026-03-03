import zhCN from './zh-CN.js'
import enUS from './en-US.js'

export const messages = {
  'zh-CN': zhCN,
  'en-US': enUS
}

export function createI18n(getCurrentLanguage) {
  const cache = new Map()
  
  function getNestedValue(obj, path) {
    const cacheKey = `${getCurrentLanguage()}_${path}`
    if (cache.has(cacheKey)) {
      return cache.get(cacheKey)
    }
    
    const keys = path.split('.')
    let result = obj
    for (const key of keys) {
      if (result && typeof result === 'object' && key in result) {
        result = result[key]
      } else {
        result = undefined
        break
      }
    }
    
    if (result !== undefined) {
      cache.set(cacheKey, result)
    }
    return result
  }

  function t(key, params = {}) {
    const lang = getCurrentLanguage()
    const langMessages = messages[lang] || messages['zh-CN']
    let text = getNestedValue(langMessages, key)
    
    if (text === undefined) {
      const fallback = messages['zh-CN']
      text = getNestedValue(fallback, key)
    }
    
    if (text === undefined) {
      return key
    }
    
    if (typeof text === 'string' && Object.keys(params).length > 0) {
      Object.entries(params).forEach(([paramKey, paramValue]) => {
        text = text.replace(new RegExp(`\\{${paramKey}\\}`, 'g'), paramValue)
      })
    }
    
    return text
  }

  function clearCache() {
    cache.clear()
  }

  return { t, clearCache }
}

export default messages
