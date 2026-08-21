import { useTranslation } from '@ops/i18n'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { ApiError, api } from '../api/client.js'
import type { ListOf } from '../api/types.js'

interface Group {
  id: number
  code: string
  name: string
  description?: string
  sort_order?: number
}

/**
 * 应用分组。
 *
 * # 它同时是两样东西
 *
 *   1. 门户里的**分栏**（一个应用只出现在主分组那一栏）
 *   2. 授权规则的**作用域**（「分组级」规则对整组生效）
 *
 * 第 2 条是删除时要小心的原因：删掉一个分组，挂在它上面的授权规则一起没，
 * 一批人当场失去访问。所以删之前先问接口"会连带删几条"，把数字摆出来再让人确认。
 */
export function AppGroups() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [name, setName] = useState('')
  const [code, setCode] = useState('')
  const [err, setErr] = useState<string | null>(null)
  const [confirming, setConfirming] = useState<Group | null>(null)

  const q = useQuery({
    queryKey: ['app-groups'],
    queryFn: () => api.get<ListOf<Group>>('/app-groups'),
  })

  const create = useMutation({
    mutationFn: () => api.post('/app-groups', { code: code.trim(), name: name.trim() }),
    onSuccess: () => {
      setErr(null)
      setName('')
      setCode('')
      void qc.invalidateQueries({ queryKey: ['app-groups'] })
      // 应用列表里显示的是分组名，建完要一起刷
      void qc.invalidateQueries({ queryKey: ['apps'] })
    },
    onError: (e) =>
      setErr(
        e instanceof ApiError
          ? t(`errors.${e.code}`, { ...e.params, defaultValue: e.code })
          : t('error.unreachable'),
      ),
  })

  return (
    <section className="rounded-[var(--radius-md)] border border-border bg-card p-4">
      <h2 className="text-sm font-semibold">{t('sso:groups.title')}</h2>
      <p className="mt-1 mb-3 max-w-[80ch] text-[12px] text-muted-foreground">
        {t('sso:groups.desc')}
      </p>

      <div className="mb-3 flex flex-wrap gap-1.5">
        {(q.data?.items ?? []).map((g) => (
          <span
            key={g.id}
            className="flex items-center gap-1.5 rounded-[var(--radius-sm)] border border-border px-2 py-1 text-[12px]"
          >
            {g.name}
            <span className="font-mono text-[10px] text-muted-foreground">{g.code}</span>
            <button
              type="button"
              onClick={() => setConfirming(g)}
              title={t('sso:groups.delete')}
              className="cursor-pointer text-muted-foreground hover:text-danger"
            >
              ×
            </button>
          </span>
        ))}
        {q.data && q.data.items.length === 0 ? (
          <span className="text-[12px] text-muted-foreground">{t('sso:groups.none')}</span>
        ) : null}
        {q.isError ? (
          // 读不出来 ≠ 没有分组。前者别渲染成"一个都没有"，
          // 那会让人去建一个已经存在的分组，然后撞上编码冲突。
          <span className="text-[12px] text-warning">{t('sso:groups.unavailable')}</span>
        ) : null}
      </div>

      <div className="flex flex-wrap items-end gap-2">
        <label className="text-[12px]">
          <span className="mb-1 block text-muted-foreground">{t('sso:groups.fName')}</span>
          <input
            value={name}
            onChange={(e) => setName(e.target.value)}
            className="w-40 rounded-[var(--radius)] border border-border bg-background px-2.5 py-1.5 text-[13px]"
          />
        </label>
        <label className="text-[12px]">
          <span className="mb-1 block text-muted-foreground">{t('sso:groups.fCode')}</span>
          <input
            value={code}
            onChange={(e) => setCode(e.target.value.toLowerCase().replace(/[^a-z0-9_-]/g, ''))}
            placeholder="infra"
            className="w-40 rounded-[var(--radius)] border border-border bg-background px-2.5 py-1.5 text-[13px]"
          />
        </label>
        <button
          type="button"
          disabled={name.trim() === '' || code.trim() === '' || create.isPending}
          onClick={() => create.mutate()}
          className="cursor-pointer rounded-[var(--radius)] border border-border px-3 py-1.5 text-[13px] hover:bg-secondary disabled:cursor-default disabled:opacity-50"
        >
          {create.isPending ? t('sso:groups.adding') : t('sso:groups.add')}
        </button>
      </div>

      {err ? (
        <p className="mt-2 rounded-[var(--radius)] bg-danger-bg px-2.5 py-1.5 text-[12px] text-danger">
          {err}
        </p>
      ) : null}

      {confirming ? (
        <DeleteConfirm
          group={confirming}
          onClose={() => setConfirming(null)}
          onDeleted={() => {
            setConfirming(null)
            void qc.invalidateQueries({ queryKey: ['app-groups'] })
            void qc.invalidateQueries({ queryKey: ['apps'] })
            void qc.invalidateQueries({ queryKey: ['policies'] })
          }}
        />
      ) : null}
    </section>
  )
}

/**
 * 删除确认。
 *
 * **先问影响面再删**：删掉分组会连带删掉挂在它上面的授权规则，
 * 一批人当场失去访问。删完才告诉人家「顺带删了 12 条规则」，
 * 那时候已经来不及了。
 */
function DeleteConfirm({
  group,
  onClose,
  onDeleted,
}: {
  group: Group
  onClose: () => void
  onDeleted: () => void
}) {
  const { t } = useTranslation()
  const impact = useQuery({
    queryKey: ['group-impact', group.id],
    queryFn: () => api.get<{ affected_policies: number }>(`/app-groups/${group.id}/impact`),
  })
  const del = useMutation({
    mutationFn: () => api.del(`/app-groups/${group.id}`),
    onSuccess: onDeleted,
  })

  return (
    <div className="fixed inset-0 z-30 grid place-items-center bg-overlay p-4">
      <div className="w-full max-w-md rounded-[var(--radius-md)] border border-border bg-card p-5">
        <h3 className="text-sm font-semibold">{t('sso:groups.confirmTitle', { name: group.name })}</h3>

        {impact.isPending ? (
          <p className="mt-3 text-[12px] text-muted-foreground">{t('sso:groups.checking')}</p>
        ) : impact.isError ? (
          // 影响面查不出来时**不给删**：这个确认框的全部意义就是让人看到代价，
          // 看不到代价的确认框只是一个多余的点击。
          <p className="mt-3 rounded-[var(--radius)] border border-warning bg-warning-bg px-3 py-2 text-[12px]">
            {t('sso:groups.impactUnavailable')}
          </p>
        ) : (
          <p
            className={[
              'mt-3 rounded-[var(--radius)] px-3 py-2 text-[12px]',
              (impact.data?.affected_policies ?? 0) > 0
                ? 'border border-destructive bg-danger-bg'
                : 'bg-muted',
            ].join(' ')}
          >
            {(impact.data?.affected_policies ?? 0) > 0
              ? t('sso:groups.impactSome', { n: impact.data?.affected_policies })
              : t('sso:groups.impactNone')}
          </p>
        )}

        {del.isError ? (
          <p className="mt-2 rounded-[var(--radius)] bg-danger-bg px-2.5 py-1.5 text-[12px] text-danger">
            {t('sso:groups.deleteFailed')}
          </p>
        ) : null}

        <div className="mt-4 flex gap-2">
          <button
            type="button"
            onClick={onClose}
            className="flex-1 cursor-pointer rounded-[var(--radius)] border border-border px-3 py-1.5 text-[13px] hover:bg-secondary"
          >
            {t('sso:groups.cancel')}
          </button>
          <button
            type="button"
            disabled={impact.isPending || impact.isError || del.isPending}
            onClick={() => del.mutate()}
            className="flex-1 cursor-pointer rounded-[var(--radius)] bg-destructive px-3 py-1.5 text-[13px] font-medium text-primary-foreground disabled:cursor-default disabled:opacity-50"
          >
            {del.isPending ? t('sso:groups.deleting') : t('sso:groups.confirmDelete')}
          </button>
        </div>
      </div>
    </div>
  )
}
