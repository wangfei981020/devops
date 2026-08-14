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

// ScheduleEmployee 排班员工
type ScheduleEmployee struct {
	ID          int               `json:"id"`
	Name        string            `json:"name"`
	GroupName   string            `json:"group_name"`
	Role        string            `json:"role"`
	RoleEn      string            `json:"role_en"` // v763: 职位英文名，导出 Excel 的「组别/日期」列用
	AvatarColor string            `json:"avatarColor"`
	Shifts      map[string]string `json:"shifts"`
	// v772: 时区历史（按生效日期升序）。前端按「格子日期」在这个列表里解析该天用哪个时区，
	// 所以不能只给一个「当前时区」——那样历史月份的换算会被今天的时区追溯改写。
	Timezones []EmployeeTimezone `json:"timezones"`
	// Timezone v772: 今天生效的时区，只用于列表里显示一个标签，判定一律走 Timezones
	Timezone string `json:"timezone"`
}

// HandleGetSchedule 获取排班数据
func HandleGetSchedule(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 获取年月参数
	yearStr := r.URL.Query().Get("year")
	monthStr := r.URL.Query().Get("month")

	year, _ := strconv.Atoi(yearStr)
	month, _ := strconv.Atoi(monthStr)

	if year == 0 {
		year = time.Now().Year()
	}
	if month == 0 {
		month = int(time.Now().Month())
	}

	// 获取所有员工
	employees, err := getScheduleEmployees()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 获取该月的排班数据 - 计算正确的月末日期
	startDate := fmt.Sprintf("%04d-%02d-01", year, month)
	// 使用下个月第1天减1天来获取当月最后一天
	lastDay := time.Date(year, time.Month(month+1), 0, 0, 0, 0, 0, time.UTC).Day()
	endDate := fmt.Sprintf("%04d-%02d-%02d", year, month, lastDay)

	// v772: pad=1 时前后各多取一天。跨时区视图和覆盖空档检查都需要看到边界外的班次——
	// 1 号凌晨有没有人在岗，取决于上个月最后一天的晚班；不多取这一天就会误报成空档。
	// 表格只渲染当月，多出来的两天不会影响显示，也不会进统计。
	if r.URL.Query().Get("pad") == "1" {
		base := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
		startDate = base.AddDate(0, 0, -1).Format("2006-01-02")
		endDate = base.AddDate(0, 0, lastDay).Format("2006-01-02")
	}
	log.Printf("查询排班数据: year=%d, month=%d, startDate=%s, endDate=%s, pad=%s", year, month, startDate, endDate, r.URL.Query().Get("pad"))

	for i := range employees {
		shifts, err := getEmployeeShifts(employees[i].ID, startDate, endDate)
		if err != nil {
			log.Printf("获取员工 %d 排班失败: %v", employees[i].ID, err)
			continue
		}
		employees[i].Shifts = shifts
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(employees)
}

// HandleSaveSchedule 保存排班数据
func HandleSaveSchedule(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var employees []ScheduleEmployee
	if err := json.NewDecoder(r.Body).Decode(&employees); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// 保存每个员工的排班
	for _, emp := range employees {
		// 确保员工存在
		if emp.ID == 0 {
			// 新员工，插入
			id, err := insertEmployee(emp)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			emp.ID = id
		} else {
			// 更新员工信息
			updateEmployee(emp)
		}

		// 保存排班
		for date, shiftType := range emp.Shifts {
			if shiftType == "" {
				// 删除该日期的排班
				deleteShift(emp.ID, date)
			} else {
				// 插入或更新排班
				upsertShift(emp.ID, date, shiftType)
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// HandleAddEmployee 添加员工
func HandleAddEmployee(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var emp ScheduleEmployee
	if err := json.NewDecoder(r.Body).Decode(&emp); err != nil {
		log.Printf("解析员工数据失败: %v", err)
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	log.Printf("添加员工: %s, %s", emp.Name, emp.Role)

	// 检查员工是否已存在
	var existingID int
	err := database.DB.QueryRow("SELECT id FROM schedule_employees WHERE name = ?", emp.Name).Scan(&existingID)
	if err == nil {
		// 员工已存在，返回现有记录
		log.Printf("员工已存在: %s, ID=%d", emp.Name, existingID)
		emp.ID = existingID
		emp.Shifts = make(map[string]string)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(emp)
		return
	}

	id, err := insertEmployee(emp)
	if err != nil {
		log.Printf("插入员工失败: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	emp.ID = id
	emp.Shifts = make(map[string]string)

	log.Printf("员工添加成功: ID=%d", id)

	// 记录审计日志
	operator := r.Header.Get("X-Operator")
	if operator == "" {
		operator = "system"
	}
	AddAuditLogFromRequest(r, "添加排班员工", fmt.Sprintf("%d", id), operator, "", 
		fmt.Sprintf(`{"name":"%s","role":"%s"}`, emp.Name, emp.Role), 
		fmt.Sprintf("添加排班员工: %s (%s)", emp.Name, emp.Role))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(emp)
}

// HandleDeleteEmployee 删除员工
func HandleDeleteEmployee(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	idStr := r.URL.Query().Get("id")
	id, err := strconv.Atoi(idStr)
	if err != nil || id == 0 {
		http.Error(w, "Invalid employee ID", http.StatusBadRequest)
		return
	}

	// 先删除该员工的排班记录
	database.DB.Exec("DELETE FROM schedule_shifts WHERE employee_id = ?", id)
	
	// 再删除员工
	_, err = database.DB.Exec("DELETE FROM schedule_employees WHERE id = ?", id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 记录审计日志
	operator := r.Header.Get("X-Operator")
	if operator == "" {
		operator = "system"
	}
	AddAuditLogFromRequest(r, "删除排班员工", fmt.Sprintf("%d", id), operator, "", "", 
		fmt.Sprintf("删除排班员工 ID: %d", id))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// HandleEmployeeOrder 更新员工排序
func HandleEmployeeOrder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var orders []struct {
		ID        int `json:"id"`
		SortOrder int `json:"sort_order"`
	}

	if err := json.NewDecoder(r.Body).Decode(&orders); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	for _, o := range orders {
		_, err := database.DB.Exec(`UPDATE schedule_employees SET sort_order = ? WHERE id = ?`, o.SortOrder, o.ID)
		if err != nil {
			log.Printf("更新员工排序失败: id=%d, err=%v", o.ID, err)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// HandleUpdateShift 更新单个排班
func HandleUpdateShift(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		EmployeeID int    `json:"employeeId"`
		Date       string `json:"date"`
		ShiftType  string `json:"shiftType"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("更新排班解析失败: %v", err)
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	log.Printf("更新排班: employeeId=%d, date=%s, shiftType=%s", req.EmployeeID, req.Date, req.ShiftType)

	if req.ShiftType == "" {
		deleteShift(req.EmployeeID, req.Date)
	} else {
		err := upsertShift(req.EmployeeID, req.Date, req.ShiftType)
		if err != nil {
			log.Printf("保存排班失败: %v", err)
		} else {
			log.Printf("排班保存成功")
		}
	}

	// 记录审计日志
	operator := r.Header.Get("X-Operator")
	if operator == "" {
		operator = "system"
	}
	AddAuditLogFromRequest(r, "更新排班", req.Date, operator, "", 
		fmt.Sprintf(`{"employee_id":%d,"shift_type":"%s"}`, req.EmployeeID, req.ShiftType),
		fmt.Sprintf("更新排班: 员工ID=%d, 日期=%s, 班次=%s", req.EmployeeID, req.Date, req.ShiftType))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// HandleBatchUpdateShift 批量更新排班
func HandleBatchUpdateShift(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Updates []struct {
			EmployeeID int    `json:"employee_id"`
			Date       string `json:"date"`
			ShiftCode  string `json:"shift_code"`
		} `json:"updates"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("批量更新排班解析失败: %v", err)
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	log.Printf("批量更新排班: %d 条记录", len(req.Updates))

	successCount := 0
	for i, u := range req.Updates {
		log.Printf("批量排班[%d]: employeeId=%d, date=%s, shiftCode=%s", i, u.EmployeeID, u.Date, u.ShiftCode)
		if u.ShiftCode == "" {
			deleteShift(u.EmployeeID, u.Date)
			successCount++
		} else {
			err := upsertShift(u.EmployeeID, u.Date, u.ShiftCode)
			if err != nil {
				log.Printf("批量排班保存失败: employeeId=%d, date=%s, err=%v", u.EmployeeID, u.Date, err)
			} else {
				log.Printf("批量排班保存成功: employeeId=%d, date=%s", u.EmployeeID, u.Date)
				successCount++
			}
		}
	}
	log.Printf("批量排班完成: total=%d, success=%d", len(req.Updates), successCount)

	// 记录审计日志
	operator := r.Header.Get("X-Operator")
	if operator == "" {
		operator = "system"
	}
	AddAuditLogFromRequest(r, "批量排班", "batch", operator, "",
		fmt.Sprintf(`{"total":%d,"success":%d}`, len(req.Updates), successCount),
		fmt.Sprintf("批量更新排班: 共%d条, 成功%d条", len(req.Updates), successCount))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "ok",
		"total":   len(req.Updates),
		"success": successCount,
	})
}

// 辅助函数
func getScheduleEmployees() ([]ScheduleEmployee, error) {
	rows, err := database.DB.Query(`
		SELECT id, name, COALESCE(group_name,''), role, COALESCE(role_en,''), avatar_color
		FROM schedule_employees
		ORDER BY group_name, sort_order, id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var employees []ScheduleEmployee
	for rows.Next() {
		var emp ScheduleEmployee
		if err := rows.Scan(&emp.ID, &emp.Name, &emp.GroupName, &emp.Role, &emp.RoleEn, &emp.AvatarColor); err != nil {
			continue
		}
		emp.Shifts = make(map[string]string)
		employees = append(employees, emp)
	}

	// v772: 挂上时区历史。查不到时区表不让整个排班页挂掉——降级成「全员默认时区」，
	// 但必须 WARN，否则跨时区视图会安静地按北京时间显示欧洲同事的班次。
	tzMap, tzErr := getAllEmployeeTimezones()
	if tzErr != nil {
		log.Printf("[排班] WARN 读取员工时区历史失败，全员按默认时区 %s 处理: %v", database.ScheduleDefaultTimezone, tzErr)
	}
	today := time.Now().Format("2006-01-02")
	for i := range employees {
		hist := tzMap[employees[i].ID]
		if len(hist) == 0 {
			hist = []EmployeeTimezone{{EmployeeID: employees[i].ID, Timezone: database.ScheduleDefaultTimezone, EffectiveFrom: "1970-01-01"}}
			if tzErr == nil {
				log.Printf("[排班] WARN 员工 %s(id=%d) 没有时区记录，按默认 %s 处理", employees[i].Name, employees[i].ID, database.ScheduleDefaultTimezone)
			}
		}
		sortTimezones(hist)
		employees[i].Timezones = hist
		employees[i].Timezone = resolveTimezoneAt(hist, today)
	}
	return employees, nil
}

func getEmployeeShifts(employeeID int, startDate, endDate string) (map[string]string, error) {
	rows, err := database.DB.Query(`
		SELECT DATE_FORMAT(shift_date, '%Y-%m-%d'), shift_type 
		FROM schedule_shifts 
		WHERE employee_id = ? AND shift_date BETWEEN ? AND ?
	`, employeeID, startDate, endDate)
	if err != nil {
		log.Printf("获取排班失败 employeeID=%d: %v", employeeID, err)
		return nil, err
	}
	defer rows.Close()

	shifts := make(map[string]string)
	for rows.Next() {
		var dateStr string
		var shiftType string
		if err := rows.Scan(&dateStr, &shiftType); err != nil {
			log.Printf("扫描排班数据失败: %v", err)
			continue
		}
		shifts[dateStr] = shiftType
	}
	log.Printf("员工 %d 排班数据: %d 条", employeeID, len(shifts))
	return shifts, nil
}

func insertEmployee(emp ScheduleEmployee) (int, error) {
	if emp.Role == "" {
		emp.Role = "运维工程师"
	}
	// v763: 没填英文职位时按中文职位推导，保证导出 Excel 不出现空白列
	if emp.RoleEn == "" {
		emp.RoleEn = defaultRoleEn(emp.Role)
	}
	if emp.AvatarColor == "" {
		emp.AvatarColor = "linear-gradient(135deg, #667eea, #764ba2)"
	}

	// 获取当前最大的 sort_order，新员工添加到最后
	var maxOrder int
	database.DB.QueryRow("SELECT COALESCE(MAX(sort_order), 0) FROM schedule_employees").Scan(&maxOrder)

	result, err := database.DB.Exec(`
		INSERT INTO schedule_employees (name, group_name, role, role_en, avatar_color, sort_order)
		VALUES (?, ?, ?, ?, ?, ?)
	`, emp.Name, emp.GroupName, emp.Role, emp.RoleEn, emp.AvatarColor, maxOrder+1)
	if err != nil {
		return 0, err
	}

	id, _ := result.LastInsertId()
	return int(id), nil
}

func updateEmployee(emp ScheduleEmployee) error {
	if emp.RoleEn == "" {
		emp.RoleEn = defaultRoleEn(emp.Role)
	}
	_, err := database.DB.Exec(`
		UPDATE schedule_employees
		SET name = ?, group_name = ?, role = ?, role_en = ?, avatar_color = ?
		WHERE id = ?
	`, emp.Name, emp.GroupName, emp.Role, emp.RoleEn, emp.AvatarColor, emp.ID)
	return err
}

// defaultRoleEn v763: 中文职位 -> 英文职位兜底映射（用户可在员工弹窗里自行覆盖）
// 只区分「组长」和其余，其余一律 YW Team
func defaultRoleEn(role string) string {
	if strings.Contains(role, "组长") || strings.Contains(role, "Leader") {
		return "YW Leader"
	}
	return "YW Team"
}

func upsertShift(employeeID int, date, shiftType string) error {
	_, err := database.DB.Exec(`
		INSERT INTO schedule_shifts (employee_id, shift_date, shift_type) 
		VALUES (?, ?, ?)
		ON DUPLICATE KEY UPDATE shift_type = VALUES(shift_type)
	`, employeeID, date, shiftType)
	return err
}

func deleteShift(employeeID int, date string) error {
	_, err := database.DB.Exec(`
		DELETE FROM schedule_shifts 
		WHERE employee_id = ? AND shift_date = ?
	`, employeeID, date)
	return err
}

// ShiftConfig 班次配置
type ShiftConfig struct {
	Code   string `json:"code"`
	Label  string `json:"label"`
	Name   string `json:"name"`
	Time   string `json:"time"`
	TimeEn string `json:"time_en"` // v763: 英文说明，导出 Excel 图例的「时间」列用
	Color  string `json:"color"`
	IsDuty bool   `json:"isDuty"`
	// IsWorking v772: 是否算「有人在岗」，覆盖空档检查用。
	// ⚠️ 不能拿 IsDuty 顶替：IsDuty 是「值班」（A+ 是值班但 A 不是，两个都算在岗），
	// 而 OD/OFF/H/PL/SL/AL/CT 这些休假类两个标记都不该为真。
	IsWorking bool `json:"isWorking"`
}

// HandleGetShiftConfig 获取班次配置
func HandleGetShiftConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	rows, err := database.DB.Query(`
		SELECT code, label, name, time_range, COALESCE(time_en,''), color, is_duty,
		       COALESCE(is_working, CASE WHEN time_range IS NULL OR time_range = '' OR time_range = '-' THEN 0 ELSE 1 END)
		FROM schedule_shift_configs
		ORDER BY sort_order, id
	`)
	if err != nil {
		// 如果表不存在或查询失败，返回空数组
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]ShiftConfig{})
		return
	}
	defer rows.Close()

	var configs []ShiftConfig
	for rows.Next() {
		var cfg ShiftConfig
		if err := rows.Scan(&cfg.Code, &cfg.Label, &cfg.Name, &cfg.Time, &cfg.TimeEn, &cfg.Color, &cfg.IsDuty, &cfg.IsWorking); err != nil {
			log.Printf("[排班] WARN 扫描班次配置失败: %v", err)
			continue
		}
		configs = append(configs, cfg)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(configs)
}

// HandleSaveShiftConfig 保存班次配置
func HandleSaveShiftConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var configs []ShiftConfig
	if err := json.NewDecoder(r.Body).Decode(&configs); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// 开始事务
	tx, err := database.DB.Begin()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 删除所有现有配置
	_, err = tx.Exec("DELETE FROM schedule_shift_configs")
	if err != nil {
		tx.Rollback()
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 插入新配置
	for i, cfg := range configs {
		// v763: 英文说明留空时按时间段兜底，导出图例不会出现空格子
		timeEn := cfg.TimeEn
		if timeEn == "" {
			if cfg.Time == "" || cfg.Time == "-" {
				timeEn = "Duty"
			} else {
				timeEn = strings.ReplaceAll(cfg.Time, "-", " - ")
			}
			log.Printf("[排班] 班次 %s 未填英文说明，按时间段兜底为 %q", cfg.Code, timeEn)
		}

		// v772: 在岗标记与时间段必须自洽——没有可解析的时间段就贡献不了任何覆盖时段，
		// 标成「在岗」只会让覆盖空档图显示成绿的、实际却没人。这里强制打回并 WARN。
		isWorking := cfg.IsWorking
		_, _, parsable := parseShiftTimeRange(cfg.Time)
		if isWorking && !parsable {
			log.Printf("[排班] WARN 班次 %s 时间段为 %q 无法解析，「在岗」标记被强制改为否（否则覆盖空档检查会漏报）", cfg.Code, cfg.Time)
			isWorking = false
		}
		warnShiftDSTHazard(cfg.Code, cfg.Time)

		_, err = tx.Exec(`
			INSERT INTO schedule_shift_configs (code, label, name, time_range, time_en, color, is_duty, is_working, sort_order)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, cfg.Code, cfg.Label, cfg.Name, cfg.Time, timeEn, cfg.Color, cfg.IsDuty, isWorking, i)
		if err != nil {
			tx.Rollback()
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 记录审计日志
	operator := r.Header.Get("X-Operator")
	if operator == "" {
		operator = "system"
	}
	
	configJSON, _ := json.Marshal(configs)
	AddAuditLogFromRequest(r, "保存班次配置", "shift_config", operator, "", 
		string(configJSON),
		fmt.Sprintf("保存班次配置: %d 个班次", len(configs)))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// HandleResetSchedule 重置指定月份的排班数据
func HandleResetSchedule(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Year  int `json:"year"`
		Month int `json:"month"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.Year == 0 || req.Month == 0 {
		http.Error(w, "年份和月份必填", http.StatusBadRequest)
		return
	}

	// 计算该月的日期范围
	startDate := fmt.Sprintf("%04d-%02d-01", req.Year, req.Month)
	lastDay := time.Date(req.Year, time.Month(req.Month+1), 0, 0, 0, 0, 0, time.UTC).Day()
	endDate := fmt.Sprintf("%04d-%02d-%02d", req.Year, req.Month, lastDay)

	// 删除该月的所有排班数据
	result, err := database.DB.Exec(`
		DELETE FROM schedule_shifts 
		WHERE shift_date >= ? AND shift_date <= ?
	`, startDate, endDate)

	if err != nil {
		log.Printf("重置排班失败: %v", err)
		http.Error(w, "重置排班失败", http.StatusInternalServerError)
		return
	}

	rowsAffected, _ := result.RowsAffected()
	log.Printf("重置排班成功: year=%d, month=%d, startDate=%s, endDate=%s, deleted=%d",
		req.Year, req.Month, startDate, endDate, rowsAffected)

	// 记录审计日志
	operator := r.Header.Get("X-Operator")
	if operator == "" {
		operator = "system"
	}

	AddAuditLogFromRequest(r, "重置排班", "schedule", operator, "",
		fmt.Sprintf("重置 %d年%d月 的排班数据", req.Year, req.Month),
		fmt.Sprintf("删除了 %d 条排班记录", rowsAffected))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "ok",
		"message": fmt.Sprintf("已重置 %d年%d月 的排班数据，共删除 %d 条记录", req.Year, req.Month, rowsAffected),
		"deleted": rowsAffected,
	})
}

// ScheduleContact 联系人
type ScheduleContact struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	Phone      string `json:"phone"`
	Department string `json:"department"`
	Position   string `json:"position"`
	Remark     string `json:"remark"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
}

// HandleGetContacts 获取联系人列表
func HandleGetContacts(w http.ResponseWriter, r *http.Request) {
	rows, err := database.DB.Query(`
		SELECT id, name, phone, COALESCE(department, '') as department, 
		       COALESCE(position, '') as position, COALESCE(remark, '') as remark,
		       created_at, updated_at
		FROM schedule_contacts
		ORDER BY name ASC
	`)
	if err != nil {
		log.Printf("获取联系人列表失败: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var contacts []ScheduleContact
	for rows.Next() {
		var c ScheduleContact
		err := rows.Scan(&c.ID, &c.Name, &c.Phone, &c.Department, &c.Position, &c.Remark, &c.CreatedAt, &c.UpdatedAt)
		if err != nil {
			log.Printf("扫描联系人数据失败: %v", err)
			continue
		}
		contacts = append(contacts, c)
	}

	if contacts == nil {
		contacts = []ScheduleContact{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(contacts)
}

// HandleAddContact 添加联系人
func HandleAddContact(w http.ResponseWriter, r *http.Request) {
	var contact ScheduleContact
	if err := json.NewDecoder(r.Body).Decode(&contact); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if contact.Name == "" || contact.Phone == "" {
		http.Error(w, "姓名和电话必填", http.StatusBadRequest)
		return
	}

	result, err := database.DB.Exec(`
		INSERT INTO schedule_contacts (name, phone, department, position, remark)
		VALUES (?, ?, ?, ?, ?)
	`, contact.Name, contact.Phone, contact.Department, contact.Position, contact.Remark)

	if err != nil {
		log.Printf("添加联系人失败: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	id, _ := result.LastInsertId()
	contact.ID = int(id)

	// 记录审计日志
	operator := r.Header.Get("X-Operator")
	if operator == "" {
		operator = "system"
	}
	AddAuditLogFromRequest(r, "添加联系人", fmt.Sprintf("%d", id), operator, "",
		fmt.Sprintf(`{"name":"%s","phone":"%s","department":"%s"}`, contact.Name, contact.Phone, contact.Department),
		fmt.Sprintf("添加联系人: %s (%s)", contact.Name, contact.Phone))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(contact)
}

// HandleUpdateContact 更新联系人
func HandleUpdateContact(w http.ResponseWriter, r *http.Request) {
	// 从 URL 获取 ID
	idStr := r.URL.Path[len("/api/schedule/contacts/"):]
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	var contact ScheduleContact
	if err := json.NewDecoder(r.Body).Decode(&contact); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if contact.Name == "" || contact.Phone == "" {
		http.Error(w, "姓名和电话必填", http.StatusBadRequest)
		return
	}

	_, err = database.DB.Exec(`
		UPDATE schedule_contacts 
		SET name = ?, phone = ?, department = ?, position = ?, remark = ?, updated_at = NOW()
		WHERE id = ?
	`, contact.Name, contact.Phone, contact.Department, contact.Position, contact.Remark, id)

	if err != nil {
		log.Printf("更新联系人失败: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	contact.ID = id

	// 记录审计日志
	operator := r.Header.Get("X-Operator")
	if operator == "" {
		operator = "system"
	}
	AddAuditLogFromRequest(r, "更新联系人", fmt.Sprintf("%d", id), operator, "",
		fmt.Sprintf(`{"name":"%s","phone":"%s","department":"%s"}`, contact.Name, contact.Phone, contact.Department),
		fmt.Sprintf("更新联系人: %s (%s)", contact.Name, contact.Phone))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(contact)
}

// HandleDeleteContact 删除联系人
func HandleDeleteContact(w http.ResponseWriter, r *http.Request) {
	// 从 URL 获取 ID
	idStr := r.URL.Path[len("/api/schedule/contacts/"):]
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	_, err = database.DB.Exec(`DELETE FROM schedule_contacts WHERE id = ?`, id)
	if err != nil {
		log.Printf("删除联系人失败: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 记录审计日志
	operator := r.Header.Get("X-Operator")
	if operator == "" {
		operator = "system"
	}
	AddAuditLogFromRequest(r, "删除联系人", fmt.Sprintf("%d", id), operator, "", "",
		fmt.Sprintf("删除联系人 ID: %d", id))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// ============================================================
// v733: 排班月度应工作天数（schedule_month_target）
// 给排班统计分析页判定 达成/缺勤/超勤 用. 一行/(year,month).
// ============================================================

type ScheduleMonthTarget struct {
	Year             int    `json:"year"`
	Month            int    `json:"month"`
	ExpectedWorkDays int    `json:"expected_work_days"`
	UpdatedBy        string `json:"updated_by"`
	UpdatedAt        string `json:"updated_at"`
}

// HandleGetMonthTarget GET /api/schedule/month-target?year=Y&month=M
// 未配置时返回 expected_work_days=0，前端按"未设定"处理.
func HandleGetMonthTarget(w http.ResponseWriter, r *http.Request) {
	year, _ := strconv.Atoi(r.URL.Query().Get("year"))
	month, _ := strconv.Atoi(r.URL.Query().Get("month"))
	if year == 0 || month < 1 || month > 12 {
		http.Error(w, "year/month 参数无效", http.StatusBadRequest)
		return
	}
	var t ScheduleMonthTarget
	var updatedAt time.Time
	err := database.DB.QueryRow(`
		SELECT year, month, expected_work_days, updated_by, updated_at
		FROM schedule_month_target WHERE year=? AND month=?
	`, year, month).Scan(&t.Year, &t.Month, &t.ExpectedWorkDays, &t.UpdatedBy, &updatedAt)
	if err != nil {
		// 没配过：返回零值（前端兜底）
		t = ScheduleMonthTarget{Year: year, Month: month, ExpectedWorkDays: 0}
	} else {
		t.UpdatedAt = updatedAt.Format("2006-01-02 15:04:05")
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(t)
}

// HandleSetMonthTarget PUT /api/schedule/month-target
// body: { year, month, expected_work_days }
// 需要 schedule_analytics:set_target 按钮权限（admin 自动放行）.
func HandleSetMonthTarget(w http.ResponseWriter, r *http.Request) {
	// 权限校验：HasAnyPermission 内部已对 admin/super_admin 自动放行
	if !HasAnyPermission(r, "schedule_analytics:set_target") {
		http.Error(w, "没有修改应工作天数的权限", http.StatusForbidden)
		return
	}

	var in ScheduleMonthTarget
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	if in.Year < 2000 || in.Year > 2100 || in.Month < 1 || in.Month > 12 {
		http.Error(w, "year/month 参数无效", http.StatusBadRequest)
		return
	}
	if in.ExpectedWorkDays < 0 || in.ExpectedWorkDays > 31 {
		http.Error(w, "expected_work_days 必须在 0-31 之间", http.StatusBadRequest)
		return
	}
	operator := r.Header.Get("X-Operator")
	if operator == "" {
		operator = "system"
	}
	_, err := database.DB.Exec(`
		INSERT INTO schedule_month_target (year, month, expected_work_days, updated_by)
		VALUES (?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			expected_work_days = VALUES(expected_work_days),
			updated_by = VALUES(updated_by)
	`, in.Year, in.Month, in.ExpectedWorkDays, operator)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	AddAuditLogFromRequest(r, "修改月度应工作天数",
		fmt.Sprintf("%04d-%02d", in.Year, in.Month), operator, "", "",
		fmt.Sprintf("设为 %d 天", in.ExpectedWorkDays))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":             "ok",
		"year":               in.Year,
		"month":              in.Month,
		"expected_work_days": in.ExpectedWorkDays,
	})
}
