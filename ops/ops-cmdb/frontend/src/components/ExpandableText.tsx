import { useState } from 'react'

/**
 * 一行截断、点开看全文。
 *
 * # 为什么不能只靠 `title`
 *
 * `title` 是有的，但它**不是一个看得见的入口**：鼠标停够久才出现，
 * 触屏上根本没有，而且长消息在原生 tooltip 里排版极差。
 *
 * 而被截断的恰恰是排障最要紧的部分 —— 实测事件中心三条消息里，
 * Pod 名、Secret 名、容器名**全都正好卡在省略号上**（OPSCMDB-031 P2-20）：
 *
 *   missing request for memory in container repo-server of Pod arg…
 *   Unable to retrieve some image pull secrets (ops-harbor-login-s…
 *
 * 每一条都停在"到底是哪一个"这个问题的正前方。
 *
 * # ⚠️ 展开态必须写 `whitespace-normal`
 *
 * DataTable 给每个 td 加了 `whitespace-nowrap`，它会继承给单元格里的文本。
 * 不覆盖的话展开只是把一行拉得更长，横向裁掉的部分照样看不见
 * （check-clamp-in-cells 守的就是这条）。
 */
export function ExpandableText({
  text,
  className = '',
  maxWidth = '380px',
}: {
  text: string
  className?: string
  /** 收起态的最大宽度。不限的话长消息会把后面的列挤出屏幕 */
  maxWidth?: string
}) {
  const [open, setOpen] = useState(false)
  if (!text) return null
  return (
    <button
      type="button"
      onClick={() => setOpen((v) => !v)}
      // title 保留：鼠标党不用点也能看
      title={open ? undefined : text}
      className={
        open
          ? `block cursor-pointer whitespace-normal break-words text-left ${className}`
          : `block cursor-pointer truncate text-left ${className}`
      }
      style={open ? undefined : { maxWidth }}
    >
      {text}
    </button>
  )
}
