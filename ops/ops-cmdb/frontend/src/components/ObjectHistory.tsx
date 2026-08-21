import { toErrorInfo } from '@ops/api'
import { shouldRetry } from '@ops/api'
import { Badge, Banner, Button, Dialog, Skeleton } from '@ops/ui'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from '@ops/i18n'
import { apiGet } from '../lib/fetchJson.js'

/**
 * 单个对象的字段级变更历史。
 *
 * # 它回答的问题
 *
 * 「这个数据源/集群**昨天还好好的**，今天怎么查不到数据了？」
 *
 * 审计日志页有全量记录，但那是**按时间**排的一大列表 —— 要在里面找出
 * 「谁动过这一条」得先知道动的时刻。而人来问的时候恰恰只知道对象、不知道时刻。
 *
 * ⚠️ 后端 `/api/audit-history` 一直都在（`table` + `pk`），
 * 只是从来没有页面调过它（OPSCMDB-023 第三档）。
 *
 * # ⚠️ 三态
 *
 *   查询失败   → 显示失败。**不能**退化成"没有变更"，
 *               否则「查不了」会被读成「没人动过」，而这两者的结论完全相反
 *   确实没有   → 明说"没有记录到变更"，并点出它的边界（只记经审计的写操作）
 *   有         → 逐条列出谁、什么时候、改了哪些字段
 */
export interface ObjectChange {
  change_id?: number
  audit_id?: number
  op?: string
  /**
   * 字段名 → 变化。
   *
   * 🔴 键名是 `old` / `new`，**不是** `before` / `after`。
   *
   *	我第一版写的就是 before/after —— TypeScript 不报错、运行时不抛异常，
   *	只是每次取值都得到 undefined，于是**每一条都渲染成「— → —」**。
   *	数据在库里好好躺着，界面上却把「把 URL 从 A 改成 B」显示成「从无到无」。
   *	⚠️ 这比"没有详情"更坏：没有详情时人会去查库，
   *	显示成「—→—」时人会以为这条记录本来就没值。
   *
   *	⚠️ 这个坑仓库里**已经栽过一次**，还专门写了 check-audit-diff-keys 守卫，
   *	而我把类型声明写在新文件里就绕过去了（守卫只盯 routes/audit/queries.ts）。
   *	守卫已同步放宽到全前端扫描。
   *
   * 第三种形状：`{ changed: true }` —— 敏感字段（凭据）**只说改过、不给值**。
   * 这是有意的，不能渲染成「—→—」，那看起来像没改。
   */
  diff?: Record<string, { old?: unknown; new?: unknown; changed?: boolean }>
  revert_kind?: string
  username?: string
  action?: string
  at?: string
}

function useObjectHistory(table: string, pk: string, enabled: boolean) {
  return useQuery({
    queryKey: ['object-history', table, pk],
    enabled,
    queryFn: () =>
      apiGet<{ list: ObjectChange[] }>(
        `/api/audit-history?table=${encodeURIComponent(table)}&pk=${encodeURIComponent(pk)}&limit=20`,
      ),
    retry: shouldRetry,
  })
}

export function ObjectHistoryDialog({
  table,
  pk,
  title,
  onClose,
}: {
  /** 审计表里的 table_name，如 obs_endpoints / k8s_clusters / registrars */
  table: string
  /** 这一行的主键 */
  pk: string
  /** 给人看的对象名 */
  title: string
  onClose: () => void
}) {
  const { t } = useTranslation()
  const q = useObjectHistory(table, pk, true)
  const list = q.data?.list ?? []

  return (
    <Dialog
      open
      onClose={onClose}
      title={t('audit:objectHistory.title', { name: title })}
      description={t('audit:objectHistory.desc')}
      closeLabel={t('common:action.close')}
      width={820}
      footer={
        <Button size="sm" onClick={onClose}>
          {t('common:action.close')}
        </Button>
      }
    >
      {q.isPending ? (
        <Skeleton className="h-5 w-[50%]" />
      ) : q.isError ? (
        // 🔴 失败必须显示成失败。退化成"没有变更"的话，
        //	「查不了」会被读成「没人动过」—— 而这两者的结论完全相反
        <Banner tone="bad">
          <span className="font-medium">{t('audit:objectHistory.loadFailed')}</span>
          <span className="mt-0.5 block break-all">
            {toErrorInfo(q.error).detail || t(toErrorInfo(q.error).messageKey)}
          </span>
        </Banner>
      ) : list.length === 0 ? (
        <div className="py-2">
          <p className="text-[13px] text-foreground">{t('audit:objectHistory.none')}</p>
          {/* 边界要说清楚：只记经过审计的写操作。定时任务采集回来的字段变化
              不在这里，否则会被读成"这个对象从来没变过" */}
          <p className="mt-1 text-xs text-muted-foreground">
            {t('audit:objectHistory.noneHint')}
          </p>
        </div>
      ) : (
        <ul className="flex flex-col">
          {list.map((c) => (
            <ChangeRow key={c.change_id} c={c} t={t} />
          ))}
        </ul>
      )}
    </Dialog>
  )
}

function ChangeRow({
  c,
  t,
}: {
  c: ObjectChange
  t: (k: string, o?: Record<string, unknown>) => string
}) {
  const fields = Object.entries(c.diff ?? {})
  return (
    <li className="border-b border-border py-2 last:border-0">
      <div className="flex flex-wrap items-baseline gap-2 text-xs">
        <span className="tabular text-muted-foreground">{c.at}</span>
        <span className="font-medium text-foreground">{c.username || t('common:state.unknown')}</span>
        <Badge tone="mute">{c.op || c.action}</Badge>
        {/* 已回滚的要标出来：否则会把一条"改了又改回去"读成"现在还是改后的值" */}
        {c.revert_kind ? <Badge tone="info">{c.revert_kind}</Badge> : null}
      </div>

      {fields.length === 0 ? (
        // ⚠️ 有这条变更、但 diff 是空的 —— 说明记了操作没记字段，
        //	不能显示成"什么都没改"（审计详情页栽过这个：diff 键名读错时
        //	整页渲染成「—→—」，看起来像"改了但值都一样"）
        <p className="mt-0.5 text-xs text-warning">{t('audit:objectHistory.noDiff')}</p>
      ) : (
        <dl className="mt-1 flex flex-col gap-0.5">
          {fields.map(([k, v]) => (
            <div key={k} className="flex gap-2 text-xs">
              <dt className="w-[140px] shrink-0 truncate font-mono text-muted-foreground" title={k}>
                {k}
              </dt>
              <dd className="min-w-0 flex-1 break-all">
                {v?.changed ? (
                  // 敏感字段（凭据）只记"改过"不记值 —— 这是有意的安全设计，
                  // 必须说出来，否则渲染成「—→—」看起来像没改
                  <span className="text-warning">{t('audit:objectHistory.changedOnly')}</span>
                ) : (
                  <>
                    <span className="text-muted-foreground line-through">{fmt(v?.old)}</span>
                    <span className="mx-1 text-muted-foreground">→</span>
                    <span className="text-foreground">{fmt(v?.new)}</span>
                  </>
                )}
              </dd>
            </div>
          ))}
        </dl>
      )}
    </li>
  )
}

/**
 * 渲染一个 diff 值。
 *
 * ⚠️ `null` / `undefined` / 空串是三件事，不能都渲染成「—」：
 *   undefined  这次变更没有这个字段
 *   null       字段被清成了 NULL
 *   ''         被清成了空串
 * 分不开的话，「把值删了」和「这个字段没参与这次变更」看起来一样。
 */
function fmt(v: unknown): string {
  if (v === undefined) return '—'
  if (v === null) return 'NULL'
  if (v === '') return '(空串)'
  if (typeof v === 'object') return JSON.stringify(v)
  return String(v)
}
