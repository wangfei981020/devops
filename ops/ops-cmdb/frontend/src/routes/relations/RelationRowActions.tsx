import { toErrorInfo } from '@ops/api'
import { tError, useTranslation } from '@ops/i18n'
import { Banner, Dialog, MutationError } from '@ops/ui'
import { Trash2 } from 'lucide-react'
import { useState } from 'react'
import { WriteButton } from '../../components/WriteButton.js'
import { type Relation, useDeleteRelation } from './queries.js'

// 后端 perm.go 把关系图谱的写操作归在 cmdb:manage_basic 下（菜单码另有 menu:cmdb_relations）
const PERM = 'cmdb:manage_basic'

/**
 * 删一条关系。
 *
 * ⚠️ 采集推出来的边删了会**在下一轮同步时回来** —— 那不是"删不掉"，
 * 是这条边本来就是从现实推出来的。要它不回来，得改现实（或者把推断规则改掉）。
 * 弹窗里按来源分别说明，否则人会反复删同一条边然后以为系统坏了。
 */
export function RelationRowActions({ r }: { r: Relation }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const del = useDeleteRelation()
  const manual = r.origin === 'manual'

  return (
    <div className="flex items-center justify-end">
      <button
        type="button"
        aria-label={t('common:action.delete')}
        title={t('common:action.delete')}
        onClick={() => setOpen(true)}
        className="flex size-7 cursor-pointer items-center justify-center rounded-[var(--radius)] text-muted-foreground transition-colors duration-150 hover:bg-danger-bg hover:text-danger"
      >
        <Trash2 className="size-3.5" />
      </button>

      {open ? (
        <Dialog
          open
          onClose={() => setOpen(false)}
          title={t('relations:del.title')}
          description={`${r.src_name} → ${r.dst_name}`}
          closeLabel={t('common:action.close')}
          width={480}
          footer={
            <>
              <WriteButton perm={PERM} size="sm" onClick={() => setOpen(false)}>
                {t('common:action.cancel')}
              </WriteButton>
              <WriteButton
                perm={PERM}
                variant="danger"
                size="sm"
                loading={del.isPending}
                onClick={() => del.mutate(r.id, { onSuccess: () => setOpen(false) })}
              >
                {t('common:action.delete')}
              </WriteButton>
            </>
          }
        >
          <Banner tone={manual ? 'warn' : 'info'}>
            <span>{t(manual ? 'relations:del.manualNote' : 'relations:del.syncNote')}</span>
          </Banner>
          {del.isError ? (
            <MutationError error={del.error} toInfo={toErrorInfo} t={(k, p) => tError(t, k, p)} />
          ) : null}
        </Dialog>
      ) : null}
    </div>
  )
}
