import { Button, type ButtonProps } from '@ops/ui'
import { useWriteGate } from '../lib/writeGate.js'

export interface WriteButtonProps extends Omit<ButtonProps, 'disabled'> {
  /** 需要的权限码，与后端 perm.go 里那条同名 */
  perm: string
  /**
   * 权限之外的禁用原因（如"先填 issuer"）。
   *
   * ⚠️ 刻意要求传**原因**而不是一个 disabled 布尔值：置灰不给原因，
   * 有权限的人会以为系统坏了，没权限的人不知道该找谁。
   * 想置灰就必须能说出为什么。
   */
  blockedReason?: string
}

/**
 * 写操作按钮。自己判断能不能点，并把**不能点的原因**挂在 title 上。
 *
 * ⚠️ 光置灰不给原因，是这类界面最常见的毛病：
 * 有权限的人以为系统坏了，没权限的人不知道该找谁。
 */
export function WriteButton({ perm, blockedReason, children, ...rest }: WriteButtonProps) {
  const gate = useWriteGate(perm)
  // 没权限优先于"还没填完"：先回答"你能不能做这件事"，再回答"现在能不能做"。
  // 反过来的话，一个没权限的人会一直以为只是自己少填了什么
  const reason = gate.allowed ? blockedReason : gate.reason
  // 🔴 title 要**合并**，不能直接覆盖。
  //
  //	原来写的是 `title={reason || undefined}`，而它排在 `{...rest}` 后面 ——
  //	于是能点的时候（reason 为空）会把调用方传进来的 title **抹成 undefined**。
  //
  //	平时看不出来：有文字的按钮本来也不太需要 title。
  //	但纯图标按钮的 title 是它**唯一**能说明自己是什么的途径（对鼠标用户），
  //	抹掉之后就是一个谁也不知道是什么的图标。
  //
  //	⚠️ 这类"运行时被覆盖"守卫查不出来 —— 源码里 title= 明明写着，
  //	check-icon-buttons 也是绿的。只有真的 hover 一下才看得见。
  const title = reason || rest.title
  return (
    <Button {...rest} disabled={!gate.allowed || !!blockedReason} title={title || undefined}>
      {children}
    </Button>
  )
}
