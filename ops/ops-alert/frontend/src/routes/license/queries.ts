import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { get, post } from '../../lib/api.js'

/**
 * 授权状态七态。与 licensekit 的 Status 一一对应。
 *
 * ⚠️ 这是个联合类型而不是 string，是为了让 banner.ts 里的 never 断言生效：
 * 共享库新增一态而前端没跟上时，编译期就失败。
 * 写成 string 的话，新状态会静默走进 default 分支被当成正常。
 */
export type LicenseStatus =
  | 'active'
  | 'grace'
  | 'expired'
  | 'lapsed'
  | 'finger_mismat'
  | 'not_activated'
  | 'not_licensed'

export interface FeatureState {
  /** 真的能用（授权里有 **且** 这版实现了）。业务判断只看它 */
  has: boolean
  /**
   * 授权里有没有买这一项，**不管实现了没有**。
   *
   * ⚠️ 界面上判断"要不要提示去采购"必须用它而不是 has ——
   * has 已经把 implemented 折进去了，买了但没做的功能 has 也是 false，
   * 拿它判断会让客户去催采购，而他其实已经买了。
   */
  granted: boolean
  /** 这个版本有没有实现它 */
  implemented: boolean
}

export interface LicenseInfo {
  status: LicenseStatus
  readOnly: boolean
  /** null = 不适用（永久授权 / 未激活），0 = 今天到期。**绝不能压成 0** */
  daysUntilExpiry: number | null
  shouldRemind: boolean
  perpetual: boolean
  licenseId: string
  licensee: { org?: string; contact?: string; scope_name?: string } | null
  expiresAt: string | null
  features: Record<string, FeatureState>
  /** 授权里给的容量上限，0 = 不限 */
  capacity: Record<string, number>
  /** 未激活时生效的上限 */
  ceLimits: Record<string, number>
  /** 后端接没接授权存储。false 时不显示激活入口 */
  canActivate: boolean
}

type RawLicense = {
  status: LicenseStatus
  read_only: boolean
  days_until_expiry: number | null
  should_remind: boolean
  perpetual?: boolean
  license_id?: string
  licensee?: LicenseInfo['licensee']
  expires_at?: string
  features: Record<string, FeatureState>
  capacity?: Record<string, number>
  ce_limits?: Record<string, number>
  can_activate: boolean
}

/**
 * ⚠️ 这里逐字段映射，**不用 `as unknown as LicenseInfo` 糊过去**。
 * 后端是 snake_case、前端是 camelCase，强转能编译过但运行期全是 undefined，
 * 而 undefined 在界面上渲染成空白 —— 看起来像"这个客户没填"，
 * 而不是"我们没接上"。check-field-names 守卫只查字段名对不对得上，
 * 查不出这一层。
 */
function toInfo(r: RawLicense): LicenseInfo {
  return {
    status: r.status,
    readOnly: r.read_only,
    daysUntilExpiry: r.days_until_expiry,
    shouldRemind: r.should_remind,
    perpetual: r.perpetual ?? false,
    licenseId: r.license_id ?? '',
    licensee: r.licensee ?? null,
    expiresAt: r.expires_at ?? null,
    features: r.features ?? {},
    capacity: r.capacity ?? {},
    ceLimits: r.ce_limits ?? {},
    canActivate: r.can_activate,
  }
}

export function useLicense() {
  return useQuery({
    queryKey: ['license'],
    queryFn: async () => toInfo(await get<RawLicense>('/license')),
    // 60 秒刷一次：激活之后其余副本要 20 秒才收敛，
    // 不自动刷的话管理员会看到"激活了但状态没变"并反复重贴
    refetchInterval: 60_000,
  })
}

/** 安装指纹。单独一个接口——它要连库算，不该拖慢状态查询。 */
export function useFingerprint() {
  return useQuery({
    queryKey: ['license', 'fingerprint'],
    queryFn: () => get<{ fingerprint: string }>('/license/fingerprint'),
    staleTime: Infinity, // 一套安装里它不变
  })
}

export function useActivate() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (token: string) =>
      post<{ status: string; features: string[]; note: string }>('/license', { token }),
    onSuccess: () => {
      // 激活会改变几乎所有页面的可用性，所以把整棵缓存刷掉而不是只刷 license
      qc.invalidateQueries()
    },
  })
}
