import { useTranslation } from '@ops/i18n'
import type { Cell } from './types.js'
import { CHIP, KIND, SYNC_CHIP } from './verdict.js'

/**
 * 版本号渲染：把**构建号**加粗，其余降噪。
 *
 * `20260519082034-58ac8c3-114` 里人真正在比的只有尾部那个 114。
 * 整串同一个字重会逼人逐字符读 —— 这张表一屏有几十个版本号，
 * 逐字符读和一眼扫过去是完全不同的使用体验。
 */
function Version({ tag }: { tag: string }) {
  const i = tag.lastIndexOf('-')
  // 没有分隔符（stable、latest 这类）时整串照原样，不硬拆
  if (i < 0 || i === tag.length - 1) {
    return <span className="font-mono text-xs text-muted-foreground">{tag}</span>
  }
  return (
    <span className="font-mono text-xs whitespace-nowrap text-muted-foreground">
      {tag.slice(0, i + 1)}
      <span className="font-semibold text-foreground">{tag.slice(i + 1)}</span>
    </span>
  )
}

/**
 * 落差刻度：把「落后 4 个版本」画出来，而不只是写个数字。
 * 差 1 个和差 40 个的风险完全不同，但两个数字长得一样长。
 */
function Gap({ delta }: { delta: number }) {
  const n = Math.min(Math.abs(delta), 8)
  const over = Math.abs(delta) > 8
  return (
    <span className="ml-1.5 inline-flex items-center gap-[1.5px] align-[-1px]">
      {Array.from({ length: n }, (_, i) => (
        <i
          key={i}
          className={`block h-2 w-[3px] rounded-[.5px] ${over ? 'bg-danger' : 'bg-warning'}`}
        />
      ))}
      {over && <span className="ml-0.5 text-[10px] text-danger">+</span>}
    </span>
  )
}

export function VerdictCell({ cell, columnFailed }: { cell: Cell; columnFailed?: boolean }) {
  const { t } = useTranslation()
  const kind = KIND[cell.Verdict] ?? 'none'
  const tag = cell.Snap?.Tag

  return (
    <div className="flex flex-col gap-0.5 py-0.5">
      {tag ? <Version tag={tag} /> : <span className="font-mono text-xs text-muted-foreground">—</span>}

      {cell.Verdict !== 'same' && (
        <span className="flex items-center">
          <span
            className={`inline-flex items-center rounded-[2px] py-px pr-[5px] pl-1 text-[10.5px] leading-[1.45] font-semibold ${CHIP[kind]}`}
          >
            {t(`opsversion:verdict.${cell.Verdict}`)}
            {cell.Delta != null && ` ${Math.abs(cell.Delta)}`}
          </span>
          {cell.Delta != null && <Gap delta={cell.Delta} />}
        </span>
      )}

      {/* 发布中是附加标记：一个服务可以既「一致」又「正在滚动更新」。
          只看声明的 tag 会把「YAML 改了但一个 pod 都没起来」显示成已升级 */}
      {cell.Deploying && cell.Snap && (
        <span className="inline-flex w-fit items-center rounded-[2px] bg-info-bg py-px pr-[5px] pl-1 text-[10.5px] font-semibold text-info shadow-[inset_2px_0_0_var(--color-info)]">
          {t('opsversion:verdict.deploying')}
        </span>
      )}
      {cell.Deploying && cell.Snap && (
        <span className="font-mono text-[10.5px] text-muted-foreground">
          {cell.Snap.Tag} → {cell.Snap.RunningTag}
        </span>
      )}

      {/* 归因：这个差异该找谁。
          🔴 只在有差异时出现 —— 一致的格子标一个「已同步」纯属噪音 */}
      {cell.Sync && (
        <span
          className={`inline-flex w-fit items-center rounded-[2px] py-px pr-[5px] pl-1 text-[10.5px] font-semibold ${SYNC_CHIP[cell.Sync] ?? SYNC_CHIP.unknown}`}
          title={cell.SyncNote}
        >
          {t(`opsversion:syncAttr.${cell.Sync}`)}
        </span>
      )}

      {/* 🔴 说明文字只有 conflict 用红。
          no_data / unknown 的说明是「为什么判不了」，不是故障描述 —— 用红会让
          一列 token 过期看起来像对方全线崩溃。

          ⚠️ 整列采集失败时**不再逐格重复原因**：那句话对整列都一样，
          顶部横幅已经说过一次，在 100 行里再印 100 遍只会把真正逐格不同的
          信息（冲突详情、发布中的实跑版本）淹掉。格子里保留标记，
          让人知道这一格没有结论；原因去看横幅。 */}
      {cell.Note && !(columnFailed && cell.Verdict === 'no_data') && (
        <span
          className={`text-[10.5px] leading-[1.4] ${
            kind === 'bad' ? 'text-danger' : 'text-muted-foreground'
          }`}
        >
          {cell.Note}
        </span>
      )}
    </div>
  )
}
