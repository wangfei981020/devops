import { Button, type ButtonProps } from '@ops/ui'
import { useWriteGate } from '../lib/writeGate.js'

export interface WriteButtonProps extends Omit<ButtonProps, 'disabled'> {
  /** 需要的权限码，与后端 internal/api/perm.go 里那条同名 */
  perm: string
  /**
   * 权限之外的禁用原因（如"先选一条规则"）。
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
 * 用它替换写操作上的裸 Button：只读角色看到一排能点的按钮、
 * 点一个错一个，和没做权限是一样的体验。
 */
export function WriteButton({ perm, blockedReason, children, ...rest }: WriteButtonProps) {
  const gate = useWriteGate(perm)
  // 没权限优先于"还没填完"：先回答"你能不能做这件事"，再回答"现在能不能做"。
  // 反过来的话，一个没权限的人会一直以为只是自己少填了什么
  const reason = gate.allowed ? blockedReason : gate.reason
  return (
    <Button {...rest} disabled={!gate.allowed || !!blockedReason} title={reason || undefined}>
      {children}
    </Button>
  )
}
