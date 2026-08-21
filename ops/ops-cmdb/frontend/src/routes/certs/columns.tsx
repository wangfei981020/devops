import type { Locale } from '@ops/i18n'
import { Badge, type ColumnDef, NoValue, type NoValueKind } from '@ops/ui'
import { CertRowActions } from './CertRowActions.js'
import { type Cert, healthOf } from './queries.js'

type TFn = (key: string, opts?: Record<string, unknown>) => string

export function certColumns(
  t: TFn,
  _locale: Locale,
  labels: Record<NoValueKind, string>,
): ColumnDef<Cert>[] {
  return [
    {
      accessorKey: 'cn',
      header: t('certs:column.cn'),
      cell: ({ row }) => {
        const c = row.original
        // SAN 与 CN 相同时不重复显示 —— 绝大多数证书是这样，
        // 重复一遍只会让这一列变宽而不增加信息
        const extra = c.sans
          .split(',')
          .map((s) => s.trim())
          .filter((s) => s !== '' && s !== c.cn)
        return (
          <div className="flex min-w-0 flex-col">
            <span className="truncate font-medium text-foreground">{c.cn}</span>
            {/* 🔴 这里以前写的是 `extra.length > 0 ? extra.join(', ') : c.ca` ——
                SAN 和 CA 二选一。而绝大多数证书都有 SAN，
                于是「这张证书是谁签的」在列表上基本不可见（OPSCMDB-028 GAP-12）。
                旧版是两列并列，现在把 CA 拆成独立一列。 */}
            {extra.length > 0 ? (
              <span className="truncate text-xs text-muted-foreground" title={extra.join(', ')}>
                {extra.join(', ')}
              </span>
            ) : null}
          </div>
        )
      },
    },
    {
      // 签发者。换 CA、排查信任链、确认自签证书时都要看它 ——
      // 而它此前只在"这张证书没有 SAN"时才会出现。
      id: 'ca',
      header: t('certs:column.ca'),
      accessorKey: 'ca',
      cell: ({ row }) => {
        const ca = row.original.ca
        // 空 = 没解析出签发者。自签或解析失败都可能，两者都值得看见，
        // 所以显示成「未知」而不是空白 —— 空白会被当成"这一列还没做"
        return ca !== '' ? (
          <span className="truncate text-xs" title={ca}>
            {ca}
          </span>
        ) : (
          <span className="text-xs text-muted-foreground">{t('common:state.unknown')}</span>
        )
      },
    },
    {
      id: 'expiry',
      header: t('certs:column.expiry'),
      cell: ({ row }) => {
        const c = row.original
        if (c.daysLeft === null) {
          // 读不出到期日**不是正常**：它可能已经过期了，只是我们不知道。
          // 用警告色的 unknown，而不是灰色的"—"
          return <NoValue kind="unknown" labels={labels} />
        }
        const h = healthOf(c)
        const tone =
          h === 'expired' ? 'text-danger' : h === 'ok' ? 'text-muted-foreground' : 'text-warning'
        return (
          <div className="flex min-w-0 flex-col">
            <span className={`tabular text-[13px] ${tone}`}>
              {c.daysLeft < 0
                ? t('certs:expiredDaysAgo', { count: -c.daysLeft })
                : t('certs:daysLeft', { count: c.daysLeft })}
            </span>
            <span className="tabular text-xs text-muted-foreground">{c.expiryAt}</span>
            {/* 台账最后更新时刻。证书台账是**快照**：这一行说的是"我们上次看到的样子"。
                ⚠️ 不显示的话，一条三周没更新的记录和刚同步的长得一模一样 ——
                而"数据是旧的"和"数据是错的"在界面上没有区别。
                null = 从没更新过，与"刚更新"必须能分开。 */}
            {c.updatedAt ? (
              <span className="text-[11px] text-muted-foreground">
                {t('certs:updatedAt', { at: c.updatedAt })}
              </span>
            ) : (
              <span className="text-[11px] text-warning">{t('certs:neverUpdated')}</span>
            )}
          </div>
        )
      },
    },
    {
      id: 'renew',
      header: t('certs:column.renew'),
      cell: ({ row }) => {
        const c = row.original
        // ⚠️ 「自动续期 + 一直失败」是最危险的组合：只显示"自动续期"
        // 会让人放心，而它已经连续失败了。所以失败时不显示那个绿标
        if (c.lastError !== '') {
          return (
            <div className="flex min-w-0 flex-col items-start gap-0.5">
              <Badge tone="bad">{t('certs:renewFailing')}</Badge>
              <span className="truncate text-[11px] text-danger" title={c.lastError}>
                {c.lastError}
              </span>
            </div>
          )
        }
        return c.autoRenew ? (
          <Badge tone="ok">{t('certs:autoRenew')}</Badge>
        ) : (
          // 手动续期不是错误，但要看得见：到期时没有任何东西会自动救它
          <Badge tone="mute">{t('certs:manualRenew')}</Badge>
        )
      },
    },
    {
      id: 'health',
      header: t('certs:column.health'),
      cell: ({ row }) => {
        const h = healthOf(row.original)
        const tone =
          h === 'expired' || h === 'failing'
            ? 'bad'
            : h === 'unknown' || h === 'soon'
              ? 'warn'
              : 'ok'
        return <Badge tone={tone}>{t(`certs:health.${h}`)}</Badge>
      },
    },
    {
      id: 'actions',
      // 操作列固定在最右侧。⚠️ 标了就必须真的排在数组最后（check-action-column 守这条）
      meta: { action: true },
      header: '',
      cell: ({ row }) => <CertRowActions c={row.original} />,
    },
  ]
}
