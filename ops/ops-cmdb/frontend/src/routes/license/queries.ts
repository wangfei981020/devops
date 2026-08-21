import { ApiError, normalizeError, shouldRetry } from '@ops/api'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { getToken } from '../../lib/auth.js'

/**
 * 授权状态。
 *
 * 七态各自的含义与出路完全不同，所以这里**不做任何归并**——
 * 不要在前端算出一个 `expired = status !== 'active'` 之类的布尔量，
 * 那会把"指纹不匹配"和"欠费"说成同一句话，客户每次都得打电话问。
 */
export type LicenseStatus =
  | 'active'
  | 'grace'
  | 'expired'
  | 'lapsed'
  | 'finger_mismat'
  | 'not_activated'
  | 'not_licensed'

export interface Exceeded {
  item: string
  current: number
  limit: number
}

export interface LicenseInfo {
  status: LicenseStatus
  readOnly: boolean
  /** null = 不适用（永久授权 / 未激活），0 = 今天到期。绝不能压成 0 */
  daysUntilExpiry: number | null
  shouldRemind: boolean
  perpetual: boolean
  licensee: { org: string; contact: string; scopeName: string } | null
  expiresAt: string
  licenseId: string
  /** 截断显示用 */
  /**
   * 安装指纹，**完整值**。申请授权时要报给我们，所以界面完整显示。
   *
   * ⚠️ 这里原来还有一个截断版 `fingerprintFull` 的对偶字段（后端 `fingerprint`
   * 是截断的、`fingerprint_full` 才是全的），而截断版一个消费方都没有。
   * "掩码 + 全量并存"会被照抄到真正的敏感字段上（OPSCMDB-031 P2-67）。
   */
  fingerprint: string
  /** 完整指纹，复制按钮取这个 —— 拿截断的去签发会签出一份永远对不上的授权 */
  capacity: Record<string, number>
  usage: Record<string, number>
  /**
   * 保留策略是不是「永不清理」。
   *
   * ⚠️ 单独一个字段，**不要从 `usage.retention_days === 0` 推断**。
   * 0 同时可能是"没取到配置"和"永不清理"，而这两者的下一步相反：
   * 前者要去查为什么取不到，后者是一个已经超出授权上限的真实配置。
   * 原来这一格渲染成「—」（看起来像没数据），而实际配的就是 0 ——
   * 而且它超出了 3650 天的上限，`exceeded` 里却什么都没有（P1-73）。
   */
  retentionUnlimited: boolean
  exceeded: Exceeded[]
  features: string[]
  /** 这一档**没有**的功能。只列已启用的，客户看不出缺什么、也就没有升级动机（P1-74） */
  featuresMissing: string[]
  featuresTotal: number
}

interface RawLicense {
  status?: string
  read_only?: boolean
  days_until_expiry?: number | null
  should_remind?: boolean
  perpetual?: boolean
  licensee?: { org?: string; contact?: string; scope_name?: string } | null
  expires_at?: string
  license_id?: string
  fingerprint?: string
  capacity?: Record<string, number>
  usage?: Record<string, number & { retention_unlimited?: boolean }>
  exceeded?: Exceeded[]
  features?: string[]
  features_missing?: string[]
  features_total?: number
}

function toInfo(d: RawLicense): LicenseInfo {
  return {
    // ⚠️ 兜底成 not_activated 而不是 active：认不出来的状态当成"正常"，
    // 等于把一个我们还不认识的授权问题渲染成一切正常
    status: (d.status ?? 'not_activated') as LicenseStatus,
    // ⚠️ 同理兜底成 true。缺字段时当成可写，会让界面显示一堆点了没反应的按钮
    readOnly: d.read_only !== false,
    daysUntilExpiry: d.days_until_expiry ?? null,
    shouldRemind: d.should_remind === true,
    perpetual: d.perpetual === true,
    licensee: d.licensee
      ? {
          org: d.licensee.org ?? '',
          contact: d.licensee.contact ?? '',
          scopeName: d.licensee.scope_name ?? '',
        }
      : null,
    expiresAt: d.expires_at ?? '',
    licenseId: d.license_id ?? '',
    fingerprint: d.fingerprint ?? '',
    capacity: d.capacity ?? {},
    usage: (d.usage ?? {}) as Record<string, number>,
    // ⚠️ 兜底成 false：读不到时不要声称"永不清理"，那是个会触发超限提示的结论
    retentionUnlimited: (d.usage as { retention_unlimited?: boolean } | undefined)
      ?.retention_unlimited === true,
    exceeded: d.exceeded ?? [],
    features: d.features ?? [],
    featuresMissing: d.features_missing ?? [],
    featuresTotal: d.features_total ?? 0,
  }
}

async function call<T>(path: string, init?: RequestInit): Promise<T> {
  let res: Response
  try {
    res = await fetch(path, {
      ...init,
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${getToken()}`,
        ...(init?.headers ?? {}),
      },
    })
  } catch (e) {
    throw new ApiError(normalizeError(e, { path }))
  }
  if (!res.ok) {
    throw new ApiError(
      normalizeError(await res.json().catch(() => ({})), {
        method: init?.method ?? 'GET',
        path,
        status: res.status,
      }),
    )
  }
  return (await res.json()) as T
}

export const licenseKey = ['license'] as const

export function useLicense() {
  return useQuery({
    queryKey: licenseKey,
    queryFn: async () => toInfo(await call<RawLicense>('/api/license')),
    // 授权状态变化是低频的，但**过期是会自己发生的**（时间到了就到了），
    // 所以不能设成 Infinity 让一个长开的页面永远显示"正常"
    staleTime: 60_000,
    refetchInterval: 5 * 60_000,
    retry: shouldRetry,
  })
}

export function useActivate() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (token: string) =>
      toInfo(
        await call<RawLicense>('/api/license', {
          method: 'POST',
          body: JSON.stringify({ token: token.trim() }),
        }),
      ),
    onSuccess: (info) => {
      qc.setQueryData(licenseKey, info)
      // 授权一变，能不能写、有哪些功能全变了。
      // 逐个失效容易漏，整体重取最省心
      void qc.invalidateQueries()
    },
  })
}
