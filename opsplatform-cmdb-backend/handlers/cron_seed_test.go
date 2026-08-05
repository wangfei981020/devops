package handlers

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/robfig/cron/v3"
)

// 把**迁移文件里种下的每一条 cron 表达式**都用调度器实际使用的解析器跑一遍。
//
//	为什么值得单独写这个测试：cron 表达式写错的失败方式极其隐蔽——
//	  · 构建不报错
//	  · 部署不报错
//	  · 启动只打一行日志（没人天天看日志）
//	  · 页面上「下次执行」显示一个 `—`，和"还没到时间"长得一模一样
//	结果就是任务**注册以来一次都没跑过**，而所有人都以为它在跑。
//	registrar_expiry_sync 就是这么躺了好几天：我在迁移 084 里写成了
//	`0 30 3 * * *`（6 段带秒），而调度器用的是 cron.New()，标准 5 段解析器。
//
//	⚠️ 必须用 cron.ParseStandard——和 handlers/scheduler.go 里 cron.New() 的
//	解析器保持一致。用 WithSeconds 的解析器测就测不出这个问题了。
func TestSeededCronExpressionsAreValid(t *testing.T) {
	dir := filepath.Join("..", "database", "migrations")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Skipf("读不到迁移目录：%v", err)
	}

	// 匹配 scheduled_tasks 的 INSERT/UPDATE 里的 cron 字面量。
	// 形如 ...'task_key', '任务名', '0 3 * * *', 1)  或  SET schedule = '30 3 * * *'
	insertRe := regexp.MustCompile(`(?is)INSERT\s+(?:IGNORE\s+)?INTO\s+scheduled_tasks.*?VALUES\s*(.+?);`)
	updateRe := regexp.MustCompile(`(?is)UPDATE\s+scheduled_tasks\s+SET\s+schedule\s*=\s*'([^']+)'`)
	literalRe := regexp.MustCompile(`'([^']*)'`)

	checked := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		sql := string(b)

		for _, m := range updateRe.FindAllStringSubmatch(sql, -1) {
			checkCron(t, e.Name(), m[1])
			checked++
		}

		for _, m := range insertRe.FindAllStringSubmatch(sql, -1) {
			for _, lit := range literalRe.FindAllStringSubmatch(m[1], -1) {
				v := lit[1]
				// cron 表达式的特征：以 @ 开头（@every/@daily），或者含空格且由
				// 数字/*//,- 组成。任务名和 key 不会长这样。
				if looksLikeCron(v) {
					checkCron(t, e.Name(), v)
					checked++
				}
			}
		}
	}
	if checked == 0 {
		t.Skip("迁移里没有找到 cron 字面量（可能格式变了，这个测试需要跟着更新）")
	}
	t.Logf("已校验 %d 条种子 cron 表达式", checked)
}

func looksLikeCron(v string) bool {
	if strings.HasPrefix(v, "@") {
		return true
	}
	fields := strings.Fields(v)
	if len(fields) < 5 {
		return false
	}
	for _, f := range fields {
		if strings.TrimLeft(f, "0123456789*/,-") != "" {
			return false
		}
	}
	return true
}

func checkCron(t *testing.T, file, expr string) {
	t.Helper()
	if _, err := cron.ParseStandard(expr); err != nil {
		n := len(strings.Fields(expr))
		hint := ""
		if n == 6 {
			hint = "（这是 6 段带秒的写法，但调度器用的是标准 5 段解析器——" +
				"去掉最前面的秒字段。任务会静默不注册，页面上只显示一个 `—`）"
		}
		t.Errorf("%s 里的 cron 表达式 %q 无法被调度器解析：%v%s", file, expr, err, hint)
	}
}

// 反向确认：6 段表达式确实会被标准解析器拒绝。
// 这条锁住的是"上面那个测试真的能抓到问题"，不是摆设。
func TestSixFieldCronIsRejectedByStandardParser(t *testing.T) {
	if _, err := cron.ParseStandard("0 30 3 * * *"); err == nil {
		t.Fatal("6 段 cron 竟然被标准解析器接受了——" +
			"若解析器换过，上面的种子校验就形同虚设，需要重新审视")
	}
	if _, err := cron.ParseStandard("30 3 * * *"); err != nil {
		t.Fatalf("5 段 cron 应该被接受：%v", err)
	}
}
