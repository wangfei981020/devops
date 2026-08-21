// Package appcat 是应用目录的领域层：应用、自定义分组、以及它们的归属关系。
//
// 与 access 包的分工：appcat 回答「有哪些应用、怎么分组」，
// access 回答「谁能进」。两者只通过 ID 交互，互不引用对方的类型。
package appcat

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// ConnectType 接入方式。后两种是本产品的差异化：应用不改一行代码。
type ConnectType string

const (
	ConnectOIDC     ConnectType = "oidc"     // 应用支持 OIDC，直连
	ConnectSAML     ConnectType = "saml"     // 应用支持 SAML，直连
	ConnectGateway  ConnectType = "gateway"  // 网关代管，注入请求头，零改造
	ConnectFormFill ConnectType = "formfill" // 网关侧表单填充，给只有账号密码登录页的老系统
)

func (c ConnectType) Valid() bool {
	switch c {
	case ConnectOIDC, ConnectSAML, ConnectGateway, ConnectFormFill:
		return true
	}
	return false
}

// ZeroChange 是否属于「零改造」接入。售前话术与统计口径都用这个函数，
// 免得各处自己判断 —— 口径不一致的数字比没有数字更糟。
func (c ConnectType) ZeroChange() bool {
	return c == ConnectGateway || c == ConnectFormFill
}

// App 一个被纳管的应用。
type App struct {
	ID          int64
	TenantID    int64
	Code        string
	Name        string
	Description string
	ConnectType ConnectType
	Env         string // 原样照搬客户的枚举（PROD/UAT/...），不加解释性后缀
	BaseURL     string
	IconText    string
	IconColor   string
	Status      string

	// ShowWhenDenied 无权限时是否仍在门户里显示为「需申请」。
	// 默认 false：看不见就申请不了，但看得见等于把应用清单暴露给全员。
	// 所以做成每应用可选，而不是全局开关。
	ShowWhenDenied bool

	GroupIDs       []int64
	PrimaryGroupID int64
}

// Group 应用分组。同时用于门户分栏与授权作用域。
type Group struct {
	ID          int64
	TenantID    int64
	Code        string
	Name        string
	Description string
	SortOrder   int
	AppCount    int
}

var (
	ErrInvalidCode = errors.New("appcat: 标识不合法")
	ErrInvalidApp  = errors.New("appcat: 应用不合法")
)

// codePattern 标识只允许小写字母、数字、短横线。
//
// 不允许大小写混用是有原因的：MySQL 默认排序规则不区分大小写，
// 而代码里的 map 区分 —— 同一个 code 在库里唯一、在内存里却是两个键，
// 这种 bug 只会在某个客户真的建了 `Jira` 和 `jira` 时才炸。
var codePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}[a-z0-9]$`)

func ValidateCode(code string) error {
	if !codePattern.MatchString(code) {
		return fmt.Errorf("%w: %q（只允许小写字母、数字、短横线，2–64 位）", ErrInvalidCode, code)
	}
	return nil
}

// Validate 应用自身的合法性。
func (a App) Validate() error {
	if err := ValidateCode(a.Code); err != nil {
		return err
	}
	if strings.TrimSpace(a.Name) == "" {
		return fmt.Errorf("%w: 名称不能为空", ErrInvalidApp)
	}
	if !a.ConnectType.Valid() {
		return fmt.Errorf("%w: 未知接入方式 %q", ErrInvalidApp, a.ConnectType)
	}
	if strings.TrimSpace(a.Env) == "" {
		return fmt.Errorf("%w: 环境不能为空", ErrInvalidApp)
	}
	// 主分组必须在归属列表里，否则门户不知道该把它排到哪一栏
	if a.PrimaryGroupID != 0 {
		var in bool
		for _, g := range a.GroupIDs {
			if g == a.PrimaryGroupID {
				in = true
				break
			}
		}
		if !in {
			return fmt.Errorf("%w: 主分组 %d 不在归属分组里", ErrInvalidApp, a.PrimaryGroupID)
		}
	}
	return nil
}

// Validate 分组自身的合法性。
func (g Group) Validate() error {
	if err := ValidateCode(g.Code); err != nil {
		return err
	}
	if strings.TrimSpace(g.Name) == "" {
		return fmt.Errorf("%w: 分组名称不能为空", ErrInvalidApp)
	}
	return nil
}
