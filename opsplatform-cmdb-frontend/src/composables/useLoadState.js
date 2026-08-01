import { ref } from 'vue'
import { normalizeError } from '../api/http'

// useLoadState —— 页面加载的三态（loading / error / success）统一实现。
//
// 为什么要有它：CMDB-013。后端挂掉时全站页面显示的是「共 0 条 · 全部健康」，
// 因为每页都写成 `try { list = await api() } catch (e) {}` —— 异常被吞掉，
// 空数组既表示"真的没有"也表示"没拿到"，而所有结论文案都按前者写。
// 监控类系统最危险的失效模式就是这个：故障时告诉运维"没问题"。
//
// 约定：
//   - error 非空 => 这一屏的数据不可信，页面必须显式报错，且不得展示任何计数/结论
//   - ok       => 只有成功加载过才为 true，用于给"全部健康"这类断言把关
//   - num(v)   => error 态下统计数字显示 '—'，因为 0 是一个断言，没拿到数据时给断言就是撒谎
export function useLoadState() {
  const loading = ref(false)
  const error = ref('')
  const ok = ref(false)

  // run 包住一次加载：异常一律落到 error，loading 必定复位
  //（此前的"遮罩还在转 + 显示 No Data"就是异常路径没复位 loading 造成的）。
  async function run(fn) {
    loading.value = true
    error.value = ''
    try {
      const r = await fn()
      ok.value = true
      return r
    } catch (e) {
      error.value = normalizeError(e).message
      ok.value = false
      return undefined
    } finally {
      loading.value = false
    }
  }

  // num 统计卡取值：失败时给 '—' 而不是 0
  function num(v) {
    if (error.value) return '—'
    return v ?? 0
  }

  return { loading, error, ok, run, num }
}
