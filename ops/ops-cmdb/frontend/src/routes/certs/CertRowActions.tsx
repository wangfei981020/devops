import { toErrorInfo } from '@ops/api'
import { tError, useTranslation } from '@ops/i18n'
import { Banner, Button, Dialog, MenuItem, MenuSeparator, MutationError, Popover } from '@ops/ui'
import { useQuery } from '@tanstack/react-query'
import { CheckCircle2, Download, MoreHorizontal, RefreshCw, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { WriteButton } from '../../components/WriteButton.js'
import { apiGet } from '../../lib/fetchJson.js'
import { type Cert, useCertDNSReady, useDeleteCert, useRenewCert } from './queries.js'

const PERM = 'cmdb:issue_cert'

type Open = null | 'dnsReady' | 'delete'

interface CertDetail {
  status?: string
  challenge?: string
  challenge_fqdn?: string
  challenge_value?: string
  last_error?: string
}

/** 单张证书的操作：立即续期 / DNS 验证放行 / 删除。 */
export function CertRowActions({ c }: { c: Cert }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState<Open>(null)
  const renew = useRenewCert()
  const del = useDeleteCert()

  return (
    <div className="flex items-center justify-end gap-2">
      {renew.isPending ? (
        <span className="text-xs text-muted-foreground">{t('common:state.loading')}</span>
      ) : renew.isError ? (
        <span className="max-w-[180px] truncate text-xs text-danger" title={toErrorInfo(renew.error).detail || undefined}>
          {tError(t, toErrorInfo(renew.error).messageKey, toErrorInfo(renew.error).params)}
        </span>
      ) : renew.isSuccess ? (
        <span className="text-xs text-success">{renew.data?.msg ?? t('common:write.saved')}</span>
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
        <div className="min-w-[192px] py-1">
          <MenuItem icon={<RefreshCw />} onClick={() => renew.mutate(c.ciId)}>
            {t('certs:action.renewNow')}
          </MenuItem>
          <MenuItem icon={<CheckCircle2 />} onClick={() => setOpen('dnsReady')}>
            {t('certs:action.dnsReady')}
          </MenuItem>
          <MenuSeparator />
          <MenuItem icon={<Trash2 />} danger onClick={() => setOpen('delete')}>
            {t('common:action.delete')}
          </MenuItem>
        </div>
      </Popover>

      {/*
        下载证书包。

        ⚠️ 这里原来用的是 `<MenuItem>`，但它渲染在 Popover **外面** ——
        一个菜单项漂在行里，占满整行宽度，所以才显得又长又怪。

        改成纯图标按钮：下载是这一行的主操作、只读、且 ⬇ 是公认图标，
        正好符合 iconOnly 的三个条件（见 Button 的 iconOnly 注释）。

        直接跳转不走 fetch —— 浏览器自己处理文件流最省事，
        也避免把整个证书内容读进内存。
        ⚠️ 这是**发凭据**的接口（zip 里含私钥），后端记审计。
      */}
      <Button
        size="sm"
        iconOnly
        icon={<Download className="size-3.5" />}
        aria-label={t('certs:action.download')}
        title={t('certs:action.downloadHint')}
        onClick={() => {
          window.location.href = `/api/certs/${c.ciId}/download`
        }}
      />
      {open === 'dnsReady' ? <DNSReadyDialog c={c} onClose={() => setOpen(null)} /> : null}
      {open === 'delete' ? (
        <Dialog
          open
          onClose={() => setOpen(null)}
          title={t('certs:del.title')}
          description={c.cn}
          closeLabel={t('common:action.close')}
          width={480}
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
                onClick={() => del.mutate(c.ciId, { onSuccess: () => setOpen(null) })}
              >
                {t('common:action.delete')}
              </WriteButton>
            </>
          }
        >
          {/* ⚠️ 后端那个函数叫 Revoke，但它只删本地记录。
              已签发的证书在有效期内**仍然被信任** —— 私钥泄露时
              有人会以为在这儿点一下就吊销了，那是致命的误解 */}
          <Banner tone="warn">
            <span className="font-medium">{t('certs:del.notRevoke.title')}</span>
            <span className="mt-0.5 block">{t('certs:del.notRevoke.body')}</span>
          </Banner>
          {del.isError ? (
            <MutationError error={del.error} toInfo={toErrorInfo} t={(k, p) => tError(t, k, p)} />
          ) : null}
        </Dialog>
      ) : null}
    </div>
  )
}

/**
 * DNS-01 手动验证放行。
 *
 * ⚠️ 绝不能只给一个「继续」按钮：点早了（TXT 还没生效）会验证失败，
 * 而失败要吃掉一次 CA 配额。所以先把要加的 TXT 记录**摆出来让人核对**，
 * 取不到记录时直接不给放行按钮 —— 不知道要加什么就不该说"我加好了"。
 */
function DNSReadyDialog({ c, onClose }: { c: Cert; onClose: () => void }) {
  const { t } = useTranslation()
  const ready = useCertDNSReady()
  const detail = useQuery({
    queryKey: ['cert-detail', c.ciId],
    queryFn: () => apiGet<CertDetail>(`/api/certs/${c.ciId}`),
    retry: false,
    refetchOnWindowFocus: false,
  })
  const d = detail.data
  const hasChallenge = Boolean(d?.challenge_fqdn && d?.challenge_value)

  return (
    <Dialog
      open
      onClose={onClose}
      title={t('certs:dnsReady.title')}
      description={c.cn}
      closeLabel={t('common:action.close')}
      width={560}
      footer={
        <>
          <WriteButton perm={PERM} size="sm" onClick={onClose}>
            {t('common:action.cancel')}
          </WriteButton>
          <WriteButton
            perm={PERM}
            variant="primary"
            size="sm"
            loading={ready.isPending}
            blockedReason={hasChallenge ? undefined : t('certs:dnsReady.noChallengeBlock')}
            onClick={() => ready.mutate(c.ciId, { onSuccess: onClose })}
          >
            {t('certs:dnsReady.confirm')}
          </WriteButton>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        {detail.isPending ? (
          <span className="text-[13px] text-muted-foreground">{t('common:state.loading')}</span>
        ) : null}
        {detail.isError ? (
          <MutationError error={detail.error} toInfo={toErrorInfo} t={(k, p) => tError(t, k, p)} />
        ) : null}

        {hasChallenge ? (
          <>
            <Banner tone="info">
              <span>{t('certs:dnsReady.hint')}</span>
            </Banner>
            <div className="flex flex-col gap-2 rounded-[var(--radius)] border border-border bg-secondary/40 p-3 text-[13px]">
              <Row label={t('certs:dnsReady.recordType')} value="TXT" />
              <Row label={t('certs:dnsReady.recordName')} value={d?.challenge_fqdn ?? ''} />
              <Row label={t('certs:dnsReady.recordValue')} value={d?.challenge_value ?? ''} />
            </div>
            <span className="text-xs text-warning">{t('certs:dnsReady.tooEarlyWarn')}</span>
          </>
        ) : detail.isSuccess ? (
          // 取不到验证记录：可能还没到验证阶段，也可能不是 dns-01。
          // ⚠️ 这不是"可以放行了"，所以按钮同时被 blockedReason 挡住
          <Banner tone="warn">
            <span className="font-medium">{t('certs:dnsReady.noChallenge.title')}</span>
            <span className="mt-0.5 block">
              {t('certs:dnsReady.noChallenge.body', {
                status: d?.status || '—',
                challenge: d?.challenge || '—',
              })}
            </span>
          </Banner>
        ) : null}

        {ready.isError ? (
          <MutationError error={ready.error} toInfo={toErrorInfo} t={(k, p) => tError(t, k, p)} />
        ) : null}
      </div>
    </Dialog>
  )
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex gap-3">
      <span className="w-20 shrink-0 text-muted-foreground">{label}</span>
      {/* 要手抄进 DNS 后台的东西：等宽 + 可选中，且不能截断 */}
      <span className="min-w-0 flex-1 font-mono text-xs break-all select-all">{value || '—'}</span>
    </div>
  )
}
