package handlers

import (
	"fmt"
	"log"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"opsplatform/database"

	"github.com/xuri/excelize/v2"
)

// v763: 排班表导出 Excel（带班次填充色、按组分块、底部图例）
//
// 旧实现是前端手工拼 CSV，CSV 装不下颜色/合并单元格/列宽，Excel 打开是全白的。
// 这里改成后端 excelize 直接产 xlsx，颜色来源是 schedule_shift_configs.color，
// 也就是「班次配置」页改了颜色，导出的表跟着变，不写死。

const maxExportMonths = 12 // 一次最多导 12 个月，防止区间填太大把内存撑爆

// 表格布局：A=姓名，B=职位英文，C 起是 1..31 号
const (
	colName = 1
	colRole = 2
	colDay1 = 3
)

// exportStyles 一个 sheet 内复用的样式集合。
// excelize 每次 NewStyle 都会新建一条样式记录，31 天 × N 人挨个建会把文件撑大，所以全部缓存。
type exportStyles struct {
	f          *excelize.File
	corner     int         // 左上角「姓名 / 组别/日期」表头
	dayNum     int         // 第 1 行日号
	weekDay    int         // 第 2 行星期（工作日）
	weekEnd    int         // 第 2 行星期（周六日，橙红字）
	nameCell   int         // 姓名格
	roleCell   int         // 职位格
	emptyShift int         // 没排班的空格子（只有边框）
	legendHead int         // 图例表头
	legendCell int         // 图例普通格
	shiftCache map[string]int
}

func borderAll() []excelize.Border {
	return []excelize.Border{
		{Type: "left", Color: "B0B0B0", Style: 1},
		{Type: "right", Color: "B0B0B0", Style: 1},
		{Type: "top", Color: "B0B0B0", Style: 1},
		{Type: "bottom", Color: "B0B0B0", Style: 1},
	}
}

func centerAlign() *excelize.Alignment {
	return &excelize.Alignment{Horizontal: "center", Vertical: "center"}
}

func newExportStyles(f *excelize.File) (*exportStyles, error) {
	s := &exportStyles{f: f, shiftCache: make(map[string]int)}
	var err error

	mk := func(font *excelize.Font, fillColor string) (int, error) {
		style := &excelize.Style{
			Border:    borderAll(),
			Alignment: centerAlign(),
			Font:      font,
		}
		if fillColor != "" {
			style.Fill = excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{fillColor}}
		}
		return f.NewStyle(style)
	}

	if s.corner, err = mk(&excelize.Font{Bold: true, Size: 10}, "F2F2F2"); err != nil {
		return nil, err
	}
	if s.dayNum, err = mk(&excelize.Font{Bold: true, Size: 10}, "F2F2F2"); err != nil {
		return nil, err
	}
	if s.weekDay, err = mk(&excelize.Font{Size: 9}, "F2F2F2"); err != nil {
		return nil, err
	}
	// 周六日星期用橙红字标出来，跟参考表一致
	if s.weekEnd, err = mk(&excelize.Font{Size: 9, Color: "E36C09"}, "F2F2F2"); err != nil {
		return nil, err
	}
	if s.nameCell, err = mk(&excelize.Font{Size: 10}, ""); err != nil {
		return nil, err
	}
	if s.roleCell, err = mk(&excelize.Font{Size: 9}, ""); err != nil {
		return nil, err
	}
	if s.emptyShift, err = mk(&excelize.Font{Size: 10}, ""); err != nil {
		return nil, err
	}
	if s.legendHead, err = mk(&excelize.Font{Bold: true, Size: 10}, "F2F2F2"); err != nil {
		return nil, err
	}
	if s.legendCell, err = mk(&excelize.Font{Size: 10}, ""); err != nil {
		return nil, err
	}
	return s, nil
}

// shift 返回某个班次颜色对应的格子样式（整格填色 + 自动黑白字）
func (s *exportStyles) shift(hexColor string) int {
	fill := normalizeHexColor(hexColor)
	if id, ok := s.shiftCache[fill]; ok {
		return id
	}
	fontColor := shiftFontColor(fill)
	id, err := s.f.NewStyle(&excelize.Style{
		Border:    borderAll(),
		Alignment: centerAlign(),
		Font:      &excelize.Font{Size: 10, Bold: true, Color: fontColor},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{fill}},
	})
	if err != nil {
		log.Printf("[排班导出] WARN 创建班次样式失败 color=%s: %v", hexColor, err)
		return s.emptyShift
	}
	s.shiftCache[fill] = id
	return id
}

// normalizeHexColor 把 #3a84ff / 3a84ff / #fff 统一成 excelize 要的 6 位大写 RRGGBB
func normalizeHexColor(c string) string {
	c = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(c), "#"))
	if len(c) == 3 {
		c = string([]byte{c[0], c[0], c[1], c[1], c[2], c[2]})
	}
	if len(c) != 6 {
		log.Printf("[排班导出] WARN 无法识别的颜色值 %q，回退成灰色", c)
		return "CCCCCC"
	}
	return strings.ToUpper(c)
}

// shiftFontColor 按 WCAG 相对亮度算白字对比度，低于 3:1 就换深字，保证怎么配色都能看清
// ⚠️ 判定规则要和前端 ScheduleView.vue 的 shiftTextColor 保持一致，否则页面和导出的表两个样
func shiftFontColor(hex6 string) string {
	if contrastWithWhite(hex6) < 3 {
		return "1F2937"
	}
	return "FFFFFF"
}

func contrastWithWhite(hex6 string) float64 {
	return 1.05 / (relativeLuminance(hex6) + 0.05)
}

func relativeLuminance(hex6 string) float64 {
	v, err := strconv.ParseUint(hex6, 16, 32)
	if err != nil {
		log.Printf("[排班导出] WARN 颜色值 %q 解析失败，按深色处理", hex6)
		return 0
	}
	chan8 := func(shift uint) float64 {
		c := float64((v>>shift)&0xFF) / 255
		if c <= 0.03928 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	return 0.2126*chan8(16) + 0.7152*chan8(8) + 0.0722*chan8(0)
}

var weekDayEn = [7]string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}

// HandleExportSchedule 导出排班表 Excel
// GET /api/schedule/export?start=2026-08&end=2026-08  —— 每个月一个 sheet
func HandleExportSchedule(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	_, username, role := GetUserFromContext(r)
	ok, err := UserHasPermission(username, role, "schedule:export")
	if err != nil {
		log.Printf("[排班导出] 权限检查失败 username=%s: %v", username, err)
		http.Error(w, "权限检查失败", http.StatusInternalServerError)
		return
	}
	if !ok {
		log.Printf("[排班导出] 权限不足 username=%s", username)
		http.Error(w, "权限不足", http.StatusForbidden)
		return
	}

	months, err := parseExportMonths(r.URL.Query().Get("start"), r.URL.Query().Get("end"))
	if err != nil {
		log.Printf("[排班导出] 参数错误 start=%q end=%q: %v", r.URL.Query().Get("start"), r.URL.Query().Get("end"), err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	employees, err := getScheduleEmployees()
	if err != nil {
		log.Printf("[排班导出] 查询员工失败: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	configs, err := getShiftConfigs()
	if err != nil {
		log.Printf("[排班导出] 查询班次配置失败: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if len(configs) == 0 {
		log.Printf("[排班导出] WARN 班次配置为空，导出的表将没有颜色和图例")
	}
	// v776: 班次的按组覆盖。不带上的话，导出的图例只会写全局定义，
	// 而 ig 的 C 实际是 00:00-09:00 —— 拿着这张表的人会按 24:00-09:00 理解，差一整天。
	overrides, ovErr := getShiftOverridesForExport()
	if ovErr != nil {
		log.Printf("[排班导出] WARN 读取班次组覆盖失败，图例将只反映全局定义: %v", ovErr)
	}

	f := excelize.NewFile()
	defer func() {
		if cerr := f.Close(); cerr != nil {
			log.Printf("[排班导出] WARN 关闭工作簿失败: %v", cerr)
		}
	}()

	for i, m := range months {
		sheet := m.Format("2006-01")
		if i == 0 {
			// 第一个月直接复用默认的 Sheet1，避免留一个空白 sheet
			if err := f.SetSheetName(f.GetSheetName(0), sheet); err != nil {
				log.Printf("[排班导出] 重命名默认 sheet 失败: %v", err)
			}
		} else if _, err := f.NewSheet(sheet); err != nil {
			log.Printf("[排班导出] 新建 sheet %s 失败: %v", sheet, err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := writeScheduleSheet(f, sheet, m, employees, configs, overrides); err != nil {
			log.Printf("[排班导出] 写 sheet %s 失败: %v", sheet, err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	if idx, err := f.GetSheetIndex(months[0].Format("2006-01")); err == nil {
		f.SetActiveSheet(idx)
	}

	filename := fmt.Sprintf("排班表_%s.xlsx", months[0].Format("2006年01月"))
	if len(months) > 1 {
		filename = fmt.Sprintf("排班表_%s至%s.xlsx",
			months[0].Format("2006年01月"), months[len(months)-1].Format("2006年01月"))
	}

	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	// 中文文件名走 RFC 5987 的 filename*，同时给个 ASCII 兜底
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="schedule.xlsx"; filename*=UTF-8''%s`, url.PathEscape(filename)))
	w.Header().Set("Cache-Control", "no-store")

	if err := f.Write(w); err != nil {
		log.Printf("[排班导出] 写响应失败: %v", err)
		return
	}
	log.Printf("[排班导出] 完成 username=%s 月份=%d 员工=%d 班次=%d 文件=%s",
		username, len(months), len(employees), len(configs), filename)
}

// parseExportMonths 解析 start/end（格式 2026-08），返回区间内每个月的首日
func parseExportMonths(start, end string) ([]time.Time, error) {
	now := time.Now()
	parse := func(s string) (time.Time, error) {
		if s == "" {
			return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.Local), nil
		}
		t, err := time.ParseInLocation("2006-01", s, time.Local)
		if err != nil {
			return time.Time{}, fmt.Errorf("月份格式应为 YYYY-MM，收到 %q", s)
		}
		return t, nil
	}

	from, err := parse(start)
	if err != nil {
		return nil, err
	}
	to := from
	if end != "" {
		if to, err = parse(end); err != nil {
			return nil, err
		}
	}
	if to.Before(from) {
		return nil, fmt.Errorf("结束月份不能早于开始月份")
	}

	var months []time.Time
	for m := from; !m.After(to); m = m.AddDate(0, 1, 0) {
		months = append(months, m)
		if len(months) > maxExportMonths {
			return nil, fmt.Errorf("一次最多导出 %d 个月", maxExportMonths)
		}
	}
	return months, nil
}

// getShiftConfigs 读班次配置（颜色 / 英文说明 / 中文名都在这张表里）
// getShiftOverridesForExport v776: 导出用的班次组覆盖。
// 图例只列全局定义的话，ig 的 C 会被写成 24:00-09:00，而他们实际是 00:00-09:00——
// 拿着这张表排班的人会差一整天，所以必须在表里把差异写清楚。
func getShiftOverridesForExport() ([]ShiftOverride, error) {
	rows, err := database.DB.Query(`
		SELECT group_name, timezone, code, time_range, COALESCE(name,'')
		FROM schedule_shift_overrides
		ORDER BY group_name, timezone, code
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []ShiftOverride
	for rows.Next() {
		var o ShiftOverride
		if err := rows.Scan(&o.GroupName, &o.Timezone, &o.Code, &o.TimeRange, &o.Name); err != nil {
			log.Printf("[排班导出] WARN 扫描班次覆盖失败: %v", err)
			continue
		}
		list = append(list, o)
	}
	return list, nil
}

func getShiftConfigs() ([]ShiftConfig, error) {
	rows, err := database.DB.Query(`
		SELECT code, label, name, time_range, COALESCE(time_en,''), color, is_duty
		FROM schedule_shift_configs
		ORDER BY sort_order, id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var configs []ShiftConfig
	for rows.Next() {
		var cfg ShiftConfig
		if err := rows.Scan(&cfg.Code, &cfg.Label, &cfg.Name, &cfg.Time, &cfg.TimeEn, &cfg.Color, &cfg.IsDuty); err != nil {
			log.Printf("[排班导出] WARN 扫描班次配置失败: %v", err)
			continue
		}
		configs = append(configs, cfg)
	}
	return configs, rows.Err()
}

// writeScheduleSheet 写一个月的排班表
func writeScheduleSheet(f *excelize.File, sheet string, month time.Time, employees []ScheduleEmployee, configs []ShiftConfig, overrides []ShiftOverride) error {
	styles, err := newExportStyles(f)
	if err != nil {
		return err
	}

	colorByCode := make(map[string]string, len(configs))
	for _, c := range configs {
		colorByCode[c.Code] = c.Color
	}

	year, mon := month.Year(), int(month.Month())
	lastDay := time.Date(year, time.Month(mon+1), 0, 0, 0, 0, 0, time.Local).Day()
	startDate := fmt.Sprintf("%04d-%02d-01", year, mon)
	endDate := fmt.Sprintf("%04d-%02d-%02d", year, mon, lastDay)

	// ===== 表头两行：第 1 行日号，第 2 行英文星期 =====
	f.SetCellValue(sheet, "A1", "姓名")
	f.SetCellValue(sheet, "B1", "组别/日期")
	if err := f.MergeCell(sheet, "A1", "A2"); err != nil {
		return err
	}
	if err := f.MergeCell(sheet, "B1", "B2"); err != nil {
		return err
	}
	f.SetCellStyle(sheet, "A1", "B2", styles.corner)

	for d := 1; d <= lastDay; d++ {
		col := colDay1 + d - 1
		date := time.Date(year, time.Month(mon), d, 0, 0, 0, 0, time.Local)
		weekday := date.Weekday()

		numCell, _ := excelize.CoordinatesToCellName(col, 1)
		weekCell, _ := excelize.CoordinatesToCellName(col, 2)
		f.SetCellValue(sheet, numCell, d)
		f.SetCellValue(sheet, weekCell, weekDayEn[weekday])
		f.SetCellStyle(sheet, numCell, numCell, styles.dayNum)
		if weekday == time.Saturday || weekday == time.Sunday {
			f.SetCellStyle(sheet, weekCell, weekCell, styles.weekEnd)
		} else {
			f.SetCellStyle(sheet, weekCell, weekCell, styles.weekDay)
		}
	}

	// ===== 主体：按组分块，组之间空一行 =====
	// getScheduleEmployees 已按 group_name, sort_order 排好，这里顺序遍历即可，
	// 组的先后和组内顺序跟页面上看到的一致（含拖拽排序结果）。
	row := 3
	prevGroup := ""
	firstGroup := true
	shiftCount := 0

	for _, emp := range employees {
		if !firstGroup && emp.GroupName != prevGroup {
			row++ // 换组，空一整行
		}
		prevGroup = emp.GroupName
		firstGroup = false

		shifts, err := getEmployeeShifts(emp.ID, startDate, endDate)
		if err != nil {
			log.Printf("[排班导出] WARN 员工 %s(%d) 排班读取失败，本行留空: %v", emp.Name, emp.ID, err)
			shifts = map[string]string{}
		}

		nameCell, _ := excelize.CoordinatesToCellName(colName, row)
		roleCell, _ := excelize.CoordinatesToCellName(colRole, row)
		f.SetCellValue(sheet, nameCell, emp.Name)
		f.SetCellValue(sheet, roleCell, employeeRoleEn(emp))
		f.SetCellStyle(sheet, nameCell, nameCell, styles.nameCell)
		f.SetCellStyle(sheet, roleCell, roleCell, styles.roleCell)

		for d := 1; d <= lastDay; d++ {
			col := colDay1 + d - 1
			cell, _ := excelize.CoordinatesToCellName(col, row)
			code := shifts[fmt.Sprintf("%04d-%02d-%02d", year, mon, d)]
			if code == "" {
				f.SetCellStyle(sheet, cell, cell, styles.emptyShift)
				continue
			}
			shiftCount++
			f.SetCellValue(sheet, cell, code)
			color, known := colorByCode[code]
			if !known {
				// 班次配置里查不到这个代码：数据比配置新，或者配置被删了。填灰底并报警，别静默出白格
				log.Printf("[排班导出] WARN 未识别的班次代码 %q（员工 %s，%04d-%02d-%02d），按灰色处理", code, emp.Name, year, mon, d)
				color = "#CCCCCC"
			}
			f.SetCellStyle(sheet, cell, cell, styles.shift(color))
		}
		f.SetRowHeight(sheet, row, 16)
		row++
	}

	// ===== 底部图例：班次 | 时间 | 颜色 | 备注 =====
	legendTop := row + 1
	headers := []string{"班次", "时间", "颜色", "备注"}
	for i, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(colName+i, legendTop)
		f.SetCellValue(sheet, cell, h)
		f.SetCellStyle(sheet, cell, cell, styles.legendHead)
	}
	for i, cfg := range configs {
		r := legendTop + 1 + i
		codeCell, _ := excelize.CoordinatesToCellName(1, r)
		timeCell, _ := excelize.CoordinatesToCellName(2, r)
		colorCell, _ := excelize.CoordinatesToCellName(3, r)
		nameCell, _ := excelize.CoordinatesToCellName(4, r)

		f.SetCellValue(sheet, codeCell, cfg.Code)
		f.SetCellValue(sheet, timeCell, legendTimeEn(cfg))
		f.SetCellValue(sheet, colorCell, cfg.Code) // 色块格里写代码，一眼对得上
		f.SetCellValue(sheet, nameCell, cfg.Name)

		f.SetCellStyle(sheet, codeCell, timeCell, styles.legendCell)
		f.SetCellStyle(sheet, colorCell, colorCell, styles.shift(cfg.Color))
		f.SetCellStyle(sheet, nameCell, nameCell, styles.legendCell)
	}

	// ===== 班次差异说明 v776 =====
	// 图例里的时间段是全局定义。某些组的班次口径不一样（ig 的 C 是当天 00:00-09:00，
	// 全局是 24:00-09:00 跨到次日），不写出来的话看表的人会整整差一天。
	ovRow := legendTop + 1 + len(configs) + 1
	// 只列时间段真的和全局不同的。有些覆盖只是为了改叫法（ig 管 09:00-18:00 叫「中班」），
	// 时段一模一样，写成「= 09:00-18:00（图例为 09:00-18:00）」纯属噪音，
	// 会把真正有差异的那条（C）淹掉。
	diffOverrides := make([]ShiftOverride, 0, len(overrides))
	for _, o := range overrides {
		base := ""
		for _, c := range configs {
			if c.Code == o.Code {
				base = c.Time
				break
			}
		}
		if strings.TrimSpace(base) != strings.TrimSpace(o.TimeRange) {
			diffOverrides = append(diffOverrides, o)
		}
	}
	overrides = diffOverrides
	if len(overrides) > 0 {
		headCell, _ := excelize.CoordinatesToCellName(colName, ovRow)
		f.SetCellValue(sheet, headCell, "班次差异 / Shift differences")
		f.SetCellStyle(sheet, headCell, headCell, styles.legendHead)
		ovRow++
		noteCell, _ := excelize.CoordinatesToCellName(colName, ovRow)
		f.SetCellValue(sheet, noteCell, "下列组的班次时间段与上方图例不同，以本节为准")
		f.SetCellStyle(sheet, noteCell, noteCell, styles.legendCell)
		ovRow++
		for _, o := range overrides {
			base := ""
			for _, c := range configs {
				if c.Code == o.Code {
					base = c.Time
					break
				}
			}
			label := o.Code
			if o.Name != "" {
				label = fmt.Sprintf("%s（%s）", o.Code, o.Name)
			}
			gCell, _ := excelize.CoordinatesToCellName(colName, ovRow)
			dCell, _ := excelize.CoordinatesToCellName(colName+1, ovRow)
			f.SetCellValue(sheet, gCell, fmt.Sprintf("%s / %s", o.GroupName, o.Timezone))
			f.SetCellValue(sheet, dCell, fmt.Sprintf("%s = %s（图例为 %s）", label, o.TimeRange, base))
			f.SetCellStyle(sheet, gCell, dCell, styles.legendCell)
			ovRow++
		}
		ovRow++
		log.Printf("[排班导出] sheet=%s 写入班次差异说明 %d 条", sheet, len(overrides))
	}

	// ===== 时区说明 v772 =====
	// 图例里的时间是「员工本地时间」。不写这句的话，导出的表发给别人，
	// 欧洲同事的 09:00 会被当成北京 09:00 理解，而这两者差 6~7 小时。
	// 接在班次差异块之后。原来是从图例末尾硬算的，加了差异块后会写到同一批行上互相覆盖
	tzRow := ovRow
	monthStart := fmt.Sprintf("%04d-%02d-01", year, mon)
	monthEnd := fmt.Sprintf("%04d-%02d-%02d", year, mon, lastDay)

	tzHeadCell, _ := excelize.CoordinatesToCellName(colName, tzRow)
	f.SetCellValue(sheet, tzHeadCell, "时区 / Time zone")
	f.SetCellStyle(sheet, tzHeadCell, tzHeadCell, styles.legendHead)

	noteCell, _ := excelize.CoordinatesToCellName(colName, tzRow+1)
	f.SetCellValue(sheet, noteCell, "上表时间均为员工本地时间 / Times are in each employee's local time")
	f.SetCellStyle(sheet, noteCell, noteCell, styles.legendCell)

	tzLine := tzRow + 2
	for _, emp := range employees {
		atStart := resolveTimezoneAt(emp.Timezones, monthStart)
		atEnd := resolveTimezoneAt(emp.Timezones, monthEnd)
		desc := atStart
		if atStart != atEnd {
			// 本月内换过时区，两段都写出来，否则看表的人会以为整月都是同一个时区
			desc = fmt.Sprintf("%s → %s（本月内变更）", atStart, atEnd)
		}
		empCell, _ := excelize.CoordinatesToCellName(colName, tzLine)
		tzCell, _ := excelize.CoordinatesToCellName(colName+1, tzLine)
		f.SetCellValue(sheet, empCell, emp.Name)
		f.SetCellValue(sheet, tzCell, desc)
		f.SetCellStyle(sheet, empCell, tzCell, styles.legendCell)
		tzLine++
	}

	// ===== 版式：列宽 / 冻结 / 打印 =====
	f.SetColWidth(sheet, "A", "A", 14)
	f.SetColWidth(sheet, "B", "B", 16)
	lastCol, _ := excelize.ColumnNumberToName(colDay1 + lastDay - 1)
	f.SetColWidth(sheet, "C", lastCol, 5)
	f.SetRowHeight(sheet, 1, 16)
	f.SetRowHeight(sheet, 2, 16)

	// 冻结姓名/职位两列 + 表头两行，31 天横向滚动时人名不跑掉
	if err := f.SetPanes(sheet, &excelize.Panes{
		Freeze:      true,
		XSplit:      2,
		YSplit:      2,
		TopLeftCell: "C3",
		ActivePane:  "bottomRight",
		Selection: []excelize.Selection{
			{SQRef: "C3", ActiveCell: "C3", Pane: "bottomRight"},
		},
	}); err != nil {
		log.Printf("[排班导出] WARN 设置冻结窗格失败: %v", err)
	}

	landscape := "landscape"
	fitToWidth, fitToHeight := 1, 0
	if err := f.SetPageLayout(sheet, &excelize.PageLayoutOptions{Orientation: &landscape}); err != nil {
		log.Printf("[排班导出] WARN 设置页面方向失败: %v", err)
	}
	if err := f.SetSheetProps(sheet, &excelize.SheetPropsOptions{
		FitToPage: &[]bool{true}[0],
	}); err != nil {
		log.Printf("[排班导出] WARN 设置缩放适配失败: %v", err)
	}
	if err := f.SetPageLayout(sheet, &excelize.PageLayoutOptions{
		FitToWidth:  &fitToWidth,
		FitToHeight: &fitToHeight,
	}); err != nil {
		log.Printf("[排班导出] WARN 设置打印缩放失败: %v", err)
	}

	log.Printf("[排班导出] sheet=%s 员工=%d 排班格=%d 图例=%d 行数=%d", sheet, len(employees), shiftCount, len(configs), row-3)
	return nil
}

// employeeRoleEn 取员工的英文职位，历史数据没填的按中文职位兜底
func employeeRoleEn(emp ScheduleEmployee) string {
	if strings.TrimSpace(emp.RoleEn) != "" {
		return emp.RoleEn
	}
	fallback := defaultRoleEn(emp.Role)
	log.Printf("[排班导出] WARN 员工 %s 未设置英文职位，按中文职位 %q 兜底为 %q", emp.Name, emp.Role, fallback)
	return fallback
}

// legendTimeEn 图例「时间」列：优先英文说明，没填就退回中文时间段
func legendTimeEn(cfg ShiftConfig) string {
	if strings.TrimSpace(cfg.TimeEn) != "" {
		return cfg.TimeEn
	}
	log.Printf("[排班导出] WARN 班次 %s 未设置英文说明，回退到时间段 %q", cfg.Code, cfg.Time)
	return cfg.Time
}
