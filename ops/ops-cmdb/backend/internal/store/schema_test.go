package store

import (
	"database/sql"
	"os"
	"sort"
	"testing"

	_ "github.com/go-sql-driver/mysql"
)

// 这组测试需要一个真实数据库。设 TEST_MYSQL_DSN 后运行：
//
//	TEST_MYSQL_DSN='user:pass@tcp(127.0.0.1:3306)/dbname' go test ./internal/store/...
//
// 没设就跳过 —— 不引入 sqlite/mock 依赖（那会让测试通过而真实 schema 仍然错）。
// CI 里必须设这个变量，否则本组检查形同虚设。
func openTestDB(t *testing.T) (*sql.DB, string) {
	t.Helper()
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("未设 TEST_MYSQL_DSN，跳过 schema 一致性检查")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("连接失败: %v", err)
	}
	var schema string
	if err := db.QueryRow("SELECT DATABASE()").Scan(&schema); err != nil {
		t.Fatalf("取库名失败: %v", err)
	}
	return db, schema
}

// ★ 每一张非平台级表都必须有 tenant_id。
//
// 这条测试存在的理由：第一版迁移用 grep 从迁移文件里提表名，
// 字符类写成 [a-z_]+ 漏了数字，把 25 张 k8s_* 表全截断成 "k"，
// 而那个 "k" 又被当成解析噪声过滤掉了 —— 于是 25 张表静默地没有租户列。
//
// 表清单必须来自 information_schema，不能来自对迁移文件的文本猜测。
func TestEveryTenantTableHasTenantID(t *testing.T) {
	db, schema := openTestDB(t)
	defer db.Close()

	rows, err := db.Query(`
		SELECT t.table_name
		  FROM information_schema.tables t
		 WHERE t.table_schema = ? AND t.table_type = 'BASE TABLE'
		   AND NOT EXISTS (
		       SELECT 1 FROM information_schema.columns c
		        WHERE c.table_schema = t.table_schema
		          AND c.table_name = t.table_name
		          AND c.column_name = 'tenant_id')
		 ORDER BY t.table_name`, schema)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	defer rows.Close()

	var missing []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		if !IsPlatformTable(name) {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Errorf("下列表缺 tenant_id 且不在平台白名单里：%v\n"+
			"要么补迁移加列，要么在 store/whitelist.go 里登记为平台级（需评审）", missing)
	}
}

// ★ 反向检查：白名单里的表**不该**有 tenant_id。
//
// 有的话说明认知不一致 —— 要么白名单加错了，要么迁移多加了列。
// 这类不一致会让人对隔离边界产生错误理解，比缺列更难查。
func TestPlatformTablesHaveNoTenantID(t *testing.T) {
	db, schema := openTestDB(t)
	defer db.Close()

	for _, name := range PlatformTableNames() {
		var n int
		err := db.QueryRow(`
			SELECT COUNT(*) FROM information_schema.columns
			 WHERE table_schema = ? AND table_name = ? AND column_name = 'tenant_id'`,
			schema, name).Scan(&n)
		if err != nil {
			t.Fatalf("查 %s 失败: %v", name, err)
		}
		// user_tenants 是例外：它的 tenant_id 是关联键，不是隔离列
		if n > 0 && name != "user_tenants" {
			t.Errorf("平台级表 %q 却有 tenant_id 列 —— 白名单与 schema 认知不一致", name)
		}
	}
}

// ★ tenant_id 必须建索引，且是复合索引的第一列。
//
// 所有查询都会带 tenant_id，它不在第一列等于没有索引 ——
// 表一大就是全表扫描，而这类性能问题只在客户数据量上来后才暴露。
func TestTenantIDIsIndexedFirst(t *testing.T) {
	db, schema := openTestDB(t)
	defer db.Close()

	rows, err := db.Query(`
		SELECT c.table_name
		  FROM information_schema.columns c
		 WHERE c.table_schema = ? AND c.column_name = 'tenant_id'
		   AND NOT EXISTS (
		       SELECT 1 FROM information_schema.statistics s
		        WHERE s.table_schema = c.table_schema
		          AND s.table_name = c.table_name
		          AND s.column_name = 'tenant_id'
		          AND s.seq_in_index = 1)
		 ORDER BY c.table_name`, schema)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	defer rows.Close()

	var bad []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		bad = append(bad, name)
	}
	if len(bad) > 0 {
		t.Errorf("下列表的 tenant_id 不是任何索引的第一列：%v\n"+
			"所有查询都带 tenant_id，它不在首列等于没索引", bad)
	}
}

// 默认租户必须存在且 id=1 —— 存量数据全归它。
func TestDefaultTenantExists(t *testing.T) {
	db, _ := openTestDB(t)
	defer db.Close()

	var id int64
	var status string
	err := db.QueryRow(`SELECT id, status FROM tenants WHERE id = 1`).Scan(&id, &status)
	if err != nil {
		t.Fatalf("默认租户不存在: %v", err)
	}
	if status != "active" {
		t.Errorf("默认租户状态 = %q, want active", status)
	}
}

// ★ 后台任务用到的固定查询必须能在真库上跑通。
//
// 这条测试来自一个真实故障：两处后台循环都把租户列表查询写成
// `WHERE enabled=1`，而 tenants 表根本没有 enabled 列。
// 后果是定时任务自愈与成本快照**对所有租户都不执行**，
// 而现象只有一行日志 —— 功能就那么静静地不工作。
//
// 这类错编译器抓不到（SQL 是字符串），没有真库的单测也抓不到。
// 凡是写死在代码里的 SQL 常量，都该在这里过一遍真库。
func TestFixedQueriesRunAgainstRealSchema(t *testing.T) {
	db, _ := openTestDB(t)
	defer db.Close()
	for name, q := range map[string]string{
		"ActiveTenantsQuery":   ActiveTenantsQuery,
		"SeedAdminTenantQuery": SeedAdminTenantQuery,
	} {
		t.Run(name, func(t *testing.T) {
			// 在事务里跑再回滚：写类语句（INSERT/UPDATE）也能验，
			// 但不会往库里留数据。直接 Query 一条 INSERT 会**真的写进去**，
			// 让测试自己污染了它要检查的库。
			tx, err := db.Begin()
			if err != nil {
				t.Fatalf("开事务: %v", err)
			}
			defer tx.Rollback() //nolint:errcheck // 本来就是要回滚
			if _, err := tx.Exec(q); err != nil {
				t.Fatalf("%s 在真库上执行失败：%v\nSQL: %s", name, err, q)
			}
		})
	}
}
