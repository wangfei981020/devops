import { useTranslation } from '@ops/i18n'
import { Badge, Banner, Button, Dialog } from '@ops/ui'
import { useMutation, useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { WriteButton } from '../components/WriteButton.js'
import { get, post } from '../lib/api.js'

/**
 * 旧规则导入。
 *
 * # 为什么是「先预检、再应用」两步
 *
 * 旧系统的规则不是逐字段等价的：cron 调度、按字段值路由、多命名空间、
 * @人 这些在新模型里要么形态不同、要么根本没有对应物。
 * 一步到位地"导入成功"会让人以为搬完了，而真相是有几条被悄悄改了语义 ——
 * 上线后表现为「这条规则怎么不响了」或「怎么全在响」。
 *
 * 所以预检**只算不写**，把每条的判定和注意事项摆出来：
 *   auto     可直接迁
 *   confirm  能迁，但有需要人决定的地方（默认不勾选）
 *   rewrite  翻译不了，必须重写
 *
 * ⚠️ confirm/rewrite 默认**不勾选**。默认全选等于把"需要人确认"退化成一句
 * 没人看的提示 —— 而那些提示里恰恰包括"这 19 个错误码原本是不告警的"。
 */

interface Translation {
  legacy_id: number
  name: string
  kind: string
  verdict: 'auto' | 'confirm' | 'rewrite'
  notes?: string[] | null
  draft?: {
    interval_sec?: number
    lookback_sec?: number
    severity?: string
    field?: string
  } | null
}

interface Preflight {
  total: number
  auto: number
  confirm: number
  rewrite: number
  items: Translation[]
}

const VERDICT_TONE = { auto: 'ok', confirm: 'warn', rewrite: 'bad' } as const

export function ImportRulesDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { t } = useTranslation()
  const datasources = useQuery({
    queryKey: ['datasources'],
    queryFn: () => get<{ items: { id: number; name: string; type: string }[] }>('/datasources'),
    enabled: open,
  })
  const [raw, setRaw] = useState('')
  const [parsed, setParsed] = useState<unknown[]>([])
  // 旧规则里的 loki_connection_id / es_connection_id 是**旧系统的**连接 ID，
  // 在新系统里没有对应物 —— 必须由人指定这批规则查哪个数据源，
  // 猜一个的后果是规则建出来了却查了错误的集群，而界面上一切正常
  const [dsID, setDsID] = useState<number | null>(null)
  const [result, setResult] = useState<Preflight | null>(null)
  const [picked, setPicked] = useState<Set<number>>(new Set())
  const [parseError, setParseError] = useState('')
  const [applied, setApplied] = useState<{ imported: number; note?: string } | null>(null)

  const preflight = useMutation({
    mutationFn: async () => {
      let rules: unknown
      try {
        rules = JSON.parse(raw)
      } catch (e) {
        // JSON 语法错误要就地说清楚，而不是丢给后端返回一个 400 ——
        // 那样用户看到的是"导入失败"，不知道是自己少了个逗号
        throw new Error(t('opsalert:import.badJson', { detail: String(e) }))
      }
      if (!Array.isArray(rules)) {
        throw new Error(t('opsalert:import.notArray'))
      }
      setParsed(rules as unknown[])
      return post<Preflight>('/import/preflight', { rules })
    },
    onSuccess: (d) => {
      setResult(d)
      setParseError('')
      // 只默认勾选 auto。confirm 要人逐条看过再勾
      setPicked(new Set(d.items.filter((i) => i.verdict === 'auto').map((i) => i.legacy_id)))
    },
    onError: (e: Error) => setParseError(e.message),
  })

  const apply = useMutation({
    mutationFn: () =>
      post<{ imported: number; total: number; note?: string }>('/import/apply', {
        datasource_id: dsID,
        rules: parsed,
        legacy_ids: [...picked],
      }),
    onSuccess: (d) => setApplied(d),
  })

  function toggle(id: number) {
    setPicked((s) => {
      const n = new Set(s)
      if (n.has(id)) n.delete(id)
      else n.add(id)
      return n
    })
  }

  function reset() {
    setRaw('')
    setResult(null)
    setPicked(new Set())
    setParseError('')
    setApplied(null)
  }

  const footer = (
    <div className="flex justify-end gap-2">
      <Button variant="ghost" onClick={() => (result ? reset() : onClose())}>
        {result ? t('opsalert:import.back') : t('opsalert:rules.cancel')}
      </Button>
      {!result ? (
        <Button loading={preflight.isPending} disabled={!raw.trim()} onClick={() => preflight.mutate()}>
          {t('opsalert:import.preflight')}
        </Button>
      ) : (
        <WriteButton
          perm="alert:import"
          loading={apply.isPending}
          blockedReason={
            picked.size === 0
              ? t('opsalert:import.pickNone')
              : dsID === null
                ? t('opsalert:import.pickDatasource')
                : undefined
          }
          onClick={() => apply.mutate()}
        >
          {t('opsalert:import.applyN', { count: picked.size })}
        </WriteButton>
      )}
    </div>
  )

  return (
    <Dialog
      open={open}
      onClose={() => {
        reset()
        onClose()
      }}
      title={t('opsalert:import.title')}
      description={t('opsalert:import.desc')}
      closeLabel={t('opsalert:rules.cancel')}
      width={860}
      footer={footer}
    >
      <div className="flex flex-col gap-3">
        {applied ? (
          <Banner tone="info">
            {t('opsalert:import.done', { count: applied.imported })}
            {applied.note ? ` ${applied.note}` : ''}
          </Banner>
        ) : null}

        {!result ? (
          <>
            <label className="text-xs text-muted-foreground" htmlFor="legacy-json">
              {t('opsalert:import.pasteHint')}
            </label>
            <textarea
              id="legacy-json"
              className="h-64 w-full rounded-md border border-border bg-background p-2 font-mono text-xs"
              value={raw}
              onChange={(e) => setRaw(e.target.value)}
              placeholder='[{"id":1,"name":"...","data_source_type":"loki"}]'
            />
            {parseError ? <Banner tone="bad">{parseError}</Banner> : null}
          </>
        ) : (
          <>
            <div className="flex flex-wrap items-center gap-2">
              <label className="text-xs text-muted-foreground" htmlFor="import-ds">
                {t('opsalert:import.datasource')}
              </label>
              <select
                id="import-ds"
                className="rounded-md border border-border bg-background px-2 py-1 text-xs"
                value={dsID ?? ''}
                onChange={(e) => setDsID(e.target.value ? Number(e.target.value) : null)}
              >
                <option value="">{t('opsalert:import.pickDatasource')}</option>
                {(datasources.data?.items ?? []).map((d) => (
                  <option key={d.id} value={d.id}>
                    {d.name}（{d.type}）
                  </option>
                ))}
              </select>
            </div>

            <div className="flex flex-wrap items-center gap-2 text-xs">
              <Badge tone="mute">{t('opsalert:import.total', { count: result.total })}</Badge>
              <Badge tone="ok">{t('opsalert:import.auto', { count: result.auto })}</Badge>
              <Badge tone="warn">{t('opsalert:import.confirm', { count: result.confirm })}</Badge>
              <Badge tone="bad">{t('opsalert:import.rewrite', { count: result.rewrite })}</Badge>
              <span className="text-muted-foreground">{t('opsalert:import.pickHint')}</span>
            </div>

            <div className="max-h-96 overflow-y-auto rounded-md border border-border">
              {result.items.map((it) => (
                <div key={it.legacy_id} className="border-b border-border p-3 last:border-b-0">
                  <div className="flex items-start gap-2">
                    <input
                      type="checkbox"
                      className="mt-1"
                      checked={picked.has(it.legacy_id)}
                      disabled={it.verdict === 'rewrite'}
                      onChange={() => toggle(it.legacy_id)}
                      aria-label={it.name}
                    />
                    <div className="min-w-0 flex-1">
                      <div className="flex flex-wrap items-center gap-2">
                        <span className="text-sm font-medium">{it.name}</span>
                        <Badge tone={VERDICT_TONE[it.verdict]}>
                          {t(`opsalert:import.verdict.${it.verdict}`)}
                        </Badge>
                        <span className="font-mono text-xs text-muted-foreground">{it.kind}</span>
                      </div>
                      {it.draft ? (
                        <div className="mt-1 font-mono text-xs text-muted-foreground">
                          {t('opsalert:import.draftLine', {
                            interval: it.draft.interval_sec ?? 0,
                            lookback: it.draft.lookback_sec ?? 0,
                            severity: it.draft.severity ?? '',
                          })}
                        </div>
                      ) : null}
                      {(it.notes ?? []).map((n, i) => (
                        <div key={i} className="mt-1 text-xs text-warning">
                          · {n}
                        </div>
                      ))}
                    </div>
                  </div>
                </div>
              ))}
            </div>
          </>
        )}
      </div>
    </Dialog>
  )
}
