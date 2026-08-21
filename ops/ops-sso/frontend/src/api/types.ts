/** 与后端实际返回逐字段对齐 —— 是调真接口抄下来的，不是照代码猜的。 */

export interface Identity {
  user_id: number
  tenant_id: number
  username: string
  display_name: string
  source: string
  is_break_glass: boolean
  must_change_password: boolean
  /** 当前租户里的角色（user_tenants.role_code），空串 = 普通成员 */
  role: string
  /**
   * 是不是当前租户的管理员。
   *
   * ⚠️ 只用来决定**显不显示**管理入口。真正的拦截在后端 adminOnly 中间件里 ——
   * 改一个 JS 变量能让按钮出现，但接口照样 403。
   * 「权限判据一律以后端为准，前端禁止自行推导」在别的项目上栽过三次。
   */
  is_admin: boolean
}

export interface PortalApp {
  id: number
  code: string
  name: string
  env: string
  icon_text: string
  icon_color: string
  connect_type: string
  base_url: string
  /** 能不能进。false 时 reason 说明为什么 —— 门户要把原因说出来。 */
  allowed: boolean
  reason: string
}

export interface PortalSection {
  group_id: number
  /** 未分组时后端返回空串，前端用固定文案渲染（后端不给中文）。 */
  name: string
  items: PortalApp[]
}

export interface Session {
  id: number
  client_ip: string
  user_agent: string
  source: string
  created_at: string
  last_seen_at: string
  expires_at: string
  current: boolean
}

export interface Grant {
  app_name?: string
  scope?: string
  granted_via?: string
  expires_at?: string
}

export interface ListOf<T> {
  items: T[]
  total: number
}

// ── 控制台 ──────────────────────────────────────────────

export interface App {
  id: number
  code: string
  name: string
  env: string
  connect_type: string
  base_url: string
  icon_text: string
  icon_color: string
  show_when_denied: boolean
  group_ids: number[]
  primary_group_id: number
}

export interface Rule {
  id: number
  scope: 'global' | 'group' | 'app'
  scope_id: number
  subject_type: 'public' | 'dept' | 'role' | 'group' | 'user'
  subject_id: number
  effect: 'allow' | 'deny'
  enforced: boolean
  note?: string
  /** 主体显示名。后端批量解析，public 类型不返回。 */
  subject_name?: string
  /** true 表示主体查不到 —— 多半是被删了而规则还留着，那条规则仍在参与判定。 */
  subject_missing?: boolean
}

/** 一条规则在本次判定里的下场。won = 生效，其余是被盖住的原因。 */
export type Outcome = 'won' | 'enforced' | 'shadowed' | 'lost_to_deny'

export interface TraceStep {
  rule: Rule
  /** 主体具体度：public < dept < role < group < user。数越大越优先。 */
  subject_rank: number
  /** 作用域具体度：global < group < app。 */
  scope_rank: number
  outcome: Outcome
  matched_depth: number
}

export interface Decision {
  effect: 'allow' | 'deny'
  allowed: boolean
  reason: string
  decided_by?: Rule
  trace: TraceStep[]
}

export interface AppGroup {
  id: number
  name: string
}
