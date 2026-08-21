import { hintText } from '../../lib/hintText.js'
import { toErrorInfo } from '@ops/api'
import { tError, useTranslation } from '@ops/i18n'
import { Badge, Banner, Dialog, MutationError, Select, Skeleton } from '@ops/ui'
import { useState } from 'react'
import { WriteButton } from '../../components/WriteButton.js'
import { useBasicDicts } from '../basic/queries.js'
import { type NsProject, useAutoNsProjects, useNsProjects, useSetNsProject } from './queries.js'

const PERM = 'cmdb:manage_basic'

/**
 * 命名空间归属。
 *
 * 成本归属全靠它：没有归属的命名空间，钱会全部落进「未归属」——
 * 而「未归属」在成本报表上看起来像一个巨大的、无人负责的项目。
 *
 * ⚠️ 自动归属**默认只预览**（后端的设计，前端不绕过去）。
 * 一次点击改掉几十个命名空间的成本归属，必须先让人看到要改哪些。
 */
export function NsProjectDialog({
  clusterID,
  onClose,
}: {
  clusterID: number
  onClose: () => void
}) {
  const { t } = useTranslation()
  const list = useNsProjects(clusterID)
  const dicts = useBasicDicts()
  const setOne = useSetNsProject(clusterID)
  const auto = useAutoNsProjects(clusterID)
  const [previewed, setPreviewed] = useState(false)

  const projects = (dicts.data?.projects ?? []).map((p) => ({ value: p.name, label: p.name }))
  const items = list.data ?? []
  const unassigned = items.filter((i) => !i.project).length
  const preview = auto.data

  return (
    <Dialog
      open
      onClose={onClose}
      title={t('namespaces:nsproject.title')}
      description={t('namespaces:nsproject.desc')}
      closeLabel={t('common:action.close')}
      width={760}
      footer={
        <>
          <WriteButton perm={PERM} size="sm" onClick={onClose}>
            {t('common:action.close')}
          </WriteButton>
          {!previewed ? (
            <WriteButton
              perm={PERM}
              size="sm"
              loading={auto.isPending}
              blockedReason={unassigned > 0 ? undefined : t('namespaces:nsproject.allAssigned')}
              onClick={() => auto.mutate(false, { onSuccess: () => setPreviewed(true) })}
            >
              {t('namespaces:nsproject.autoPreview')}
            </WriteButton>
          ) : (
            <WriteButton
              perm={PERM}
              variant="primary"
              size="sm"
              loading={auto.isPending}
              blockedReason={
                preview?.will_apply ? undefined : t('namespaces:nsproject.nothingToApply')
              }
              onClick={() =>
                auto.mutate(true, { onSuccess: () => setPreviewed(false) })
              }
            >
              {t('namespaces:nsproject.autoApply', { count: preview?.will_apply ?? 0 })}
            </WriteButton>
          )}
        </>
      }
    >
      <div className="flex flex-col gap-3">
        {/* 一个项目都没有：自动归属根本无从匹配。后端会直接回一句说明，
            这里把它原样显示，并指向该去哪儿建 */}
        {preview?.error ? (
          <Banner tone="warn">
            {/* hint 里写着唯一的出路（"先到「基础配置 → 项目」建好项目"）——
                丢了就只剩一句"没法自动归属"。hint_key 优先，见 lib/hintText.ts */}
            <span className="font-medium">{preview.error}</span>
            <span className="mt-0.5 block">
              {hintText(t, {
                hint_key: preview.hint_key,
                hint: preview.hint,
              })}
            </span>
          </Banner>
        ) : null}

        {previewed && preview && !preview.error ? (
          <Banner tone="info">
            <span className="font-medium">
              {t('namespaces:nsproject.previewTitle', { count: preview.will_apply ?? 0 })}
            </span>
            <span className="mt-0.5 block">{t('namespaces:nsproject.previewHint')}</span>
          </Banner>
        ) : null}

        {auto.data?.applied != null && !previewed ? (
          <Banner tone="info">
            <span>{auto.data.msg}</span>
          </Banner>
        ) : null}

        {list.isPending ? (
          <Skeleton className="h-5 w-[50%]" />
        ) : (
          <div className="max-h-[420px] overflow-y-auto rounded-[var(--radius)] border border-border">
            <table className="w-full text-[13px]">
              <thead className="sticky top-0 z-10 bg-card">
                <tr className="border-b border-border text-left text-xs text-muted-foreground">
                  <th className="px-3 py-2 font-medium">{t('namespaces:nsproject.col.ns')}</th>
                  <th className="px-3 py-2 font-medium">{t('namespaces:nsproject.col.project')}</th>
                  <th className="px-3 py-2 font-medium">{t('namespaces:nsproject.col.suggest')}</th>
                </tr>
              </thead>
              <tbody>
                {/* 未归属的排前面：这一页存在的意义就是把它们消化掉 */}
                {[...items]
                  .sort((a, b) => Number(!!a.project) - Number(!!b.project))
                  .map((it) => (
                    <Row
                      key={it.name}
                      item={it}
                      projects={projects}
                      onSet={(project) => setOne.mutate({ namespace: it.name, project })}
                      t={t}
                    />
                  ))}
              </tbody>
            </table>
          </div>
        )}

        {setOne.isError ? (
          <MutationError error={setOne.error} toInfo={toErrorInfo} t={(k, p) => tError(t, k, p)} />
        ) : null}
      </div>
    </Dialog>
  )
}

function Row({
  item,
  projects,
  onSet,
  t,
}: {
  item: NsProject
  projects: { value: string; label: string }[]
  onSet: (project: string) => void
  t: (k: string, p?: Record<string, unknown>) => string
}) {
  return (
    <tr className="border-b border-border last:border-0">
      <td className="px-3 py-1.5 font-mono text-xs">{item.name}</td>
      <td className="px-3 py-1.5">
        <Select<string>
          label={t('namespaces:nsproject.col.project')}
          value={item.project}
          onChange={onSet}
          options={[
            { value: '', label: t('namespaces:nsproject.unassigned') },
            ...projects,
          ]}
        />
      </td>
      <td className="px-3 py-1.5">
        <div className="flex items-start gap-2">
          {item.suggest ? (
            <Badge tone="info">{item.suggest}</Badge>
          ) : (
            // 平台组件被显式标出来：硬归到业务项目会污染成本口径，
            // 显示成"暂无建议"会让人以为只是系统没算出来
            <Badge tone="mute">{t(`namespaces:nsproject.rule.${item.suggest_rule}`, {
              defaultValue: item.suggest_rule,
            })}</Badge>
          )}
          {/* 建议的**理由必须显示**：只给一个建议值，人无法判断该不该采纳 */}
          <span className="text-[11px] leading-relaxed text-muted-foreground">
            {item.suggest_reason}
          </span>
        </div>
      </td>
    </tr>
  )
}
