package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"opsplatform/database"
)

// v774 班次时间段的「按组 + 时区」覆盖
//
// 背景：ig 和 sl 不是一家公司，班次口径不同。
// 两边 A(09:00-18:00)、B(15:00-24:00) 一致，只有 C 不同：
//
//	sl 的一天从早上 9 点开始：A -> B -> C(24:00-09:00，跨到次日凌晨)
//	ig 的一天从自然日 0 点开始：C(00:00-09:00) -> A -> B，当天走完
//
// 同一个格子里标的 C，sl 的人次日凌晨才上，ig 的人当天凌晨就上了。
//
// ⚠️ key 必须是 (组, 时区, 代码) 三元组，不能只按组：
// ig 组里有人在上海，他按北京口径排班，不能因为组名是 ig 就套塞尔维亚的定义。
// 又因为时区本身是按日期解析的，「改时区之前按老口径、之后按新口径」自动成立，
// 历史排班不用回溯修改。

// ShiftOverride 某个组在某个时区下对某个班次的特殊定义
type ShiftOverride struct {
	ID        int    `json:"id"`
	GroupName string `json:"group_name"`
	Timezone  string `json:"timezone"`
	Code      string `json:"code"`
	TimeRange string `json:"time_range"`
	Name      string `json:"name"`
}

// HandleGetShiftOverrides GET /api/schedule/shift-overrides
func HandleGetShiftOverrides(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	rows, err := database.DB.Query(`
		SELECT id, group_name, timezone, code, time_range, COALESCE(name,'')
		FROM schedule_shift_overrides
		ORDER BY group_name, timezone, code
	`)
	if err != nil {
		log.Printf("[排班] 读取班次覆盖失败: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	list := []ShiftOverride{}
	for rows.Next() {
		var o ShiftOverride
		if err := rows.Scan(&o.ID, &o.GroupName, &o.Timezone, &o.Code, &o.TimeRange, &o.Name); err != nil {
			log.Printf("[排班] WARN 扫描班次覆盖记录失败: %v", err)
			continue
		}
		list = append(list, o)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}

// HandleSaveShiftOverride POST /api/schedule/shift-overrides
// 同一 (组, 时区, 代码) 已存在时视为修改。
func HandleSaveShiftOverride(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req ShiftOverride
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	req.GroupName = strings.TrimSpace(req.GroupName)
	req.Timezone = strings.TrimSpace(req.Timezone)
	req.Code = strings.TrimSpace(req.Code)
	req.TimeRange = strings.TrimSpace(req.TimeRange)
	req.Name = strings.TrimSpace(req.Name)

	if req.GroupName == "" || req.Code == "" {
		http.Error(w, "组别和班次代码不能为空", http.StatusBadRequest)
		return
	}
	// 时区必须能解析。写错了不会立刻显形——只会让这条覆盖永远命中不到，
	// 而「命中不到」和「没配过」在界面上长得一模一样，所以在这里就打回。
	if _, err := time.LoadLocation(req.Timezone); err != nil || req.Timezone == "" || req.Timezone == "Local" {
		log.Printf("[排班] WARN 班次覆盖的时区无法识别: %q", req.Timezone)
		http.Error(w, fmt.Sprintf("无法识别的时区: %q（需要 IANA 时区名，如 Europe/Belgrade）", req.Timezone), http.StatusBadRequest)
		return
	}
	// 时间段必须能解析成真实区间，否则这个班次会被算成「不在岗」，覆盖统计凭空少一个人
	if _, _, ok := parseShiftTimeRange(req.TimeRange); !ok {
		log.Printf("[排班] WARN 班次覆盖的时间段无法解析: code=%s time_range=%q", req.Code, req.TimeRange)
		http.Error(w, fmt.Sprintf("无法解析的时间段: %q（形如 00:00-09:00 或 24:00-09:00）", req.TimeRange), http.StatusBadRequest)
		return
	}
	// 代码必须是已有的班次，否则排班表上根本排不出这个班，覆盖也就没有意义
	var exists int
	database.DB.QueryRow(`SELECT COUNT(*) FROM schedule_shift_configs WHERE code = ?`, req.Code).Scan(&exists)
	if exists == 0 {
		http.Error(w, fmt.Sprintf("班次代码 %q 不存在，请先在班次配置里添加", req.Code), http.StatusBadRequest)
		return
	}
	warnShiftDSTHazard(req.Code+"@"+req.GroupName, req.TimeRange)

	var before string
	database.DB.QueryRow(`
		SELECT time_range FROM schedule_shift_overrides WHERE group_name=? AND timezone=? AND code=?
	`, req.GroupName, req.Timezone, req.Code).Scan(&before)

	operator := r.Header.Get("X-Operator")
	if operator == "" {
		operator = "system"
	}

	if _, err := database.DB.Exec(`
		INSERT INTO schedule_shift_overrides (group_name, timezone, code, time_range, name, created_by)
		VALUES (?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE time_range=VALUES(time_range), name=VALUES(name), created_by=VALUES(created_by)
	`, req.GroupName, req.Timezone, req.Code, req.TimeRange, req.Name, operator); err != nil {
		log.Printf("[排班] 保存班次覆盖失败: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	log.Printf("[排班] 班次覆盖: 组=%s 时区=%s 班次=%s 时间段 %s -> %s 名称=%q 操作人=%s",
		req.GroupName, req.Timezone, req.Code, orDash(before), req.TimeRange, req.Name, operator)

	AddAuditLogFromRequest(r, "设置班次组覆盖", req.GroupName+"/"+req.Timezone+"/"+req.Code, operator,
		fmt.Sprintf(`{"time_range":"%s"}`, before),
		fmt.Sprintf(`{"time_range":"%s","name":"%s"}`, req.TimeRange, req.Name),
		fmt.Sprintf("设置 %s 组（%s）的班次 %s 时间段为 %s", req.GroupName, req.Timezone, req.Code, req.TimeRange))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// HandleDeleteShiftOverride DELETE /api/schedule/shift-overrides?id=
func HandleDeleteShiftOverride(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id, err := strconv.Atoi(r.URL.Query().Get("id"))
	if err != nil || id == 0 {
		http.Error(w, "Invalid id", http.StatusBadRequest)
		return
	}

	var o ShiftOverride
	if err := database.DB.QueryRow(`
		SELECT group_name, timezone, code, time_range FROM schedule_shift_overrides WHERE id = ?
	`, id).Scan(&o.GroupName, &o.Timezone, &o.Code, &o.TimeRange); err != nil {
		http.Error(w, "记录不存在", http.StatusBadRequest)
		return
	}
	if _, err := database.DB.Exec(`DELETE FROM schedule_shift_overrides WHERE id = ?`, id); err != nil {
		log.Printf("[排班] 删除班次覆盖失败: id=%d err=%v", id, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	operator := r.Header.Get("X-Operator")
	if operator == "" {
		operator = "system"
	}
	log.Printf("[排班] 删除班次覆盖: 组=%s 时区=%s 班次=%s (原 %s) 操作人=%s，该组该时区起改用全局定义",
		o.GroupName, o.Timezone, o.Code, o.TimeRange, operator)
	AddAuditLogFromRequest(r, "删除班次组覆盖", o.GroupName+"/"+o.Timezone+"/"+o.Code, operator,
		fmt.Sprintf(`{"time_range":"%s"}`, o.TimeRange), "",
		fmt.Sprintf("删除 %s 组（%s）的班次 %s 覆盖，改用全局定义", o.GroupName, o.Timezone, o.Code))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
