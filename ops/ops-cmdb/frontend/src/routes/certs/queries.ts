import { queryKeys, shouldRetry } from '@ops/api'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../../lib/api.js'
import { apiAction, apiGet } from '../../lib/fetchJson.js'

/**
 * 证书。
 *
 * ⚠️ 这里**没有也不该有** cert_pem / key_pem 字段 —— 列表接口不返回它们。
 * 上一代在证书接口上出过私钥泄露的 P0：它不报错、不影响功能，
 * 只是安静地躺在每个人的 devtools 里。加字段前先想清楚谁会看到。
 */
export interface Cert {
  ciId: number
  cn: string
  sans: string
  ca: string
  status: string
  expiryAt: string
  /** ⚠️ null = 读不出到期日，**不是**"还有 0 天"。它可能已经过期了，只是我们不知道 */
  daysLeft: number | null
  autoRenew: boolean
  /** 上次续期的错误。与 autoRenew 必须一起看，见 healthOf */
  lastError: string
  updatedAt: string | null
}

export type CertHealth = 'all' | 'expired' | 'failing' | 'unknown' | 'soon' | 'ok'

export const CERT_SOON_DAYS = 30

/**
 * 健康度。与后端 certHealth 同一套判据 —— 两边分叉的话，
 * 筛选出来的条数和列表里的颜色会对不上。
 */
export function healthOf(c: Cert): Exclude<CertHealth, 'all'> {
  if (c.daysLeft === null) return 'unknown'
  if (c.daysLeft < 0) return 'expired'
  // 续期失败优先于"还剩多少天"：它正在失去自我修复能力，
  // 而"自动续期开着"这几个字会让人以为有人在管
  if (c.lastError !== '') return 'failing'
  if (c.daysLeft <= CERT_SOON_DAYS) return 'soon'
  return 'ok'
}

export interface CertListParams {
  page: number
  size: number
  q?: string
  health?: CertHealth
  [k: string]: string | number | undefined
}

export interface CertCaveat {
  /** never = 一整类信息压根不存在；partial = 部分缺失。决定色调 */
  kind: string
  note_key: string
  note_params?: Record<string, unknown>
}

export interface CertListResult {
  items: Cert[]
  total: number
  facets: Record<string, Record<string, number> | undefined>
  /** 这批数据的已知局限（某一列为什么整片是空的）。缺省 = 没有局限 */
  caveat?: CertCaveat
}

interface RawCert {
  ci_id?: number
  cn?: string
  sans?: string
  ca?: string
  status?: string
  expiry_at?: string
  days_left?: number | null
  auto_renew?: boolean
  last_error?: string
  updated_at?: string
}

function toCert(r: RawCert): Cert {
  return {
    ciId: r.ci_id ?? 0,
    cn: r.cn ?? '',
    sans: r.sans ?? '',
    ca: r.ca ?? '',
    status: r.status ?? '',
    expiryAt: r.expiry_at ?? '',
    // `?? 0` 在这里是危险的：会把一张读不出到期日的证书说成"今天到期"，
    // 更糟的是反过来 —— 用一个大数兜底会说成"还早着呢"
    daysLeft: r.days_left === undefined || r.days_left === null ? null : r.days_left,
    autoRenew: r.auto_renew === true,
    lastError: r.last_error ?? '',
    updatedAt: r.updated_at ? r.updated_at : null,
  }
}

export function useCerts(params: CertListParams) {
  return useQuery({
    queryKey: queryKeys.certs.list(params),
    queryFn: async (): Promise<CertListResult> => {
      const { data, error } = await api.GET('/cert-list', { params: { query: params } })
      if (error) throw error
      return {
        items: ((data?.items ?? []) as RawCert[]).map(toCert),
        total: data?.total ?? 0,
        facets: (data?.facets ?? {}) as Record<string, Record<string, number> | undefined>,
        // 🔴 到期日整片为空时后端会给出**为什么**。不显示它，
        //	一页空白的到期日看起来就是"数据还没采到"——
        //	而真相可能是"证书临期提醒根本不工作"（生产实测 890 张里 828 张如此，
        //	两张生产网关证书因此活到剩 28 小时）。
        caveat: (data as { caveat?: CertCaveat })?.caveat,
      }
    },
    staleTime: 30_000,
    retry: shouldRetry,
  })
}

/**
 * ACME 账号。
 *
 * # 为什么这一页非有不可
 *
 * 证书自动续期任务在没有对应 ACME 账号时会**逐张证书失败**，
 * 报的是「无对应 ACME 账户（ca=letsencrypt）」——而在旧版里根本没有地方能配它。
 * 实测这个任务就是这么挂着的。
 *
 * ⚠️ `ca` 要和证书上记录的 CA **完全一致**（letsencrypt / zerossl…）：
 * 续期时按 ca 找账号，对不上就等于没配。
 */
export interface AcmeAccount {
  id: number
  email: string
  ca: string
  /**
   * 账号私钥配没配（后端判据：account_key_enc 非空）。没有的话这条账号是摆设。
   *
   * ⚠️ 字段名是 `registered`，**不是 has_key**。
   * 声明错的后果是**反向假警报**：`!a.has_key` 里 undefined 取反恒为 true，
   * 于是每一条 ACME 账户都被标上红色的「无私钥」——
   * 包括配好了正常在用的。假警报比缺信息更坏：它会让人去修一个没坏的东西，
   * 修完发现标签还在，最后学会忽略这个标签。
   */
  registered: boolean
  status: string
}

export function useAcmeAccounts() {
  return useQuery({
    queryKey: ['acme-accounts'],
    queryFn: () => apiGet<AcmeAccount[]>('/api/acme-accounts'),
    staleTime: 60_000,
    retry: shouldRetry,
  })
}

export function useCreateAcme() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (b: { email: string; ca: string }) => apiAction('/api/acme-accounts', 'POST', b),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['acme-accounts'] }),
  })
}

export function useDeleteAcme() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => apiAction(`/api/acme-accounts/${id}`, 'DELETE'),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['acme-accounts'] }),
  })
}

// ─────────────────────────────────────────────────────────────────
// 证书本身的写操作
//
// ⚠️ 这一块以前是空的：证书页只能配 ACME 账号，**申请/续期/删除证书全都够不着**。
// 后端 4 个接口一直都在。于是这一页的实际能力是"看着证书过期"。
// ─────────────────────────────────────────────────────────────────

/** 与后端 certApplyIn 对齐 */
export interface CertApplyInput {
  cn: string
  sans: string[]
  ca: string
  challenge: string
  acme_account_id: number
  domain_ci_id?: number
  project?: string
  env?: string
  module?: string
  owner?: string
  auto_renew: number
  renew_days: number
  /** 用 CA 的测试环境签。⚠️ 签出来的证书浏览器不认，只用于验流程 */
  staging?: boolean
}

type Res = { ok?: boolean; error?: string; msg?: string; id?: number }

function useCertDone() {
  const qc = useQueryClient()
  return () => void qc.invalidateQueries({ queryKey: queryKeys.certs.all })
}

/**
 * 申请证书。
 *
 * ⚠️ **不自动重试**。这是对 ACME 服务商的非幂等调用，而 Let's Encrypt 有
 * 每周签发次数限制（同一组域名 5 次/周）—— 自动重试几次就可能把配额烧掉，
 * 之后一周都签不出来。
 */
export function useApplyCert() {
  const done = useCertDone()
  return useMutation({
    retry: false,
    mutationFn: (v: CertApplyInput) => apiAction<Res>('/api/certs', 'POST', v),
    onSuccess: done,
  })
}

/** 立即续期。同样不重试，同样吃 CA 的配额 */
export function useRenewCert() {
  const done = useCertDone()
  return useMutation({
    retry: false,
    mutationFn: (ciId: number) => apiAction<Res>(`/api/certs/${ciId}/renew`, 'POST'),
    onSuccess: done,
  })
}

/**
 * DNS-01 手动验证：TXT 记录加好之后点这个放行签发。
 *
 * ⚠️ 点早了会失败并消耗一次配额 —— 界面上必须先把要加的 TXT 记录摆出来，
 * 让人核对完再点，而不是给一个孤零零的「继续」按钮。
 */
export function useCertDNSReady() {
  const done = useCertDone()
  return useMutation({
    retry: false,
    mutationFn: (ciId: number) => apiAction<Res>(`/api/certs/${ciId}/dns-ready`, 'POST'),
    onSuccess: done,
  })
}

/**
 * 删除证书记录。
 *
 * ⚠️ 后端函数名叫 Revoke，但它做的是**删本地记录**，不是去 CA 吊销证书 ——
 * 已经签发出去的那张证书在有效期内仍然被信任。文案必须说清楚这件事，
 * 否则出了私钥泄露事故，有人会以为在这儿点一下就吊销了。
 */
export function useDeleteCert() {
  const done = useCertDone()
  return useMutation({
    retry: false,
    mutationFn: (ciId: number) => apiAction<Res>(`/api/certs/${ciId}`, 'DELETE'),
    onSuccess: done,
  })
}

/**
 * 把后端给的探测局限翻成一句话。
 *
 * ⚠️ "另有 N 条探测失败"是**单独一条 key**，不是拼在主句尾巴上的。
 *	中文可以往后接，英文的从句位置不同 —— 后端拼好再发过来，
 *	换语言就必然别扭（OPSCMDB-054 的教训之一：能拼的地方都别拼）。
 */
export function probeNoteText(
  t: (key: string, params?: Record<string, unknown>) => string,
  noteKey?: string,
  params?: Record<string, unknown>,
): string {
  if (!noteKey) return ''
  let text = t(noteKey, params)
  const failed = params?.failed
  // someFailed 那一条本身就在说失败数，不要再接一遍
  if (typeof failed === 'number' && failed > 0 && !noteKey.endsWith('someFailed')) {
    text += t('certs:probe.alsoFailed', { failed })
  }
  return text
}
