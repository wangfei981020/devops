import { useTranslation } from '@ops/i18n'
import { cn } from '@ops/ui'
import { RANGES, useTimeRange } from '../lib/timerange.js'

/**
 * 全局时间范围选择器。
 *
 * ⚠️ 只有**一份实现**，但会出现在两个位置：宽屏在顶栏，窄屏在页面内容区顶部。
 * 两处用 `hidden md:flex` / `md:hidden` 互斥显示，读的是同一个 context ——
 * 所以不存在"两个时间筛选各管各的"。
 *
 * 之所以要分两处：390px 的视口里，顶栏塞不下 5 个范围按钮 + 实时灯 +
 * 自检灯 + 用户名，实测会把文档撑到 526px 宽（整页横向滚动）。
 * 而这个控件在窄屏上恰恰更重要 —— 屏幕越小，一次能看到的事件越少，
 * 越依赖时间范围来收窄。所以是挪位置，不是砍功能。
 */
export function RangePicker({ className }: { className?: string }) {
  const { t } = useTranslation()
  const { range, setRange } = useTimeRange()
  return (
    <div
      className={cn('flex overflow-hidden rounded-md border border-border', className)}
      role="group"
      aria-label={t('opsalert:range.label')}
    >
      {RANGES.map((r) => (
        <button
          key={r || 'all'}
          type="button"
          onClick={() => setRange(r)}
          aria-pressed={range === r}
          className={cn(
            'cursor-pointer border-r border-border px-2.5 py-1 text-2xs transition-colors last:border-r-0',
            range === r
              ? 'bg-primary/10 text-primary'
              : 'bg-card text-muted-foreground hover:bg-muted hover:text-foreground',
          )}
        >
          {t(`opsalert:range.${r || 'all'}`)}
        </button>
      ))}
    </div>
  )
}
