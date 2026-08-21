package pathpolicy

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// 主体具体度，与 access 包同一套刻度（改一处必须改两处，两包的取值必须一致）。
const (
	rankPublic   = 0
	rankDeptBase = 100
	rankRole     = 200
	rankGroup    = 300
	rankUser     = 400
)

// Evaluate 判定一次接口访问。
//
// 顺序无关：把 rules 打乱重排，结果不变（TestOrderIndependent 保证这一点）。
func Evaluate(req Request, rules []Rule) Result {
	var cands []Scored

	for _, r := range rules {
		if r.AppID != req.AppID {
			continue
		}
		pathRank, ok := matchPath(r.PathPattern, req.Path)
		if !ok {
			continue
		}
		if !matchMethod(r.Methods, req.Method) {
			continue
		}
		subjRank, ok := matchSubject(r, req)
		if !ok {
			continue
		}
		condRank, ok := matchConditions(r, req)
		if !ok {
			continue
		}
		methodRank := 0
		if strings.TrimSpace(r.Methods) != "*" && r.Methods != "" {
			methodRank = 1
		}
		cands = append(cands, Scored{
			Rule:     r,
			PathRank: pathRank*2 + methodRank,
			SubjRank: subjRank,
			CondRank: condRank,
		})
	}

	if len(cands) == 0 {
		// 没有任何规则命中 → 拒绝。
		// 这里必须是拒绝而不是放行：接口级策略没配的应用，等于"还没想清楚谁能调"，
		// 默认放行会让一个刚接入的应用直接对全员开放全部接口。
		return Result{Decision: Deny, Reason: ReasonDefaultDeny}
	}

	sort.SliceStable(cands, func(i, j int) bool {
		a, b := cands[i], cands[j]
		if a.SubjRank != b.SubjRank {
			return a.SubjRank > b.SubjRank
		}
		if a.PathRank != b.PathRank {
			return a.PathRank > b.PathRank
		}
		if a.CondRank != b.CondRank {
			return a.CondRank > b.CondRank
		}
		if severity(a.Rule.Decision) != severity(b.Rule.Decision) {
			return severity(a.Rule.Decision) > severity(b.Rule.Decision)
		}
		// 兜底按 ID：同级规则的胜负不能取决于数据库返回顺序，
		// 否则同一次请求重放两遍可能得到不同结论
		return a.Rule.ID < b.Rule.ID
	})

	cands[0].Won = true
	win := cands[0].Rule

	res := Result{Decision: win.Decision, Reason: ReasonRule, Rule: &win, MFATTL: win.MFATTLSec, Candidates: cands}

	// 需要工单却没带 → 降为 challenge，并给出可执行的原因码，
	// 而不是笼统地拒绝。界面据此提示「去绑工单」，人才知道下一步做什么。
	if win.RequireTicket && !req.HasTicket && win.Decision != Deny {
		res.Decision = Challenge
		res.Reason = ReasonNoTicket
	}
	return res
}

// matchPath 前缀匹配，返回"具体度"。
//
// 具体度 = 匹配到的路径段数。/api/v2/projects/** 命中 /api/v2/projects/pay 时得 3，
// 而 /** 得 0 —— 越长越具体，这就是取代"顺序"的那把尺子。
func matchPath(pattern, path string) (int, bool) {
	p := strings.TrimSuffix(pattern, "/**")
	if p == "" {
		return 0, strings.HasPrefix(path, "/")
	}
	if pattern == p { // 精确匹配（没有 /** 后缀）
		if path == p {
			return segments(p) + 1, true // 精确比子树更具体，+1
		}
		return 0, false
	}
	if path == p || strings.HasPrefix(path, p+"/") {
		return segments(p), true
	}
	return 0, false
}

func segments(p string) int {
	n := 0
	for _, s := range strings.Split(p, "/") {
		if s != "" {
			n++
		}
	}
	return n
}

func matchMethod(methods, m string) bool {
	methods = strings.TrimSpace(methods)
	if methods == "" || methods == "*" {
		return true
	}
	m = strings.ToUpper(strings.TrimSpace(m))
	for _, x := range strings.Split(methods, ",") {
		if strings.ToUpper(strings.TrimSpace(x)) == m {
			return true
		}
	}
	return false
}

func matchSubject(r Rule, req Request) (int, bool) {
	switch r.SubjectType {
	case SubjectPublic:
		return rankPublic, true
	case SubjectUser:
		if r.SubjectID == req.UserID {
			return rankUser, true
		}
	case SubjectGroup:
		if req.GroupIDs[r.SubjectID] {
			return rankGroup, true
		}
	case SubjectRole:
		if req.RoleIDs[r.SubjectID] {
			return rankRole, true
		}
	case SubjectDept:
		if d, ok := req.DeptDepth[r.SubjectID]; ok {
			return rankDeptBase + d, true
		}
	}
	return 0, false
}

// matchConditions 校验设备/来源/时间三类条件，并返回"限了几项"作为具体度。
//
// 限得越多越具体：一条限了设备又限了时段的规则，应当盖过只限了路径的那条。
func matchConditions(r Rule, req Request) (int, bool) {
	rank := 0
	if r.DeviceState != "" {
		// 服务账号没有"设备"概念（DeviceState 为空）。
		// 这正是原型里 report-job 被误杀的那个坑：不能把"没有设备"当成"设备未纳管"。
		if req.DeviceState == "" || req.DeviceState != r.DeviceState {
			return 0, false
		}
		rank++
	}
	if r.SourceKind != "" {
		if req.SourceKind != r.SourceKind {
			return 0, false
		}
		rank++
	}
	if r.TimeWindow != "" {
		from, to, err := parseWindow(r.TimeWindow)
		if err != nil {
			return 0, false
		}
		at := req.At
		if at.IsZero() {
			at = time.Now()
		}
		mins := at.Hour()*60 + at.Minute()
		in := false
		if from <= to {
			in = mins >= from && mins <= to
		} else {
			// 跨零点，如 22:00-06:00
			in = mins >= from || mins <= to
		}
		if !in {
			return 0, false
		}
		rank++
	}
	if r.RequireTicket {
		rank++
	}
	return rank, true
}

// parseWindow 解析 "08:00-22:00" 为分钟数。
func parseWindow(w string) (from, to int, err error) {
	parts := strings.Split(w, "-")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("时间窗口格式应为 HH:MM-HH:MM")
	}
	pm := func(s string) (int, error) {
		hm := strings.Split(strings.TrimSpace(s), ":")
		if len(hm) != 2 {
			return 0, fmt.Errorf("bad time")
		}
		h, e1 := strconv.Atoi(hm[0])
		m, e2 := strconv.Atoi(hm[1])
		if e1 != nil || e2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
			return 0, fmt.Errorf("bad time")
		}
		return h*60 + m, nil
	}
	if from, err = pm(parts[0]); err != nil {
		return 0, 0, err
	}
	if to, err = pm(parts[1]); err != nil {
		return 0, 0, err
	}
	return from, to, nil
}
