import { actionMessage } from '../../lib/actionMessage.js'
import { toErrorInfo } from '@ops/api'
import { tError, useTranslation } from '@ops/i18n'
import { Banner, Dialog, MenuItem, MenuSeparator, MutationError, Popover } from '@ops/ui'
import { ListTree, MoreHorizontal, Pencil, RefreshCw, Share2, ShieldCheck, Trash2, Wallet } from 'lucide-react'
import { useState } from 'react'
import { WriteButton } from '../../components/WriteButton.js'
import { DomainDialog } from './DomainDialog.js'
import { DomainChainDialog } from './ChainDialog.js'
import { DnsRecordsDialog } from './DnsRecordsDialog.js'
import { DomainOpsDialog } from './DomainOpsDialog.js'
import {
  type Domain,
  useCheckAllCerts,
  useDeleteDomain,
  useRefreshDomain,
  useSyncDomainRecords,
} from './queries.js'

const PERM = 'cmdb:manage_domains'

type Open = null | 'edit' | 'ops' | 'dns' | 'chain' | 'delete'

/**
 * 单个域名的操作入口。
 *
 * # 为什么是菜单不是一排按钮
 *
 * 七个动作平铺会把表格挤没。而且这些动作的频率差着数量级：
 * 「续费」一年碰一次，「刷新」出问题时才点。高频的（看状态）已经在列上了。
 *
 * # ⚠️ 三类动作的处理必须不一样
 *
 * - **只读刷新**（refresh / sync-records / check-certs）：点了直接跑，
 *   结果就地显示。重跑一次没有代价。
 * - **花钱**（续费）：进 DomainOpsDialog，先看厂商侧状态再二次确认。
 * - **删除**：二次确认，且必须说清楚删的是**台账记录**不是域名本身 ——
 *   有人会以为这里能把域名从注册商那注销掉。
 */
export function DomainRowActions({ d }: { d: Domain }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState<Open>(null)
  const refresh = useRefreshDomain()
  const syncRecords = useSyncDomainRecords()
  const checkCerts = useCheckAllCerts()
  const del = useDeleteDomain()

  // 三个只读动作共用一条结果行：同一时刻人只会点其中一个
  const running = [refresh, syncRecords, checkCerts].find((m) => m.isPending)
  const failed = [refresh, syncRecords, checkCerts].find((m) => m.isError)
  const ok = [refresh, syncRecords, checkCerts].find((m) => m.isSuccess)

  return (
    <div className="flex items-center justify-end gap-2">
      {running ? (
        <span className="text-xs text-muted-foreground">{t('common:state.loading')}</span>
      ) : failed ? (
        <span className="max-w-[160px] truncate text-xs text-danger" title={toErrorInfo(failed.error).detail}>
          {tError(t, toErrorInfo(failed.error).messageKey, toErrorInfo(failed.error).params)}
        </span>
      ) : ok ? (
        <span className="text-xs text-success">{actionMessage(t, ok.data)}</span>
      ) : null}

      <Popover
        align="end"
        trigger={(p) => (
          <button
            type="button"
            {...p}
            aria-label={t('common:action.more')}
            className="flex size-7 cursor-pointer items-center justify-center rounded-[var(--radius)] text-muted-foreground transition-colors duration-150 hover:bg-secondary hover:text-foreground"
          >
            <MoreHorizontal className="size-4" />
          </button>
        )}
      >
        <div className="min-w-[184px] py-1">
          <MenuItem icon={<Pencil />} onClick={() => setOpen('edit')}>
            {t('common:action.edit')}
          </MenuItem>
          <MenuItem icon={<ListTree />} onClick={() => setOpen('dns')}>
            {t('domains:dns.entry')}
          </MenuItem>
          {/* 🔴 「这个域名背后到底跑着什么」—— 域名出事时的第一个问题，
              而回答它现在要跨四五个页面（解析看回源、CDN 页看边缘证书、
              服务与入口看 Ingress、Pod 页看副本）。
              后端 /api/k8s/topology 一直把整条链串好了，只是没人调
              （OPSCMDB-023 第一档） */}
          <MenuItem icon={<Share2 />} onClick={() => setOpen('chain')}>
            {t('domains:chain.entry')}
          </MenuItem>
          <MenuItem icon={<Wallet />} onClick={() => setOpen('ops')}>
            {t('domains:ops.entry')}
          </MenuItem>
          <MenuSeparator />
          <MenuItem icon={<RefreshCw />} onClick={() => refresh.mutate(d.ciId)}>
            {t('domains:action.refresh')}
          </MenuItem>
          <MenuItem icon={<RefreshCw />} onClick={() => syncRecords.mutate(d.ciId)}>
            {t('domains:action.syncRecords')}
          </MenuItem>
          <MenuItem icon={<ShieldCheck />} onClick={() => checkCerts.mutate(d.ciId)}>
            {t('domains:action.checkCerts')}
          </MenuItem>
          <MenuSeparator />
          <MenuItem icon={<Trash2 />} danger onClick={() => setOpen('delete')}>
            {t('common:action.delete')}
          </MenuItem>
        </div>
      </Popover>

      {open === 'edit' ? (
        <DomainDialog
          initial={{ ciId: d.ciId, name: d.name, expiry_at: d.expiryAt }}
          onClose={() => setOpen(null)}
        />
      ) : null}
      {open === 'ops' ? <DomainOpsDialog d={d} onClose={() => setOpen(null)} /> : null}
      {open === 'dns' ? <DnsRecordsDialog d={d} onClose={() => setOpen(null)} /> : null}
      {open === 'chain' ? (
        <DomainChainDialog domain={d.name} onClose={() => setOpen(null)} />
      ) : null}
      {open === 'delete' ? (
        <Dialog
          open
          onClose={() => setOpen(null)}
          title={t('domains:del.title')}
          description={t('domains:del.desc', { domain: d.name })}
          closeLabel={t('common:action.close')}
          width={460}
          footer={
            <>
              <WriteButton perm={PERM} size="sm" onClick={() => setOpen(null)}>
                {t('common:action.cancel')}
              </WriteButton>
              <WriteButton
                perm={PERM}
                variant="danger"
                size="sm"
                loading={del.isPending}
                onClick={() => del.mutate(d.ciId, { onSuccess: () => setOpen(null) })}
              >
                {t('common:action.delete')}
              </WriteButton>
            </>
          }
        >
          {/* ⚠️ 必须写清楚删的是台账记录。有人会以为这里能把域名从注册商注销 */}
          <Banner tone="warn">
            <span>{t('domains:del.note')}</span>
          </Banner>
          {del.isError ? (
            <MutationError error={del.error} toInfo={toErrorInfo} t={(k, p) => tError(t, k, p)} />
          ) : null}
        </Dialog>
      ) : null}
    </div>
  )
}
