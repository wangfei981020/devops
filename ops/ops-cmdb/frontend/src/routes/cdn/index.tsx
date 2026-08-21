import { toErrorInfo } from '@ops/api'
import { tError, useTranslation } from '@ops/i18n'
import {
  AsyncBoundary,
  Button,
  Badge,
  type LoadError,
  NotIngested,
  Skeleton,
  fromQuery,
} from '@ops/ui'
import { useState } from 'react'
import { RowMenu } from '../../components/RowMenu.js'
import { WriteButton } from '../../components/WriteButton.js'
import { AccountDialog } from './AccountDialog.js'
import {
  type CdnAccount,
  useCdnAccounts,
  useCdnZones,
  useDeleteCdnAccount,
  useSyncCdnAccount,
  useVerifyCdnAccount,
} from './queries.js'

// 见 AccountDialog：账号写走 sync_cdn（后端 /api/cdn/ 前缀规则如此）
const PERM = 'cmdb:sync_cdn'

/**
 * CDN 站点（Cloudflare）。
 *
 * ⚠️ 归在「域名与入口」而不是「平台服务」：人来看这一页几乎都是在排查
 * 某个域名，而不是在盘点"我们用了哪些外部平台"。按排障路径分组。
 */
import { CdnAnalysisDialog } from './AnalysisDialog.js'
import { CdnForensicsDialog } from './ForensicsDialog.js'
import { CdnTokenCheckDialog } from './TokenCheckDialog.js'

export function CdnPage() {
  // CDN 分析下钻：规则体检 / 规则 / 边缘证书 / 域名走向（OPSCMDB-021）
  const [analysis, setAnalysis] = useState(false)
  const [tokenCheck, setTokenCheck] = useState(false)
  const [forensics, setForensics] = useState(false)
  const { t } = useTranslation()
  const query = useCdnZones()
  const accounts = useCdnAccounts()
  const delAccount = useDeleteCdnAccount()
  const [editing, setEditing] = useState<CdnAccount | null | undefined>(undefined)

  const toLoadError = (e: unknown): LoadError => {
    const n = toErrorInfo(e)
    return { cause: tError(t, n.messageKey, n.params), detail: n.detail, retryable: n.retryable, kind: n.kind === 'license' ? 'license' : undefined, feature: n.params?.feature as string | undefined }
  }

  return (
    <div className="mx-auto max-w-[1080px] p-5">
      <div className="mb-4 flex items-baseline gap-2.5">
        <h1 className="text-sm font-semibold text-foreground">{t('cdn:title')}</h1>
        <p className="min-w-0 flex-1 text-xs text-muted-foreground">{t('cdn:hint')}</p>
        {/* 🔴 规则分析原来**没有任何打开入口**：`setAnalysis` 全文件只有
            `onClose` 里那一处调用、传的是 false，所以 state 永远是 false，
            弹窗永远不渲染（OPSCMDB-034）。

            ⚠️ 这类缺陷 tsc 查不出来 —— state 确实"被用到"了（用在条件渲染上），
            页面也不报错，少的只是一个按钮，而没人会去数按钮。
            git log 确认这个触发器**从来没写过**，不是改版时弄丢的。 */}
        {/* 🔴 规则分析原来**没有任何打开入口**：`setAnalysis` 全文件只有
            `onClose` 里那一处调用、传的是 false，所以 state 永远是 false，
            弹窗永远不渲染（OPSCMDB-034）。

            ⚠️ 这类缺陷 tsc 查不出来 —— state 确实"被用到"了（用在条件渲染上），
            页面也不报错，少的只是一个按钮，而没人会去数按钮。
            git log 确认这个触发器**从来没写过**，不是改版时弄丢的。 */}
        <Button size="sm" onClick={() => setAnalysis(true)}>
          {t('cdn:action.analysis')}
        </Button>
        {/* 🔴 token 体检是「CDN 数据为什么是空的」的答案所在。
            绝大多数时候是 token 少勾了一个权限，而这件事在别的任何页面上
            都看不出来——列表只会显示「明细获取失败: 403」或者干脆是空的。
            
            ⚠️ 它**实时打 Cloudflare**，与这一页其它数据（快照）口径不同，
            所以只在点开时请求一次，不预取不轮询（OPSCMDB-023 的接线约定）。*/}
        <Button size="sm" onClick={() => setTokenCheck(true)}>
          {t('cdn:action.tokenCheck')}
        </Button>
        {/* 🔴 实时取证：跨公网扯皮时唯一的锚点。
            对方说「请求早就发出去了，你们很久才收到」时，中间那段
            没有任何一方看得见 —— `datetime_cst`（CF 边缘收到的时刻）
            把总延迟切成「到达 CF 之前」和「CF 之后」两段。
            ⚠️ 同样实时打 CF，只在点「查询」时请求。*/}
        <Button size="sm" onClick={() => setForensics(true)}>
          {t('cdn:action.forensics')}
        </Button>
        <WriteButton perm={PERM} variant="primary" size="sm" onClick={() => setEditing(null)}>
          {t('cdn:action.addAccount')}
        </WriteButton>
      </div>

      {/* 账号在站点上面：没有账号就不会有站点，先让人看到该配什么。
          账号列表为空时，下面的"没有站点"才有解释 */}
      {(accounts.data ?? []).length > 0 ? (
        <div className="mb-5 rounded-[var(--radius-lg)] border border-border p-4">
          <div className="mb-2 text-xs font-medium text-foreground">{t('cdn:section.accounts')}</div>
          <div className="flex flex-col">
            {(accounts.data ?? []).map((a) => (
              <div
                key={a.id}
                className="flex items-center gap-3 border-b border-border py-2 text-[13px] last:border-0"
              >
                <span className="truncate font-medium text-foreground">{a.name}</span>
                <Badge tone="mute">{a.cdn || '—'}</Badge>
                {!a.enabled ? <Badge tone="mute">{t('cdn:disabled')}</Badge> : null}
                {!a.has_credential ? <Badge tone="bad">{t('cdn:noToken')}</Badge> : null}
                <span className="ml-auto text-[11px] text-muted-foreground">
                  {/* 空 = 从没同步过，和"同步失败"要分开：
                      前者是刚接入还没跑，后者是接了但拉不到 */}
                  {a.last_sync_at
                    ? `${t('cdn:lastSync')}: ${a.last_sync_at}`
                    : t('cdn:neverSynced')}
                </span>
                {a.last_result && !a.last_result.startsWith('OK') ? (
                  <span
                    className="max-w-[220px] truncate text-[11px] text-danger"
                    title={a.last_result}
                  >
                    {a.last_result}
                  </span>
                ) : null}
                {/* ⚠️ 「从没同步过」这个状态一直显示在左边，却没有任何办法让它同步一次 ——
                    后端 verify / sync 两个接口一直都在，前端没接 */}
                <AccountActions id={a.id} t={t} />
                <RowMenu
                  perm={PERM}
                  items={[
                    { key: 'edit', label: t('common:write.edit'), onClick: () => setEditing(a) },
                    {
                      key: 'delete',
                      label: t('common:write.delete'),
                      // ⚠️ 这里原本是点一下直接删，没有二次确认。
                      // 收进菜单只解决了"误点"，删除本身仍然应该有确认——已记进 OPSCMDB-029
                      onClick: () => delAccount.mutate(a.id),
                      danger: true,
                    },
                  ]}
                />
              </div>
            ))}
          </div>
        </div>
      ) : null}

      <AsyncBoundary
        state={fromQuery(query, (d) => d.length === 0, toLoadError)}
        errorTitle={t('cdn:error.title')}
        retryLabel={t('common:action.retry')}
        onRetry={() => void query.refetch()}
        pending={<Skeleton className="h-5 w-[60%]" />}
        empty={
          // ⚠️ 这里用 NotIngested 而不是普通空态。
          // "一个 CDN 站点都没有"绝大多数时候是没接凭据，
          // 而渲染成普通空态会让人以为"我们确实没用 CDN"
          <NotIngested
            title={t('cdn:empty.title')}
            reason={t('cdn:empty.reason')}
            action={
              <WriteButton perm={PERM} size="sm" onClick={() => setEditing(null)}>
                {t('cdn:action.addAccount')}
              </WriteButton>
            }
          />
        }
      >
        {(zones) => (
          <div className="flex flex-col">
            {zones.map((z) => (
              <div
                key={z.name}
                className="flex items-center gap-3 border-b border-border py-2.5 text-[13px] last:border-0"
              >
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <span className="truncate font-medium text-foreground">{z.name}</span>
                    {/* 状态原样透传（active / pending / moved），不翻译 ——
                        它是去 Cloudflare 控制台里搜的词 */}
                    <Badge tone={z.status === 'active' ? 'ok' : 'warn'}>{z.status ?? '—'}</Badge>
                    {z.plan ? <Badge tone="mute">{z.plan}</Badge> : null}
                    {/* 暂停的 zone 上所有规则都不生效。不标出来的话，
                        人会照着规则列表以为防护还在 */}
                    {z.paused ? <Badge tone="bad">{t('cdn:paused')}</Badge> : null}
                    {/* flexible = CF 到源站这一段是明文 HTTP。
                        和 full/strict 一样淡的话，这个真实风险就淹没了 */}
                    {z.ssl_mode === 'flexible' ? (
                      <Badge tone="bad">{t('cdn:sslFlexible')}</Badge>
                    ) : z.ssl_mode ? (
                      <Badge tone="mute">{z.ssl_mode}</Badge>
                    ) : null}
                  </div>
                  <div className="mt-0.5 truncate font-mono text-[11px] text-muted-foreground">
                    {(z.name_servers ?? []).join(' · ') || t('cdn:noNs')}
                  </div>
                  {/*
                    ⚠️ 后端一直在返回 risks（数组，一个站点可以同时命中多条），
                    前端从来没接。等于「flexible 回源明文」「zone 非 active」
                    这两条判定做了却没人看得见 ——
                    而 flexible 的实际后果是浏览器显示小锁、回源却是明文 HTTP。
                    刻意用数组渲染而不是只显示 risk 那一条：
                    既 paused 又 flexible 的站点两条都要说。
                  */}
                  {(z.risks ?? []).map((r) => (
                    <p key={r} className="mt-0.5 text-[11px] text-danger">
                      {r}
                    </p>
                  ))}
                </div>
                <span className="tabular w-[110px] shrink-0 text-right text-xs text-muted-foreground">
                  {/* undefined = 没采到，0 = 确实没有记录。不能都显示成 0 */}
                  {z.dns_count === undefined
                    ? t('cdn:recordsUnknown')
                    : t('cdn:records', { count: z.dns_count })}
                </span>
              </div>
            ))}
          </div>
        )}
      </AsyncBoundary>

      {editing !== undefined ? (
        <AccountDialog initial={editing ?? undefined} onClose={() => setEditing(undefined)} />
      ) : null}
      {analysis ? <CdnAnalysisDialog onClose={() => setAnalysis(false)} t={t} /> : null}
      {tokenCheck ? <CdnTokenCheckDialog onClose={() => setTokenCheck(false)} t={t} /> : null}
      {forensics ? (
        <CdnForensicsDialog
          zones={(query.data ?? []).map((z) => z.name ?? '').filter(Boolean)}
          onClose={() => setForensics(false)}
          t={t}
        />
      ) : null}
    </div>
  )
}

/** 测连通 / 立即同步。结果就地显示 —— 人是在这一行看到"没同步过"才点的 */
function AccountActions({
  id,
  t,
}: {
  id: number
  t: (k: string, p?: Record<string, unknown>) => string
}) {
  const verify = useVerifyCdnAccount()
  const sync = useSyncCdnAccount()
  const busy = verify.isPending || sync.isPending
  const err = verify.error ?? sync.error
  const done = verify.data ?? sync.data

  return (
    <>
      {busy ? (
        <span className="text-[11px] text-muted-foreground">{t('common:state.loading')}</span>
      ) : err ? (
        <span className="max-w-[200px] truncate text-[11px] text-danger" title={toErrorInfo(err).detail}>
          {tError(t, toErrorInfo(err).messageKey, toErrorInfo(err).params)}
        </span>
      ) : done ? (
        <span className="text-[11px] text-success">{done.msg ?? t('common:write.saved')}</span>
      ) : null}
      <WriteButton perm={PERM} size="sm" onClick={() => verify.mutate(id)}>
        {t('cdn:action.verify')}
      </WriteButton>
      <WriteButton perm={PERM} size="sm" onClick={() => sync.mutate(id)}>
        {t('common:write.syncNow')}
      </WriteButton>
    </>
  )
}
