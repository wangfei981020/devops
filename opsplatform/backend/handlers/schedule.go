package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"opsplatform/database"
)

// ScheduleEmployee 排班员工
type ScheduleEmployee struct {
	ID          int               `json:"id"`
	Name        string            `json:"name"`
	Role        string            `json:"role"`
	AvatarColor string            `json:"avatarColor"`
	Shifts      map[string]string `json:"shifts"`
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

	// 获取该月的排班数据
	startDate := fmt.Sprintf("%04d-%02d-01", year, month)
	endDate := fmt.Sprintf("%04d-%02d-31", year, month)

	for i := range employees {
		shifts, err := getEmployeeShifts(employees[i].ID, startDate, endDate)
		if err != nil {
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

// 辅助函数
func getScheduleEmployees() ([]ScheduleEmployee, error) {
	rows, err := database.DB.Query(`
		SELECT id, name, role, avatar_color 
		FROM schedule_employees 
		ORDER BY sort_order, id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var employees []ScheduleEmployee
	for rows.Next() {
		var emp ScheduleEmployee
		if err := rows.Scan(&emp.ID, &emp.Name, &emp.Role, &emp.AvatarColor); err != nil {
			continue
		}
		emp.Shifts = make(map[string]string)
		employees = append(employees, emp)
	}
	return employees, nil
}

func getEmployeeShifts(employeeID int, startDate, endDate string) (map[string]string, error) {
	rows, err := database.DB.Query(`
		SELECT shift_date, shift_type 
		FROM schedule_shifts 
		WHERE employee_id = ? AND shift_date BETWEEN ? AND ?
	`, employeeID, startDate, endDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	shifts := make(map[string]string)
	for rows.Next() {
		var date time.Time
		var shiftType string
		if err := rows.Scan(&date, &shiftType); err != nil {
			continue
		}
		shifts[date.Format("2006-01-02")] = shiftType
	}
	return shifts, nil
}

func insertEmployee(emp ScheduleEmployee) (int, error) {
	if emp.Role == "" {
		emp.Role = "运维工程师"
	}
	if emp.AvatarColor == "" {
		emp.AvatarColor = "linear-gradient(135deg, #667eea, #764ba2)"
	}

	result, err := database.DB.Exec(`
		INSERT INTO schedule_employees (name, role, avatar_color) 
		VALUES (?, ?, ?)
	`, emp.Name, emp.Role, emp.AvatarColor)
	if err != nil {
		return 0, err
	}

	id, _ := result.LastInsertId()
	return int(id), nil
}

func updateEmployee(emp ScheduleEmployee) error {
	_, err := database.DB.Exec(`
		UPDATE schedule_employees 
		SET name = ?, role = ?, avatar_color = ? 
		WHERE id = ?
	`, emp.Name, emp.Role, emp.AvatarColor, emp.ID)
	return err
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
	Color  string `json:"color"`
	IsDuty bool   `json:"isDuty"`
}

// HandleGetShiftConfig 获取班次配置
func HandleGetShiftConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	rows, err := database.DB.Query(`
		SELECT code, label, name, time_range, color, is_duty 
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
		if err := rows.Scan(&cfg.Code, &cfg.Label, &cfg.Name, &cfg.Time, &cfg.Color, &cfg.IsDuty); err != nil {
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
		_, err = tx.Exec(`
			INSERT INTO schedule_shift_configs (code, label, name, time_range, color, is_duty, sort_order) 
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, cfg.Code, cfg.Label, cfg.Name, cfg.Time, cfg.Color, cfg.IsDuty, i)
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
