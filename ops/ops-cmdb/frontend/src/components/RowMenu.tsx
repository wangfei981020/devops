import { useTranslation } from '@ops/i18n'
import { MenuItem, MenuSeparator, Popover } from '@ops/ui'
import { MoreHorizontal } from 'lucide-react'
import type { ReactNode } from 'react'
import { useWriteGate } from '../lib/writeGate.js'

/**
 * 列表行的操作菜单。
 *
 * # 为什么破坏性操作不能是常驻按钮
 *
 * 「删除」在任何列表里都是**最少用**的操作，却因为 danger 配色成了视觉上最重的元素，
 * 而且通常紧挨着「编辑」——最常用的和最危险的相隔几十像素、颜色还最显眼。
 *
 * 更实际的问题：一行挂三四个按钮时，行高被撑起来、每行都在重复同样的按钮文字，
 * 眼睛没法快速扫过数据本身。数据才是列表页的主体，操作是次要的。
 *
 * 所以：**只有高频操作留在行上（如「测试连通性」），其余收进 ⋯ 菜单，
 * 破坏性操作一律进菜单并标 danger。**
 *
 * ⚠️ 不要为了"少点一次"把删除放回行上。删除本来就该多点一次。
 *
 * # 权限
 *
 * 整个菜单共用一个权限码——这几个页面本来就是每页一个 PERM 常量。
 * （做成每条一个权限码会要求在循环里调 useWriteGate，违反 hooks 规则。
 * 真需要混合权限时，把不同权限的操作拆成两个 RowMenu 或留在行上。）
 *
 * 没权限 / 只读授权时**不渲染菜单**，而不是渲染成禁用态：
 * 禁用态会让人一直琢磨"为什么点不了"，而原因（没权限 / 授权只读）
 * 在菜单里没有位置解释——行内按钮有 tooltip，菜单没有。
 */
export interface RowMenuItem {
  key: string
  label: string
  icon?: ReactNode
  onClick: () => void
  /** 破坏性操作。会标红，并在它前面自动加一条分隔线 */
  danger?: boolean
}

export function RowMenu({ perm, items }: { perm: string; items: RowMenuItem[] }) {
  const { t } = useTranslation()
  const gate = useWriteGate(perm)

  if (!gate.allowed || items.length === 0) return null

  return (
    <Popover
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
      {items.map((i, idx) => (
        <div key={i.key}>
          {/* 破坏性操作前加分隔线：手滑连点时多一道视觉阻断 */}
          {i.danger && idx > 0 ? <MenuSeparator /> : null}
          <MenuItem danger={i.danger} icon={i.icon} onClick={i.onClick}>
            {i.label}
          </MenuItem>
        </div>
      ))}
    </Popover>
  )
}
