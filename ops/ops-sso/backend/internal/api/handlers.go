package api

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"

	"github.com/gin-gonic/gin"

	"ops-sso-backend/internal/apierr"
	"ops-sso-backend/internal/domain/access"
	"ops-sso-backend/internal/domain/appcat"
	"ops-sso-backend/logx"
)

// ══════════════════════════════════════════════════════════════════
// 应用分组
// ══════════════════════════════════════════════════════════════════

func listGroups(c *gin.Context, d Deps) (any, error) {
	gs, err := d.AppCat.ListGroups(c.Request.Context())
	if err != nil {
		return nil, err
	}
	out := make([]gin.H, 0, len(gs))
	for _, g := range gs {
		out = append(out, gin.H{
			"id": g.ID, "code": g.Code, "name": g.Name,
			"description": g.Description, "sort_order": g.SortOrder, "app_count": g.AppCount,
		})
	}
	return gin.H{"items": out, "total": len(out)}, nil
}

type groupReq struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description"`
	SortOrder   int    `json:"sort_order"`
}

func createGroup(c *gin.Context, d Deps) (any, error) {
	var req groupReq
	if err := c.ShouldBindJSON(&req); err != nil {
		return nil, apierr.BadRequest(apierr.CodeInvalidParam, nil)
	}
	g := appcat.Group{Code: req.Code, Name: req.Name, Description: req.Description, SortOrder: req.SortOrder}
	id, err := d.AppCat.CreateGroup(c.Request.Context(), g)
	if err != nil {
		if errors.Is(err, appcat.ErrInvalidCode) {
			return nil, apierr.BadRequest(apierr.CodeInvalidCode, map[string]any{"code": req.Code})
		}
		return nil, err
	}
	d.Audit.Write(c.Request.Context(), auditEntry{
		TenantID: identity(c).TenantID, ActorID: actorID(c), ActorName: identity(c).Username,
		Action: "app_group.create", ObjectType: "app_group", ObjectID: id, ClientIP: clientIP(c),
		Detail: map[string]any{"code": req.Code, "name": req.Name},
	})
	return gin.H{"id": id}, nil
}

func updateGroup(c *gin.Context, d Deps) (any, error) {
	id, err := pathID(c)
	if err != nil {
		return nil, err
	}
	var req groupReq
	if err := c.ShouldBindJSON(&req); err != nil {
		return nil, apierr.BadRequest(apierr.CodeInvalidParam, nil)
	}
	g := appcat.Group{ID: id, Code: req.Code, Name: req.Name, Description: req.Description, SortOrder: req.SortOrder}
	if err := d.AppCat.UpdateGroup(c.Request.Context(), g); err != nil {
		return nil, err
	}
	d.Audit.Write(c.Request.Context(), auditEntry{
		TenantID: identity(c).TenantID, ActorID: actorID(c), ActorName: identity(c).Username,
		Action: "app_group.update", ObjectType: "app_group", ObjectID: id, ClientIP: clientIP(c),
		Detail: map[string]any{"code": req.Code, "name": req.Name},
	})
	return gin.H{"id": id}, nil
}

// groupDeleteImpact 删除前的影响面：这个分组上挂了多少条授权规则。
//
// 单独一个接口而不是删的时候顺带返回，是因为界面必须**先问再删** ——
// 删掉分组会连带让一批人失去访问，这种事不能删完才告诉人家。
func groupDeleteImpact(c *gin.Context, d Deps) (any, error) {
	id, err := pathID(c)
	if err != nil {
		return nil, err
	}
	n, err := d.AppCat.CountPoliciesForGroup(c.Request.Context(), id)
	if err != nil {
		return nil, err
	}
	return gin.H{"affected_policies": n}, nil
}

func deleteGroup(c *gin.Context, d Deps) (any, error) {
	id, err := pathID(c)
	if err != nil {
		return nil, err
	}
	n, err := d.AppCat.DeleteGroup(c.Request.Context(), id)
	if err != nil {
		return nil, err
	}
	// 连带删掉的规则条数必须记：删分组会顺手让一批人失去访问，
	// 只记「删了分组 #3」的话，事后没人能把「那天起进不去了」对上这一步。
	d.Audit.Write(c.Request.Context(), auditEntry{
		TenantID: identity(c).TenantID, ActorID: actorID(c), ActorName: identity(c).Username,
		Action: "app_group.delete", ObjectType: "app_group", ObjectID: id, ClientIP: clientIP(c),
		Detail: map[string]any{"deleted_policies": n},
	})
	return gin.H{"deleted_policies": n}, nil
}

// ══════════════════════════════════════════════════════════════════
// 应用
// ══════════════════════════════════════════════════════════════════

func listApps(c *gin.Context, d Deps) (any, error) {
	apps, err := d.AppCat.ListApps(c.Request.Context())
	if err != nil {
		return nil, err
	}
	return gin.H{"items": appsJSON(apps), "total": len(apps)}, nil
}

func appsJSON(apps []appcat.App) []gin.H {
	out := make([]gin.H, 0, len(apps))
	for _, a := range apps {
		out = append(out, gin.H{
			"id": a.ID, "code": a.Code, "name": a.Name, "description": a.Description,
			"connect_type": a.ConnectType, "zero_change": a.ConnectType.ZeroChange(),
			"env": a.Env, "base_url": a.BaseURL,
			"icon_text": a.IconText, "icon_color": a.IconColor, "status": a.Status,
			"show_when_denied": a.ShowWhenDenied,
			"group_ids":        a.GroupIDs, "primary_group_id": a.PrimaryGroupID,
		})
	}
	return out
}

type appReq struct {
	Code           string  `json:"code"`
	Name           string  `json:"name"`
	Description    string  `json:"description"`
	ConnectType    string  `json:"connect_type"`
	Env            string  `json:"env"`
	BaseURL        string  `json:"base_url"`
	IconText       string  `json:"icon_text"`
	IconColor      string  `json:"icon_color"`
	ShowWhenDenied bool    `json:"show_when_denied"`
	GroupIDs       []int64 `json:"group_ids"`
	PrimaryGroupID int64   `json:"primary_group_id"`

	// OIDC 接入方式时，顺带把客户端一起建了。
	//
	// # 为什么合并成一步
	//
	// 「一个应用一个客户端」是绝大多数情况，而原先要在两个页面上做两次，
	// 第二次还得回头在下拉里找刚建的应用 —— 系统明明知道你刚建了什么。
	// 1:N 仍然支持：要加第二个客户端，去 OIDC 客户端页单独加。
	//
	// 留空则不建客户端（比如先登记应用、稍后再配协议）。
	OIDC *struct {
		RedirectURIs []string `json:"redirect_uris"`
		PublicClient bool     `json:"public_client"`
	} `json:"oidc"`
}

func createApp(c *gin.Context, d Deps) (any, error) {
	var req appReq
	if err := c.ShouldBindJSON(&req); err != nil {
		return nil, apierr.BadRequest(apierr.CodeInvalidParam, nil)
	}
	a := appcat.App{
		Code: req.Code, Name: req.Name, Description: req.Description,
		ConnectType: appcat.ConnectType(req.ConnectType), Env: req.Env, BaseURL: req.BaseURL,
		IconText: req.IconText, IconColor: req.IconColor, ShowWhenDenied: req.ShowWhenDenied,
		GroupIDs: req.GroupIDs, PrimaryGroupID: req.PrimaryGroupID,
	}
	id, err := d.AppCat.CreateApp(c.Request.Context(), a)
	if err != nil {
		if errors.Is(err, appcat.ErrInvalidCode) || errors.Is(err, appcat.ErrInvalidApp) {
			return nil, apierr.BadRequest(apierr.CodeInvalidParam, map[string]any{"reason": err.Error()})
		}
		return nil, err
	}
	d.Audit.Write(c.Request.Context(), auditEntry{
		TenantID: identity(c).TenantID, ActorID: actorID(c), ActorName: identity(c).Username,
		Action: "app.create", ObjectType: "app", ObjectID: id, ClientIP: clientIP(c),
		Detail: map[string]any{"code": req.Code, "name": req.Name, "env": req.Env},
	})

	out := gin.H{"id": id}

	// 顺带建 OIDC 客户端。
	//
	// ⚠️ 应用已经建好了，所以这一步失败**不回滚应用** ——
	// 回滚会把"应用建好了但客户端没建"变成"什么都没建"，
	// 而前者是可以从接入指引里看出来并补救的，后者只会让人重填一遍表单。
	// 但必须如实返回：不能让界面显示"接入成功"而客户端其实没建出来。
	if req.OIDC != nil && appcat.ConnectType(req.ConnectType) == appcat.ConnectOIDC {
		clientID, secret, err := createClientFor(c, d, clientOpts{
			AppID: id, RedirectURIs: req.OIDC.RedirectURIs, PublicClient: req.OIDC.PublicClient,
		})
		if err != nil {
			logx.Line("app", "应用已创建但 OIDC 客户端创建失败: "+err.Error())
			out["oidc_error"] = true
		} else {
			out["client_id"] = clientID
			// 明文只返回这一次
			out["client_secret"] = secret
			out["warning_code"] = "oidc.secret_shown_once"
		}
	}
	return out, nil
}

func setAppGroups(c *gin.Context, d Deps) (any, error) {
	id, err := pathID(c)
	if err != nil {
		return nil, err
	}
	var req struct {
		GroupIDs       []int64 `json:"group_ids"`
		PrimaryGroupID int64   `json:"primary_group_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		return nil, apierr.BadRequest(apierr.CodeInvalidParam, nil)
	}
	if err := d.AppCat.SetAppGroups(c.Request.Context(), id, req.GroupIDs, req.PrimaryGroupID); err != nil {
		return nil, err
	}
	// 改分组归属等于改这个应用适用哪些分组级规则 —— 是一次实质的授权变更，
	// 尽管它长得像一次普通编辑。
	d.Audit.Write(c.Request.Context(), auditEntry{
		TenantID: identity(c).TenantID, ActorID: actorID(c), ActorName: identity(c).Username,
		Action: "app.set_groups", ObjectType: "app", ObjectID: id, ClientIP: clientIP(c),
		Detail: map[string]any{"group_ids": req.GroupIDs, "primary_group_id": req.PrimaryGroupID},
	})
	return gin.H{"id": id}, nil
}

// ══════════════════════════════════════════════════════════════════
// 访问授权
// ══════════════════════════════════════════════════════════════════

func listPolicies(c *gin.Context, d Deps) (any, error) {
	scope := access.Scope(c.DefaultQuery("scope", string(access.ScopeGlobal)))
	scopeID, _ := strconv.ParseInt(c.DefaultQuery("scope_id", "0"), 10, 64)
	rules, err := d.Access.ListRules(c.Request.Context(), scope, scopeID)
	if err != nil {
		return nil, err
	}
	// 批量解析主体名字，避免 N+1：规则表上百条时逐条查会打出上百条 SQL
	res := resolveSubjects(c.Request.Context(), d.Store, rules)
	out := make([]gin.H, 0, len(rules))
	for _, r := range rules {
		out = append(out, ruleJSONWith(r, res))
	}
	return gin.H{"items": out, "total": len(out), "scope": scope, "scope_id": scopeID}, nil
}

// ruleJSON 序列化一条规则。res 可为 nil（不解析主体名字）。
func ruleJSONWith(r access.Rule, res *subjectResolver) gin.H {
	h := ruleJSON(r)
	res.annotate(h, r.SubjectType, r.SubjectID)
	return h
}

func ruleJSON(r access.Rule) gin.H {
	return gin.H{
		"id": r.ID, "scope": r.Scope, "scope_id": r.ScopeID,
		"subject_type": r.SubjectType, "subject_id": r.SubjectID,
		"effect": r.Effect, "enforced": r.Enforced,
		// note 必须返回：它是「半年后复核时唯一能看的东西」，
		// 界面上要求必填，读不回来等于白填 —— 而表现是列表里
		// 那一列永远是「—」，看起来像"大家都没写"。
		"note": r.Note,
	}
}

type policyReq struct {
	Scope       string `json:"scope"`
	ScopeID     int64  `json:"scope_id"`
	SubjectType string `json:"subject_type"`
	SubjectID   int64  `json:"subject_id"`
	Effect      string `json:"effect"`
	Enforced    bool   `json:"enforced"`
	Note        string `json:"note"`
}

func createPolicy(c *gin.Context, d Deps) (any, error) {
	var req policyReq
	if err := c.ShouldBindJSON(&req); err != nil {
		return nil, apierr.BadRequest(apierr.CodeInvalidParam, nil)
	}
	rule := access.Rule{
		Scope: access.Scope(req.Scope), ScopeID: req.ScopeID,
		SubjectType: access.SubjectType(req.SubjectType), SubjectID: req.SubjectID,
		Effect: access.Effect(req.Effect), Enforced: req.Enforced,
	}
	id, err := d.Access.Create(c.Request.Context(), rule, req.Note, actorID(c))
	if err != nil {
		switch {
		case errors.Is(err, access.ErrEnforcedAllow):
			return nil, apierr.BadRequest(apierr.CodeEnforcedAllow, nil)
		case errors.Is(err, access.ErrInvalidRule):
			return nil, apierr.BadRequest(apierr.CodeInvalidRule, map[string]any{"reason": err.Error()})
		case errors.Is(err, access.ErrDuplicate):
			// 409 而不是 400：这不是参数写错了，是这条规则已经存在 ——
			// 界面据此提示「去改那一条」，而不是让人对着表单反复试
			return nil, apierr.New(http.StatusConflict, apierr.CodeDuplicateRule,
				map[string]any{"scope": req.Scope, "subject_type": req.SubjectType})
		}
		return nil, err
	}
	// 授权规则的增删是这个系统里后果最大的操作 —— 必须记，且要记内容。
	// 只记「新增了规则 #12」没有用：复盘时要能看出加的是放行还是拒绝、给了谁。
	d.Audit.Write(c.Request.Context(), auditEntry{
		TenantID: identity(c).TenantID, ActorID: actorID(c), ActorName: identity(c).Username,
		Action: "policy.create", ObjectType: "policy", ObjectID: id, ClientIP: clientIP(c),
		Detail: ruleDetail(rule, req.Note),
	})
	return gin.H{"id": id}, nil
}

func deletePolicy(c *gin.Context, d Deps) (any, error) {
	id, err := pathID(c)
	if err != nil {
		return nil, err
	}
	// ⚠️ 删之前先读出来。删完就查不到了，而审计要回答的恰恰是
	// 「被删掉的那条是什么」—— 事后再补是补不上的。
	// 读失败不阻断删除：删除是用户要的动作，审计缺一个字段也比操作失败强，
	// 但缺了要在这条审计里说出来，不能装作记全了。
	old, readErr := d.Access.Get(c.Request.Context(), id)

	if err := d.Access.Delete(c.Request.Context(), id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// 跨租户或已删 —— 一律 404，不告诉对方「存在但你没权限」
			return nil, apierr.CrossTenant()
		}
		return nil, err
	}

	detail := map[string]any{}
	if readErr != nil {
		detail["content_unavailable"] = true
		logx.Line("audit", "删除策略前读不到原内容，审计将缺内容: "+readErr.Error())
	} else {
		detail = ruleDetail(old, old.Note)
	}
	d.Audit.Write(c.Request.Context(), auditEntry{
		TenantID: identity(c).TenantID, ActorID: actorID(c), ActorName: identity(c).Username,
		Action: "policy.delete", ObjectType: "policy", ObjectID: id, ClientIP: clientIP(c),
		Detail: detail,
	})
	return nil, nil
}

// ruleDetail 规则的可读快照，进审计详情。
// 主体 ID 原样记：名字会变、会被删，ID 才是能回溯的那一个。
func ruleDetail(r access.Rule, note string) map[string]any {
	return map[string]any{
		"scope": string(r.Scope), "scope_id": r.ScopeID,
		"subject_type": string(r.SubjectType), "subject_id": r.SubjectID,
		"effect": string(r.Effect), "enforced": r.Enforced, "note": note,
	}
}

// simulate 生效结果试算：这个人现在到底能不能进这个应用，以及为什么。
//
// 这是控制台里最该有的一个功能。授权规则一多，「他为什么进不去」
// 只靠看列表是答不出来的，而答不出来的结果就是运维直接给他开个全局 allow。
func simulate(c *gin.Context, d Deps) (any, error) {
	var req struct {
		UserID int64 `json:"user_id"`
		AppID  int64 `json:"app_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.UserID <= 0 || req.AppID <= 0 {
		return nil, apierr.BadRequest(apierr.CodeInvalidParam, nil)
	}
	ctx := c.Request.Context()

	app, err := d.AppCat.GetApp(ctx, req.AppID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apierr.CrossTenant()
		}
		return nil, err
	}
	sub, err := d.Access.LoadSubject(ctx, req.UserID)
	if err != nil {
		return nil, err
	}
	rules, err := d.Access.LoadRulesForApp(ctx, req.AppID)
	if err != nil {
		return nil, err
	}

	dec := access.Evaluate(sub, access.App{ID: app.ID, GroupIDs: app.GroupIDs}, rules)
	// 判定链路上的每条规则也要显示主体名字 —— 那正是排障时最先看的一栏
	return decisionJSON(dec, resolveSubjects(ctx, d.Store, rules)), nil
}

// decisionJSON 判定结果 + 完整轨迹。
//
// 轨迹里**不含任何中文** —— 只有码与参数，文案由界面的语言包渲染。
// decisionJSON 序列化一次判定。
//
// res 可为 nil —— **网关的数据面就传 nil**：那条路径每个请求都要走，
// 为了显示名字去查一次库，等于给全公司每一次访问加一次数据库往返。
// 名字只有人在看的时候才需要（控制台的试算、审计回放）。
func decisionJSON(dec access.Decision, res *subjectResolver) gin.H {
	steps := make([]gin.H, 0, len(dec.Trace))
	for _, s := range dec.Trace {
		steps = append(steps, gin.H{
			"rule":          ruleJSONWith(s.Rule, res),
			"subject_rank":  s.SubjectRank,
			"scope_rank":    s.ScopeRank,
			"outcome":       s.Outcome,
			"matched_depth": s.MatchedDepth,
		})
	}
	out := gin.H{
		"effect":  dec.Effect,
		"allowed": dec.Allowed(),
		"reason":  dec.Reason,
		"trace":   steps,
	}
	if dec.Rule != nil {
		out["decided_by"] = ruleJSONWith(*dec.Rule, res)
	}
	return out
}

// ══════════════════════════════════════════════════════════════════
// 门户
// ══════════════════════════════════════════════════════════════════

// portalApps 员工门户的「我的入口」：我能进哪些应用，按分组分栏。
//
// 与网关判定**共用同一个 Evaluate** —— 门户上看得见、点进去被网关拒，
// 是这类产品最伤人的体验之一，共用一套代码是唯一可靠的防法。
func portalApps(c *gin.Context, d Deps) (any, error) {
	ctx := c.Request.Context()
	uid := actorID(c)

	apps, err := d.AppCat.ListApps(ctx)
	if err != nil {
		return nil, err
	}
	groups, err := d.AppCat.ListGroups(ctx)
	if err != nil {
		return nil, err
	}
	sub, err := d.Access.LoadSubject(ctx, uid)
	if err != nil {
		return nil, err
	}
	rules, err := d.Access.LoadAllRules(ctx)
	if err != nil {
		return nil, err
	}

	// 一次查规则、内存里算 N 个应用：应用几十个、规则上千条时，
	// 逐个应用查库会是 N+1 次查询，门户首屏直接卡住。
	evalApps := make([]access.App, 0, len(apps))
	for _, a := range apps {
		evalApps = append(evalApps, access.App{ID: a.ID, GroupIDs: a.GroupIDs})
	}
	decisions := access.VisibleApps(sub, evalApps, rules)

	type item struct {
		gin.H
	}
	byGroup := map[int64][]gin.H{}
	var ungrouped []gin.H
	for _, a := range apps {
		if a.Status != "active" {
			continue
		}
		dec := decisions[a.ID]
		if !dec.Allowed() && !a.ShowWhenDenied {
			continue // 看不见 —— 无权限的应用默认不暴露给全员
		}
		card := gin.H{
			"id": a.ID, "code": a.Code, "name": a.Name, "env": a.Env,
			"icon_text": a.IconText, "icon_color": a.IconColor,
			"connect_type": a.ConnectType, "base_url": a.BaseURL,
			"allowed": dec.Allowed(),
			"reason":  dec.Reason,
		}
		g := a.PrimaryGroupID
		if g == 0 {
			ungrouped = append(ungrouped, card)
			continue
		}
		byGroup[g] = append(byGroup[g], card)
	}

	sections := make([]gin.H, 0, len(groups)+1)
	known := make(map[int64]bool, len(groups))
	for _, g := range groups {
		known[g.ID] = true
		items := byGroup[g.ID]
		if len(items) == 0 {
			continue // 空分组不在门户里占一栏
		}
		sections = append(sections, gin.H{"group_id": g.ID, "name": g.Name, "items": items})
	}

	// ★ 主分组已经不存在的应用，落到「未分组」，**不能丢**。
	//
	// 实测踩到的：app_group_members 里还指着一个已被删掉的分组，
	// 于是这些应用只存在于 byGroup[那个不存在的 id] 里，
	// 上面那个循环遍历的是**现存分组**，永远取不到它们 ——
	// 结果是应用状态正常、权限正常、控制台里看得见，
	// 而门户里**凭空消失，且哪里都不报错**。
	// 管理员看到 3 个应用，用户看到 0 个，两边都以为对方搞错了。
	// map 遍历顺序是随机的：不排的话「未分组」里的应用每次刷新都换位置，
	// 而人是靠位置记住常用系统的。按分组 id 排，结果稳定。
	orphanGIDs := make([]int64, 0, len(byGroup))
	for gid := range byGroup {
		if !known[gid] {
			orphanGIDs = append(orphanGIDs, gid)
		}
	}
	sort.Slice(orphanGIDs, func(i, j int) bool { return orphanGIDs[i] < orphanGIDs[j] })
	for _, gid := range orphanGIDs {
		items := byGroup[gid]
		logx.Line("portal", fmt.Sprintf(
			"应用的主分组 #%d 已不存在，已归到「未分组」显示；请修分组归属（悬空引用会让人以为应用丢了）", gid))
		ungrouped = append(ungrouped, items...)
	}

	if len(ungrouped) > 0 {
		// group_id=0 是「未分组」，前端用固定文案渲染，后端不给中文
		sections = append(sections, gin.H{"group_id": 0, "name": "", "items": ungrouped})
	}
	return gin.H{"sections": sections}, nil
}

// ══════════════════════════════════════════════════════════════════
// 应用的编辑与删除
// ══════════════════════════════════════════════════════════════════
//
// 一直没有这两个接口，表现是**建错了就永远改不掉**：
// 名字写错、环境标错、访问地址填错，都只能重建一个新的，
// 而旧的那条还留在列表和门户里。清理测试数据只能直接写库。

// appDependents 删除前的影响面。
//
// 界面拿这个渲染二次确认。**必须是真实数量，不能写死一句"会一起删掉相关配置"** ——
// 那句话既没告诉人删了几条，也没告诉人有没有网关路由，
// 而"有没有路由"决定了这一次删除会不会让一个域名当场没人接。
func appDependents(c *gin.Context, d Deps) (any, error) {
	id, err := pathID(c)
	if err != nil {
		return nil, err
	}
	if err := requireAppInTenant(c, d, id); err != nil {
		return nil, err
	}
	deps, err := d.AppCat.CountAppDeps(c.Request.Context(), id)
	if err != nil {
		return nil, err
	}
	return deps, nil
}

func updateApp(c *gin.Context, d Deps) (any, error) {
	id, err := pathID(c)
	if err != nil {
		return nil, err
	}
	var req appReq
	if err := c.ShouldBindJSON(&req); err != nil {
		return nil, apierr.BadRequest(apierr.CodeInvalidParam, nil)
	}

	before, err := d.AppCat.GetApp(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apierr.CrossTenant()
		}
		return nil, err
	}

	// code 和 connect_type 一律以库里的为准，请求里带了也不看。
	//
	// code：下游配置里填的就是它，改了等于对方那一行指向一个不存在的应用，
	//       而下游不会有任何提示 —— 只会在下次有人登录时才暴露。
	// connect_type：改它不会让已经建好的 OIDC 客户端或网关路由消失，
	//       只会让界面写着"网关代管"而 OIDC 登录照样能用 —— 界面和实际不符。
	//       真要换接入方式，先把旧的挂件删干净。
	a := appcat.App{
		ID: id, Code: before.Code, ConnectType: before.ConnectType,
		Name: req.Name, Description: req.Description, Env: req.Env, BaseURL: req.BaseURL,
		IconText: req.IconText, IconColor: req.IconColor, ShowWhenDenied: req.ShowWhenDenied,
		GroupIDs: req.GroupIDs, PrimaryGroupID: req.PrimaryGroupID,
	}
	if err := d.AppCat.UpdateApp(c.Request.Context(), a); err != nil {
		if errors.Is(err, appcat.ErrInvalidCode) || errors.Is(err, appcat.ErrInvalidApp) {
			return nil, apierr.BadRequest(apierr.CodeInvalidParam, map[string]any{"reason": err.Error()})
		}
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apierr.CrossTenant()
		}
		return nil, err
	}
	d.Audit.Write(c.Request.Context(), auditEntry{
		TenantID: identity(c).TenantID, ActorID: actorID(c), ActorName: identity(c).Username,
		Action: "app.update", ObjectType: "app", ObjectID: id, ClientIP: clientIP(c),
		// 记改前改后：审计只记"改过了"等于没记，复盘要问的是"改成了什么"
		Detail: map[string]any{
			"code": before.Code,
			"before": map[string]any{
				"name": before.Name, "env": before.Env, "base_url": before.BaseURL,
				"show_when_denied": before.ShowWhenDenied,
			},
			"after": map[string]any{
				"name": a.Name, "env": a.Env, "base_url": a.BaseURL,
				"show_when_denied": a.ShowWhenDenied,
			},
		},
	})
	return gin.H{"id": id}, nil
}

func deleteApp(c *gin.Context, d Deps) (any, error) {
	id, err := pathID(c)
	if err != nil {
		return nil, err
	}
	// 先取出来：删完就查不到了，而审计里最该记的正是"删掉的是哪个应用"
	before, err := d.AppCat.GetApp(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apierr.CrossTenant()
		}
		return nil, err
	}

	deps, err := d.AppCat.DeleteApp(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apierr.CrossTenant()
		}
		return nil, err
	}
	d.Audit.Write(c.Request.Context(), auditEntry{
		TenantID: identity(c).TenantID, ActorID: actorID(c), ActorName: identity(c).Username,
		Action: "app.delete", ObjectType: "app", ObjectID: id, ClientIP: clientIP(c),
		Detail: map[string]any{
			"code": before.Code, "name": before.Name, "connect_type": string(before.ConnectType),
			// 连带收起了什么，一并记下 —— "为什么那个域名不通了"要从这里查
			"cascaded": deps,
		},
	})
	return gin.H{"deleted": deps}, nil
}

// requireAppInTenant 应用必须属于本租户，否则一律 404。
//
// 不能报 403：403 等于确认"这个 id 存在，只是不给你看"，
// 那本身就是一条跨租户的信息泄露。
func requireAppInTenant(c *gin.Context, d Deps, id int64) error {
	q, err := d.Store.Tenant(c.Request.Context())
	if err != nil {
		return err
	}
	var n int
	if err := q.QueryRow(`SELECT COUNT(*) FROM apps
		WHERE tenant_id = ? AND id = ? AND deleted_at IS NULL`, id).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return apierr.CrossTenant()
	}
	return nil
}
