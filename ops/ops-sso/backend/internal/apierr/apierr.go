// Package apierr 是对外错误的唯一出口。
//
// # 为什么不让 handler 直接返回中文
//
// handler 里拼中文，等于把界面文案焊死在后端。要出英文版时，你得回来改
// 每一个 handler，而且总会漏 —— 漏掉的地方在英文界面上突然冒出一句中文。
//
// 所以后端只给 **码 + 参数**，文案由前端的语言包渲染。
// 代价是这里看起来啰嗦，收益是加一门语言不用碰后端。
package apierr

import "net/http"

// Code 错误码。命名规则：<域>.<具体错误>，全小写下划线。
type Code string

const (
	CodeInvalidParam       Code = "common.invalid_param"
	CodeNotFound           Code = "common.not_found"
	CodeConflict           Code = "common.conflict"
	CodeInternal           Code = "common.internal"
	CodeUnauthorized       Code = "auth.unauthorized"
	CodeForbidden          Code = "auth.forbidden"
	CodeNoTenant           Code = "auth.no_tenant"
	CodeInvalidRule        Code = "policy.invalid_rule"
	CodeEnforcedAllow      Code = "policy.enforced_allow_not_supported"
	CodeDuplicateRule      Code = "policy.duplicate"
	CodeInvalidCode        Code = "appcat.invalid_code"
	CodeBadCredential      Code = "auth.bad_credential"
	CodeAccountDisabled    Code = "auth.account_disabled"
	CodeBreakGlassUsed     Code = "auth.break_glass_used"
	CodeBreakGlassExpired  Code = "auth.break_glass_expired"
	CodeGroupHasPolicy     Code = "appcat.group_has_policy"
	CodeProbeNeedsCIDR     Code = "probe.cidr_required"
	CodeAuthBackend        Code = "auth.backend_unavailable"
	CodeMFANotEnrolled     Code = "mfa.not_enrolled"
	CodeMFABadCode         Code = "mfa.bad_code"
	CodeMFAAlreadyEnrolled Code = "mfa.already_enrolled"
	CodeLicenseReadOnly    Code = "license.read_only"
	// CodeLicenseInvalid 激活码验签不通过。**不是过期** —— 说成过期会把客户
	// 引去续费，而真正的问题是这串码本身不对（少粘了一段、被换行截断、
	// 或者它是另一个签发环境签的）。
	CodeLicenseInvalid Code = "license.invalid_token"
	// CodeLicenseWrongProduct 这份授权有效，但不含本产品。
	// 与"激活码错误"必须分开：前者要去找销售加购，后者是重新粘一遍。
	CodeLicenseWrongProduct Code = "license.wrong_product"
	CodeIdPRejected         Code = "idp.rejected"
	CodeIdPUnreachable      Code = "idp.unreachable"
	CodeIdPBadToken         Code = "idp.bad_token"
	CodeIdPNoSubject        Code = "idp.no_subject_claim"
	CodeIdPNoAccount        Code = "idp.no_local_account"
	CodeIdPStateExpired     Code = "idp.state_expired"
	CodeIdPStateInvalid     Code = "idp.state_invalid"
	CodeInvalidRequest      Code = "approval.invalid_request"
	CodeSelfApproval        Code = "approval.self_approval"
	CodeAlreadyDecided      Code = "approval.already_decided"
	CodeOIDCBadClient       Code = "oidc.unknown_client"
	CodeOIDCBadRedirect     Code = "oidc.bad_redirect_uri"
	CodeMustChangePassword  Code = "auth.must_change_password"
	// CodeNotAdmin 需要租户管理员。
	//
	// 和「未登录」必须分开：401 是"你是谁我不知道"，403 是"我知道你是谁，
	// 但你不行"。前端据此决定是跳登录页还是显示一句说明 ——
	// 混在一起会把管理员权限不足的人反复弹回登录页，他会以为是登录坏了。
	CodeNotAdmin Code = "auth.not_admin"
	// CodeUserExists 用户名已被占用。
	CodeUserExists Code = "user.exists"
	// CodeLastAdmin 不能停用/降权最后一个管理员。
	//
	// 单独一个码：这不是"参数错了"，是一条**有意的**保护。
	// 混在 invalid_param 里的话，界面只能说"参数不对"，
	// 而管理员会去检查自己填了什么 —— 问题根本不在那里。
	CodeLastAdmin Code = "user.last_admin"
	// CodeIdPMissingEndpoint 三个地址缺一个，登录会在某一步失败，
	// 而失败点在上游、很难查 —— 所以在保存时就拦。
	CodeIdPMissingEndpoint Code = "idp.missing_endpoint"
	CodeIdPNeedSecret      Code = "idp.need_secret"
	CodeIdPBadIssuer       Code = "idp.bad_issuer"
	// CodeIdPPrivateHost 拒绝把 discovery 拉向内网地址。
	// 不拦的话，这个接口就成了一个探测内网的入口，且返回体直接交出探测结果。
	CodeIdPPrivateHost Code = "idp.private_host"
	// CodeAnchorNoKey 没配锚点私钥，签不出锚点。
	// 单独一个码：这不是"失败了"，是"这个能力没开" —— 两者的下一步不同。
	CodeAnchorNoKey Code = "audit.anchor_no_key"
	// CodeRouteHostTaken 域名已被占用。⚠️ 唯一索引是全局的，
	// 冲突可能来自别的租户 —— 文案不能说"你已经配过了"。
	CodeRouteHostTaken   Code = "route.host_taken"
	CodeRouteBadHost     Code = "route.bad_host"
	CodeRouteBadUpstream Code = "route.bad_upstream"
	CodeOldPasswordWrong Code = "auth.old_password_wrong"
	CodeSamePassword     Code = "auth.same_password"
	CodeWeakPassword     Code = "auth.weak_password"
	CodeNoLocalPassword  Code = "auth.no_local_password"
)

// Error 对外错误。Params 会被前端插进语言包的占位符。
type Error struct {
	Status  int            `json:"-"`
	Code    Code           `json:"code"`
	Params  map[string]any `json:"params,omitempty"`
	Details string         `json:"details,omitempty"` // 仅用于排障，界面不展示；不得含凭据
}

func (e *Error) Error() string { return string(e.Code) }

func New(status int, code Code, params map[string]any) *Error {
	return &Error{Status: status, Code: code, Params: params}
}

func BadRequest(code Code, params map[string]any) *Error {
	return New(http.StatusBadRequest, code, params)
}

func NotFound() *Error { return New(http.StatusNotFound, CodeNotFound, nil) }

// Unauthorized 未认证。
func Unauthorized() *Error { return New(http.StatusUnauthorized, CodeUnauthorized, nil) }

// CrossTenant 跨租户访问一律按「不存在」处理。
//
// 返回 404 而不是 403 是刻意的：403 等于告诉对方「这个 ID 是存在的，只是你没权限」——
// 拿这个当探针就能把别的租户有多少应用、ID 是多少全摸出来。
func CrossTenant() *Error { return NotFound() }

func Internal(details string) *Error {
	return &Error{Status: http.StatusInternalServerError, Code: CodeInternal, Details: details}
}
