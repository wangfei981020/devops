import { toErrorInfo } from '@ops/api'
import { tError, useTranslation } from '@ops/i18n'
import { Badge, Banner, Dialog, Field, MutationError, SearchInput, Select } from '@ops/ui'
import { useState } from 'react'
import { WriteButton } from '../../components/WriteButton.js'
import { type CI, useCIs } from './ciQueries.js'
import { useCreateRelation } from './queries.js'

// 后端 perm.go 把关系图谱的写操作归在 cmdb:manage_basic 下（菜单码另有 menu:cmdb_relations）
const PERM = 'cmdb:manage_basic'

/** 与后端一致的关系类型。⚠️ 原样照搬，不加解释性括号 */
const REL_TYPES = ['related', 'protects', 'depends_on', 'runs_on', 'routes_to']

/**
 * 手工建一条关系。
 *
 * ⚠️ 图谱上大部分边是采集推出来的。手工边是给推不出来的那些用的 ——
 * 跨系统依赖、外部服务。它们在列表里标成「人工登记」，
 * 因为处置方式不同：推断出来的边下一轮同步可能就没了，人工的不会。
 */
export function RelationDialog({ onClose }: { onClose: () => void }) {
  const { t } = useTranslation()
  const create = useCreateRelation()
  const [relType, setRelType] = useState('related')
  const [src, setSrc] = useState<CI | null>(null)
  const [dst, setDst] = useState<CI | null>(null)

  const blocked = !src
    ? t('relations:form.needSrc')
    : !dst
      ? t('relations:form.needDst')
      : src.id === dst.id
        ? t('relations:form.sameCi')
        : undefined

  return (
    <Dialog
      open
      onClose={onClose}
      title={t('relations:form.title')}
      description={t('relations:form.desc')}
      closeLabel={t('common:action.close')}
      width={620}
      footer={
        <>
          <WriteButton perm={PERM} size="sm" onClick={onClose}>
            {t('common:action.cancel')}
          </WriteButton>
          <WriteButton
            perm={PERM}
            variant="primary"
            size="sm"
            loading={create.isPending}
            blockedReason={blocked}
            onClick={() =>
              src &&
              dst &&
              create.mutate(
                { src_ci_id: src.id, dst_ci_id: dst.id, rel_type: relType },
                { onSuccess: onClose },
              )
            }
          >
            {t('common:action.save')}
          </WriteButton>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        <div className="grid grid-cols-2 gap-3">
          <CIPicker label={t('relations:column.src')} value={src} onPick={setSrc} />
          <CIPicker label={t('relations:column.dst')} value={dst} onPick={setDst} />
        </div>
        <Field label={t('relations:column.relType')} hint={t('relations:form.relTypeHint')}>
          <Select
            label={t('relations:column.relType')}
            value={relType}
            onChange={setRelType}
            options={REL_TYPES.map((x) => ({
              value: x,
              label: t(`relations:rel_type.${x}`, { defaultValue: x }),
            }))}
          />
        </Field>
        {/* 方向是有意义的：A protects B 和 B protects A 不是一回事 */}
        <span className="text-xs text-muted-foreground">
          {t('relations:form.direction', {
            src: src?.name ?? '…',
            rel: relType,
            dst: dst?.name ?? '…',
          })}
        </span>
        {create.isError ? (
          <MutationError error={create.error} toInfo={toErrorInfo} t={(k, p) => tError(t, k, p)} />
        ) : null}
      </div>
    </Dialog>
  )
}

/**
 * 按名字搜一个 CI。
 *
 * ⚠️ 不预加载全量列表：CI 是所有对象的底座，几千条起步，
 * 一个装着几千项的下拉既卡又选不出来。搜到再选。
 */
function CIPicker({
  label,
  value,
  onPick,
}: {
  label: string
  value: CI | null
  onPick: (c: CI) => void
}) {
  const { t } = useTranslation()
  const [kw, setKw] = useState('')
  // 空关键词不查 —— 那会拉回全表
  const list = useCIs({ q: kw.trim() }, kw.trim().length >= 2)

  return (
    <Field label={label} hint={t('relations:form.pickHint')}>
      <div className="flex flex-col gap-1.5">
        <SearchInput
          value={kw}
          onChange={setKw}
          placeholder={t('relations:form.searchCi')}
          clearLabel={t('common:filter.clearSearch')}
        />
        {value ? (
          <span className="flex items-center gap-1.5 text-[13px]">
            <Badge tone="mute">{value.type}</Badge>
            <span className="truncate text-foreground">{value.name}</span>
          </span>
        ) : null}
        {kw.trim().length >= 2 ? (
          <div className="max-h-[168px] overflow-auto rounded-[var(--radius)] border border-border">
            {list.isPending ? (
              <p className="p-2 text-xs text-muted-foreground">{t('common:state.loading')}</p>
            ) : list.isError ? (
              <p className="p-2 text-xs text-danger">{toErrorInfo(list.error).detail}</p>
            ) : (list.data ?? []).length === 0 ? (
              // ⚠️ 搜不到 ≠ 不存在：CI 名字和界面上显示的名字可能不一样
              <p className="p-2 text-xs text-muted-foreground">{t('relations:form.noMatch')}</p>
            ) : (
              (list.data ?? []).slice(0, 50).map((c) => (
                <button
                  key={c.id}
                  type="button"
                  onClick={() => onPick(c)}
                  className="flex w-full cursor-pointer items-center gap-2 px-2 py-1.5 text-left text-[13px] hover:bg-secondary"
                >
                  <Badge tone="mute">{c.type}</Badge>
                  <span className="min-w-0 flex-1 truncate text-foreground">{c.name}</span>
                  {c.env ? <span className="text-xs text-muted-foreground">{c.env}</span> : null}
                </button>
              ))
            )}
          </div>
        ) : null}
      </div>
    </Field>
  )
}
