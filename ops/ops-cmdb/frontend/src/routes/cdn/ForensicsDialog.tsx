import { toErrorInfo } from '@ops/api'
import { Badge, Banner, Button, Dialog, Field, Select, Skeleton, TextInput } from '@ops/ui'
import { useState } from 'react'
import {
  type CdnSecurityResult,
  type CdnTrafficResult,
  useCdnSecurityEvents,
  useCdnTraffic,
} from './queries.js'

type TFn = (k: string, o?: Record<string, unknown>) => string

/**
 * CDN 实时取证：逐条请求时序 + 安全事件。
 *
 * # 🔴 为什么这个功能值得做
 *
 * 对方说「请求早就发出去了，你们很久才收到」时，双方各执一词 ——
 * 因为**中间那段没有任何一方看得见**：我方只有"应用读到的时刻"，
 * 对方只有"自己发出的时刻"，中间经过公网和 CF 的那段是黑箱。
 *
 * `datetime_cst`（CF 边缘收到该请求的时刻）就是缺失的那个锚点。
 * 三个时刻一比，总延迟立刻切成两段，责任分得清清楚楚。
 *
 * # ⚠️ 两条纪律
 *
 * 1. **实时打 CF，不读快照** —— 所以只在用户点「查询」时请求，
 *    不预取、不轮询、不随弹窗打开自动跑（与 Token 体检不同：
 *    那个没有参数、开箱即用；这个必须先选站点和条件）
 * 2. **空结果不等于没有请求** —— 该数据集按套餐有采样与保留期限制，
 *    查询窗口超出保留期同样返回空。后端给了 `empty_meaning`，原样显示
 */
export function CdnForensicsDialog({
  zones,
  onClose,
  t,
}: {
  zones: string[]
  onClose: () => void
  t: TFn
}) {
  const [tab, setTab] = useState<'traffic' | 'security'>('traffic')
  const [zone, setZone] = useState(zones[0] ?? '')
  const [host, setHost] = useState('')
  const [path, setPath] = useState('')
  const [clientIP, setClientIP] = useState('')
  const [minutes, setMinutes] = useState('60')

  const traffic = useCdnTraffic()
  const security = useCdnSecurityEvents()
  const running = traffic.isPending || security.isPending

  const run = () => {
    if (!zone) return
    if (tab === 'traffic') {
      traffic.mutate({ zone, host, path, client_ip: clientIP, minutes: Number(minutes) })
    } else {
      security.mutate({ zone, host, minutes: Number(minutes) })
    }
  }

  return (
    <Dialog
      open
      onClose={onClose}
      title={t('cdn:forensics.title')}
      description={t('cdn:forensics.desc')}
      closeLabel={t('common:action.close')}
      width={1000}
      footer={
        <>
          <Button
            size="sm"
            variant="primary"
            loading={running}
            disabled={!zone}
            onClick={run}
          >
            {t('cdn:forensics.query')}
          </Button>
          <Button size="sm" onClick={onClose}>
            {t('common:action.close')}
          </Button>
        </>
      }
    >
      <div className="mb-3 flex gap-1.5">
        {(['traffic', 'security'] as const).map((k) => (
          <button
            key={k}
            type="button"
            onClick={() => setTab(k)}
            className={`cursor-pointer rounded-[var(--radius)] px-2.5 py-1 text-xs ${
              tab === k
                ? 'bg-secondary text-foreground'
                : 'text-muted-foreground hover:bg-secondary'
            }`}
          >
            {t(`cdn:forensics.tab.${k}`)}
          </button>
        ))}
      </div>

      <div className="flex flex-wrap items-end gap-2.5 rounded-[var(--radius)] border border-border bg-secondary/40 p-3">
        <Field label={t('cdn:forensics.zone')} required>
          <Select<string>
            label=""
            value={zone}
            onChange={setZone}
            options={zones.map((z) => ({ value: z, label: z }))}
          />
        </Field>
        <Field label={t('cdn:forensics.host')} hint={t('cdn:forensics.hostHint')}>
          <TextInput value={host} onChange={(e) => setHost(e.target.value)} placeholder="api.example.com" />
        </Field>
        {tab === 'traffic' ? (
          <>
            <Field label={t('cdn:forensics.path')}>
              <TextInput value={path} onChange={(e) => setPath(e.target.value)} placeholder="/v1/pay" />
            </Field>
            <Field label={t('cdn:forensics.clientIP')}>
              <TextInput
                value={clientIP}
                onChange={(e) => setClientIP(e.target.value)}
                placeholder="203.0.113.10"
              />
            </Field>
          </>
        ) : null}
        <Field label={t('cdn:forensics.minutes')} hint={t('cdn:forensics.minutesHint')}>
          <TextInput
            type="number"
            value={minutes}
            onChange={(e) => setMinutes(e.target.value)}
            className="w-[100px]"
          />
        </Field>
      </div>

      <div className="mt-3">
        {tab === 'traffic' ? (
          <TrafficResult m={traffic} t={t} />
        ) : (
          <SecurityResult m={security} t={t} />
        )}
      </div>
    </Dialog>
  )
}

/** 查询还没跑过时的引导。⚠️ 不要留一片空白让人以为坏了 */
function NotRunYet({ t }: { t: TFn }) {
  return <p className="text-xs text-muted-foreground">{t('cdn:forensics.notRunYet')}</p>
}

function TrafficResult({ m, t }: { m: ReturnType<typeof useCdnTraffic>; t: TFn }) {
  if (m.isPending) return <Skeleton className="h-5 w-[60%]" />
  if (m.isError) {
    return (
      <Banner tone="bad">
        <span className="break-all">
          {toErrorInfo(m.error).detail || t(toErrorInfo(m.error).messageKey)}
        </span>
      </Banner>
    )
  }
  const d: CdnTrafficResult | undefined = m.data
  if (!d) return <NotRunYet t={t} />
  if (d.ok === false) {
    return (
      <Banner tone="bad">
        <span className="font-medium">{d.error}</span>
        {/* hint 里写着「权限不足需 Zone·Zone Analytics·Read，用 token 体检验」——
            这是下一步动作，比错误本身有用 */}
        {d.hint ? <span className="mt-0.5 block">{d.hint}</span> : null}
      </Banner>
    )
  }

  return (
    <div className="flex flex-col gap-2.5">
      {/* 🔴 「怎么读」必须显示在结果上方。
          这一段是整个功能的价值所在 —— 没有它，下面就只是一堆时间戳，
          而人不会自己想到"拿三个时刻去切两段" */}
      {d.how_to_read ? (
        <Banner tone="info">
          <span>{d.how_to_read}</span>
        </Banner>
      ) : null}

      <p className="text-xs text-muted-foreground">
        {t('cdn:forensics.summary', { total: d.total ?? 0, window: d.window_cst ?? '—' })}
      </p>

      {/* ⚠️ 空 ≠ 没有请求。后端把边界说清楚了，原样显示 */}
      {d.empty_meaning ? (
        <Banner tone="warn">
          <span>{d.empty_meaning}</span>
        </Banner>
      ) : null}
      {/* 截断必须说出来，否则看到的会被当成全部 */}
      {d.truncated ? (
        <Banner tone="warn">
          <span>{d.truncated}</span>
        </Banner>
      ) : null}

      {(d.requests ?? []).length > 0 ? <RecordTable rows={d.requests ?? []} /> : null}
    </div>
  )
}

function SecurityResult({ m, t }: { m: ReturnType<typeof useCdnSecurityEvents>; t: TFn }) {
  if (m.isPending) return <Skeleton className="h-5 w-[60%]" />
  if (m.isError) {
    return (
      <Banner tone="bad">
        <span className="break-all">
          {toErrorInfo(m.error).detail || t(toErrorInfo(m.error).messageKey)}
        </span>
      </Banner>
    )
  }
  const d: CdnSecurityResult | undefined = m.data
  if (!d) return <NotRunYet t={t} />
  if (d.ok === false) {
    return (
      <Banner tone="bad">
        <span>{d.error}</span>
      </Banner>
    )
  }

  return (
    <div className="flex flex-col gap-2.5">
      <p className="text-xs text-muted-foreground">
        {t('cdn:forensics.summary', { total: d.total ?? 0, window: d.window_cst ?? '—' })}
      </p>

      {/* 按动作/规则的汇总：先看"被什么拦了"，再决定要不要翻明细 */}
      {(d.by_action ?? []).length > 0 ? (
        <div className="flex flex-wrap gap-1.5">
          {(d.by_action ?? []).map((a) => (
            <Badge key={a.name} tone="warn">
              {a.name} × {a.count}
            </Badge>
          ))}
        </div>
      ) : null}
      {(d.by_rule ?? []).length > 0 ? (
        <div className="flex flex-wrap gap-1.5">
          {(d.by_rule ?? []).map((r) => (
            <Badge key={r.name} tone="mute">
              {r.name} × {r.count}
            </Badge>
          ))}
        </div>
      ) : null}

      {/* ⚠️ 空在这里尤其容易被误读成「一定没被拦过」。
          后端说清了两条边界（只有触发规则才有事件 / 有保留期），原样显示 */}
      {d.empty_meaning ? (
        <Banner tone="info">
          <span>{d.empty_meaning}</span>
        </Banner>
      ) : null}
      {d.truncated ? (
        <Banner tone="warn">
          <span>{d.truncated}</span>
        </Banner>
      ) : null}

      {(d.events ?? []).length > 0 ? <RecordTable rows={d.events ?? []} /> : null}
    </div>
  )
}

/**
 * 动态字段的记录表。
 *
 * ⚠️ 列是从数据里推出来的 —— 这两个接口的字段可以由调用方自定义（`fields` 参数），
 * 写死列名会让自定义字段查出来却显示不出来。
 *
 * ⚠️ `datetime_cst` 排最前：它是这张表存在的理由。
 */
function RecordTable({ rows }: { rows: Record<string, unknown>[] }) {
  const keys = [...new Set(rows.flatMap((r) => Object.keys(r)))]
  keys.sort((a, b) => {
    // 时刻列永远排最前，其余保持出现顺序
    const rank = (k: string) => (k === 'datetime_cst' ? 0 : k.includes('atetime') ? 1 : 2)
    return rank(a) - rank(b)
  })
  return (
    <div className="max-h-[46vh] overflow-auto rounded-[var(--radius)] border border-border">
      <table className="w-full text-[12px]">
        <thead className="sticky top-0 z-10 bg-card">
          <tr className="border-b border-border text-left text-[11px] text-muted-foreground">
            {keys.map((k) => (
              <th key={k} className="px-2 py-1.5 font-medium whitespace-nowrap">
                {k}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((r, i) => (
            // biome-ignore lint/suspicious/noArrayIndexKey: 这是一次性快照，行没有稳定 id
            <tr key={i} className="border-b border-border last:border-0">
              {keys.map((k) => (
                <td key={k} className="px-2 py-1 font-mono whitespace-nowrap">
                  {/* undefined 与空串要分开：前者是这条记录没有这个字段
                      （自定义 fields 时常见），后者是字段存在但值为空 */}
                  {r[k] === undefined ? (
                    <span className="text-muted-foreground">—</span>
                  ) : (
                    String(r[k])
                  )}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
