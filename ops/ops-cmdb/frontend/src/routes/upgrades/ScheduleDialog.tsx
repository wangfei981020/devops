import { toErrorInfo } from '@ops/api'
import { tError, useTranslation } from '@ops/i18n'
import { Badge, Banner, Dialog, Field, MutationError, SearchInput, Select, Skeleton, TextInput } from '@ops/ui'
import { useState } from 'react'
import { WriteButton } from '../../components/WriteButton.js'
import {
  type ScheduleRow,
  useClearOverride,
  useOverrideSchedule,
  useVersionSchedule,
} from './schedule.js'

const PERM = 'cmdb:manage_upgrade'

/**
 * 官网版本排期表 + 人工覆盖（**表体，可嵌进任何容器**）。
 *
 * 覆盖存在的理由只有一个：**官网页面结构变了、解析出错时的兜底**。
 * 正常情况下不该动它 —— 所以默认只显示已覆盖的和近期的，
 * 全表几百行铺出来只会让人不知道该看哪。
 *
 * ⚠️ 抽成独立组件是因为它现在有两个入口（页面顶部按钮、集群详情弹窗的一档）。
 * 复制一份的话，"默认只看近期"这条规则迟早会在其中一份里被改掉而没人发现。
 */
export function ScheduleBody() {
  const { t } = useTranslation()
  const q = useVersionSchedule()
  const clear = useClearOverride()
  const [editing, setEditing] = useState<ScheduleRow | null>(null)
  const [showAll, setShowAll] = useState(false)
  // 版本搜索。全表 28 行、放开"只看需要关注的"之后更多，
  // 想确认"1.36 什么时候升"只能一行行扫 —— 加个筛比翻页有用得多
  const [kw, setKw] = useState('')
  // 🔴 光有搜索框不够：版本号要手打，打错一位（1.36 → 1.63）
  // 表格就空了，而空表看着和"这个版本没有排期"一模一样。
  // 下拉的选项直接来自数据本身，选不出不存在的版本
  const [ver, setVer] = useState('all')
  const [ch, setCh] = useState('all')

  const rows = q.data?.rows ?? []
  // 默认只看：人工覆盖过的 + 还没到期的（days >= 0）。
  // 全表包含很多已经过去的版本，对排期毫无用处
  const notable = rows.filter((r) => r.is_manual || (r.auto_upgrade_days ?? -1) >= 0)
  // ⚠️ 必须按自动升级日期排序。后端给的顺序是按版本行存的，
  // 界面上看起来就是 2024/2025/2026 跳来跳去，排停机窗口时根本没法读
  const base = showAll ? rows : notable

  // 下拉选项从**全量** rows 取，不从 base 取。
  // 从 base 取的话，"只看需要关注的"会让老版本从下拉里消失，
  // 而用户可能正是想查一个已经过期的版本当时是什么排期
  const allVersions = Array.from(new Set(rows.map((r) => r.minor_version).filter(Boolean)))
    .sort()
    .reverse()
  const allChannels = Array.from(new Set(rows.map((r) => r.channel).filter(Boolean))).sort()
  // 搜索同时匹配版本号与通道：输 "1.36" 找版本，输 "stable" 找通道。
  // ⚠️ 搜索必须作用在 base 上而不是 shown 上，否则"只看需要关注的"这个开关
  // 会在搜索态下失效 —— 两个筛选条件必须是叠加关系
  const kwTrim = kw.trim().toLowerCase()
  // 三个条件是**叠加**的：下拉精确筛，搜索框在结果里再模糊找。
  // 任何一个覆盖掉别的，都会让人以为筛选坏了
  const matched = base.filter((r) => {
    if (ver !== 'all' && r.minor_version !== ver) return false
    if (ch !== 'all' && r.channel !== ch) return false
    if (!kwTrim) return true
    return (
      (r.minor_version ?? '').toLowerCase().includes(kwTrim) ||
      (r.channel ?? '').toLowerCase().includes(kwTrim)
    )
  })
  const shown = [...matched].sort((a, b) =>
    (a.auto_upgrade_at ?? '').localeCompare(b.auto_upgrade_at ?? ''),
  )

  return (
    <>
      <div className="flex flex-col gap-3">
        <Banner tone="info">
          <span>{t('upgrades:schedule.precisionNote')}</span>
        </Banner>

        <div className="flex flex-wrap items-center gap-2">
          <Select<string>
            label={t('upgrades:schedule.version')}
            value={ver}
            onChange={setVer}
            options={[
              { value: 'all', label: t('common:filter.all') },
              ...allVersions.map((v) => ({ value: v as string, label: v as string })),
            ]}
          />
          <Select<string>
            label={t('upgrades:schedule.channel')}
            value={ch}
            onChange={setCh}
            options={[
              { value: 'all', label: t('common:filter.all') },
              ...allChannels.map((c) => ({ value: c as string, label: c as string })),
            ]}
          />
          <SearchInput
            value={kw}
            onChange={setKw}
            placeholder={t('upgrades:schedule.searchPlaceholder')}
            clearLabel={t('common:filter.clearSearch')}
            className="w-[260px]"
          />
          {/* ⚠️ 搜了但一条没中时必须说清是"搜索没中"，
              不能只剩一张空表 —— 空表会被读成"这个版本没有排期" */}
          {(kwTrim || ver !== 'all' || ch !== 'all') && shown.length === 0 ? (
            <span className="text-xs text-warning">
              {t('upgrades:schedule.noMatch', { kw: kw.trim() || `${ver}/${ch}` })}
            </span>
          ) : kwTrim || ver !== 'all' || ch !== 'all' ? (
            <span className="text-xs text-muted-foreground">
              {t('upgrades:schedule.matched', { count: shown.length })}
            </span>
          ) : null}
        </div>

        {q.isPending ? (
          <Skeleton className="h-5 w-[40%]" />
        ) : (
          <div className="max-h-[420px] overflow-y-auto rounded-[var(--radius)] border border-border">
            <table className="w-full text-[13px]">
              <thead className="sticky top-0 z-10 bg-card">
                <tr className="border-b border-border text-left text-xs text-muted-foreground">
                  <th className="px-3 py-2 font-medium">{t('upgrades:schedule.channel')}</th>
                  <th className="px-3 py-2 font-medium">{t('upgrades:schedule.version')}</th>
                  <th className="px-3 py-2 font-medium">{t('upgrades:schedule.autoAt')}</th>
                  <th className="px-3 py-2 font-medium">{t('upgrades:schedule.eos')}</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {shown.map((r) => (
                  <tr key={r.id} className="border-b border-border last:border-0">
                    <td className="px-3 py-1.5">
                      <Badge tone="mute">{r.channel}</Badge>
                    </td>
                    <td className="px-3 py-1.5 font-mono text-xs">{r.minor_version || '—'}</td>
                    <td className="px-3 py-1.5">
                      <span className="tabular">{r.auto_upgrade_raw || '—'}</span>
                      {/* 精度是关键信息不是脚注：月/季度粒度被当成确切日期会排错停机窗口 */}
                      {r.auto_upgrade_precision && r.auto_upgrade_precision !== 'day' ? (
                        <span className="ml-1.5 text-[11px] text-warning">
                          {t(`upgrades:precision.${r.auto_upgrade_precision}`, {
                            defaultValue: r.auto_upgrade_precision,
                          })}
                        </span>
                      ) : null}
                      {/* 被钉住的格子：官网后来改了也不会同步过来 */}
                      {r.is_manual ? (
                        <Badge tone="warn">{t('upgrades:schedule.manual')}</Badge>
                      ) : null}
                    </td>
                    <td className="px-3 py-1.5">
                      {/* 支持截止：过了这天 Google 不再修它的漏洞，比"何时被升"更硬 */}
                      <span className="tabular text-xs">{r.eos_standard_at || '—'}</span>
                      {(r.eos_standard_days ?? 0) < 0 ? (
                        <Badge tone="mute">{t('upgrades:schedule.eosPassed')}</Badge>
                      ) : null}
                    </td>
                    <td className="px-3 py-1.5 text-right">
                      <WriteButton perm={PERM} size="sm" onClick={() => setEditing(r)}>
                        {t('upgrades:schedule.override')}
                      </WriteButton>
                      {r.is_manual ? (
                        <WriteButton
                          perm={PERM}
                          size="sm"
                          loading={clear.isPending && clear.variables === r.id}
                          onClick={() => clear.mutate(r.id)}
                        >
                          {t('upgrades:schedule.clear')}
                        </WriteButton>
                      ) : null}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        <button
          type="button"
          onClick={() => setShowAll((v) => !v)}
          className="mr-auto cursor-pointer text-xs text-brand-text underline-offset-2 hover:underline"
        >
          {showAll
            ? t('upgrades:schedule.showNotable')
            : t('upgrades:schedule.showAll', { count: rows.length })}
        </button>
      </div>

      {editing ? <OverrideForm row={editing} onClose={() => setEditing(null)} /> : null}
    </>
  )
}

/** 独立入口：页面顶部那个按钮用。表体见 ScheduleBody。 */
export function ScheduleDialog({ onClose }: { onClose: () => void }) {
  const { t } = useTranslation()
  return (
    <Dialog
      open
      onClose={onClose}
      title={t('upgrades:schedule.title')}
      description={t('upgrades:schedule.desc')}
      closeLabel={t('common:action.close')}
      width={780}
      footer={
        <WriteButton perm={PERM} size="sm" onClick={onClose}>
          {t('common:action.close')}
        </WriteButton>
      }
    >
      <ScheduleBody />
    </Dialog>
  )
}

function OverrideForm({ row, onClose }: { row: ScheduleRow; onClose: () => void }) {
  const { t } = useTranslation()
  const save = useOverrideSchedule()
  const [raw, setRaw] = useState(row.auto_upgrade_raw ?? '')
  const [precision, setPrecision] = useState(row.auto_upgrade_precision || 'day')

  return (
    <Dialog
      open
      onClose={onClose}
      title={t('upgrades:schedule.overrideTitle')}
      description={t('upgrades:schedule.overrideDesc')}
      closeLabel={t('common:action.close')}
      footer={
        <>
          <WriteButton perm={PERM} size="sm" onClick={onClose}>
            {t('common:action.cancel')}
          </WriteButton>
          <WriteButton
            perm={PERM}
            variant="primary"
            size="sm"
            loading={save.isPending}
            blockedReason={raw ? undefined : t('upgrades:schedule.needDate')}
            onClick={() =>
              save.mutate(
                {
                  id: row.id,
                  auto_upgrade_raw: raw,
                  // 后端两个字段：raw 是原文（可能是 2026-Q4），at 是可比较的日期
                  auto_upgrade_at: raw,
                  auto_upgrade_precision: precision,
                },
                { onSuccess: onClose },
              )
            }
          >
            {t('common:write.save')}
          </WriteButton>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        <Field label={t('upgrades:schedule.autoAt')} hint={t('upgrades:schedule.rawHint')} required>
          <TextInput value={raw} onChange={(e) => setRaw(e.target.value)} placeholder="2026-09-15" />
        </Field>
        <div className="flex flex-col gap-1.5">
          <Select<string>
            label={t('upgrades:schedule.precision')}
            value={precision}
            onChange={setPrecision}
            options={['day', 'month', 'quarter'].map((p) => ({
              value: p,
              label: t(`upgrades:precision.${p}`, { defaultValue: p }),
            }))}
            className="self-start"
          />
          <span className="text-xs leading-relaxed text-muted-foreground">
            {t('upgrades:schedule.precisionHint')}
          </span>
        </div>
        <Banner tone="warn">
          <span>{t('upgrades:schedule.pinWarning')}</span>
        </Banner>
        {save.isError ? (
          <MutationError error={save.error} toInfo={toErrorInfo} t={(k, p) => tError(t, k, p)} />
        ) : null}
      </div>
    </Dialog>
  )
}
