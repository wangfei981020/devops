import { shouldRetry } from '@ops/api'
import { useQuery } from '@tanstack/react-query'
import { apiGet } from './fetchJson.js'

/**
 * 字典类下拉的数据源。
 *
 * CONVENTIONS §2.7.4：字典类下拉一律从接口取，禁止前端写死。
 * 写死的话，用户在「管理 → 基础配置」里新增一个环境，所有下拉都选不到它，
 * 而且**没有任何报错** —— 表现成"这个环境不存在"。
 *
 * ⚠️ 放在 lib 而不是某个 routes 目录下：环境字典被多个页面用
 * （观测端点、镜像仓库…），挂在其中一个页面的 queries 里会导致跨路由 import，
 * 那个页面被删或改名时会连坐。
 */

export interface EnvOption {
  code: string
  name: string
  /** 英文显示名。空 = 没填，回退中文名 */
  name_en?: string
}

/**
 * 按当前语言取字典项的显示名。
 *
 * # ⚠️ 为什么需要这个函数，而不是各处自己判
 *
 * 字典项的显示名是**数据**不是文案（客户可以自己增删改环境、状态、类型），
 * 所以它不在语言包里，`t()` 帮不上忙。
 * 于是英文界面下这些枚举值全是中文（OPSCMDB-031 P1-76）：
 *   生产 / 测试 / 开发 / 域名 / 证书 / 主机 / 使用中 / 备用 / 待下线…
 *
 * 各处自己写 `lang === 'en' ? x.name_en : x.name` 的话，
 * 迟早有一处忘了写 —— 而忘掉的那处不会报错，只是那一列继续显示中文。
 * 收敛成一个函数，配一个守卫（见 tooling）就能查。
 *
 * ⚠️ **空值回退中文，不回退空字符串**：
 * 没填英文名时显示中文，比显示一个空白单元格好得多。
 */
export function dictLabel(
  item: { name?: string; name_en?: string; label?: string; label_en?: string },
  lang: string,
): string {
  const zh = item.name ?? item.label ?? ''
  const en = item.name_en ?? item.label_en ?? ''
  // 只有明确是英文环境且真的填了英文名才用它
  if (lang.startsWith('en') && en !== '') return en
  return zh
}

export function useEnvOptions() {
  return useQuery({
    queryKey: ['environments'],
    queryFn: () => apiGet<EnvOption[]>('/api/environments'),
    // 字典变得很少，5 分钟足够；真改了的话切页面就会重新拉
    staleTime: 5 * 60_000,
    retry: shouldRetry,
  })
}
