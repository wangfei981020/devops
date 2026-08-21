import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from 'react'

/**
 * 全局时间范围。
 *
 * 告警产品的基本交互：改一次，统计条、时序图、列表一起跟着变。
 * 通用后台通常把时间筛选放在每个页面自己的工具条里，于是同一屏上
 * 「统计说最近 24 小时」而「列表其实是全部」——两个数字对不上，
 * 排障时得先搞清楚哪个骗了你。
 *
 * ⚠️ 默认是 24h 而不是 1h：窗口太窄会让人以为"就这么几条"。
 * 「全部」保留成显式选项 —— 有些事件已经持续好几天了，
 * 任何窗口都会把它切掉，而那恰恰是最该看见的那条。
 */
export const RANGES = ['1h', '6h', '24h', '7d', ''] as const
export type TimeRange = (typeof RANGES)[number]

const STORE_KEY = 'opsalert.range'
/** 「全部」时图表退回的窗口。图表必须有个有限窗口，否则一根线画几个月没有意义。 */
export const SERIES_FALLBACK = '24h'

function readStored(): TimeRange {
  const v = localStorage.getItem(STORE_KEY)
  if (v == null) return '24h'
  return (RANGES as readonly string[]).includes(v) ? (v as TimeRange) : '24h'
}

const Ctx = createContext<{ range: TimeRange; setRange: (r: TimeRange) => void }>({
  range: '24h',
  setRange: () => {},
})

export function TimeRangeProvider({ children }: { children: ReactNode }) {
  const [range, setRangeState] = useState<TimeRange>(readStored)
  const setRange = useCallback((r: TimeRange) => {
    setRangeState(r)
    localStorage.setItem(STORE_KEY, r)
  }, [])
  return <Ctx.Provider value={{ range, setRange }}>{children}</Ctx.Provider>
}

export function useTimeRange() {
  return useContext(Ctx)
}

/** 拼进查询串。空范围 = 不加参数 = 后端返回全部。 */
export function rangeParam(range: TimeRange): string {
  return range ? `&range=${range}` : ''
}

/**
 * 数据新鲜度。顶栏的「实时」指示灯用它。
 *
 * ⚠️ 这盏灯**不能常亮绿色**。永远绿的实时灯在故障时最会骗人：
 * 后端挂了、轮询在失败、标签页被浏览器冻结，界面照样一片祥和，
 * 而你正盯着一份十分钟前的数据做判断。
 * 所以它读的是"上一次成功刷新离现在多久"，超时就变成警告色。
 */
export function useFreshness(updatedAt: number | undefined, intervalMs: number) {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 5_000)
    return () => clearInterval(id)
  }, [])
  if (!updatedAt) return { ageSec: null as number | null, stale: false }
  const ageSec = Math.max(0, Math.round((now - updatedAt) / 1000))
  // 容忍两个轮询周期：一次抖动不该把灯打成红的
  return { ageSec, stale: ageSec * 1000 > intervalMs * 2.5 }
}
