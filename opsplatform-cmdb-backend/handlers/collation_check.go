package handlers

import (
	"database/sql"
	"sort"
	"strings"

	"opsplatform-cmdb-backend/logx"
)

// 启动时自检：库里的字符串列排序规则是否一致。
//
//	## 为什么值得单独做这件事
//
//	生产上 LB 后端「81/81 全空」修了好几版没修好，真因是这个：
//
//	  Error 1267 (HY000): Illegal mix of collations
//	  (utf8mb4_0900_ai_ci,IMPLICIT) and (utf8mb4_unicode_ci,IMPLICIT) for operation '='
//
//	`cis`(迁移 002) 和 `cloud_lb_backends`(迁移 026) 建表时隔了很远，落到生产库上
//	排序规则不一致，于是 `c.name = b.instance` 这个跨表比较**从来没成功过一次**。
//	而它的失败方式极其隐蔽：
//	  · 建表不报错、迁移不报错、启动不报错
//	  · 本地库两张表恰好一致，本地怎么试都是好的
//	  · 查询错误被 `rows, _ :=` 吞掉，页面上只表现为"这个 LB 没有后端"
//	排查时唯一能看见的现象是一个业务结论（"没有后端"），和真实原因隔了三层。
//
//	所以启动时主动查一次 information_schema：库里出现多于一种排序规则就 WARN 出来。
//	这不修任何东西，但下次再有人写跨表 JOIN 撞上它时，日志里有据可查，
//	不用再从"页面数字不对"倒推三层。
//
//	只读 information_schema，不改任何数据，失败也不影响启动。
func CheckCollations(db *sql.DB) {
	rows, err := db.Query(`
		SELECT COLLATION_NAME, TABLE_NAME, COUNT(*)
		  FROM information_schema.COLUMNS
		 WHERE TABLE_SCHEMA = DATABASE() AND COLLATION_NAME IS NOT NULL
		 GROUP BY COLLATION_NAME, TABLE_NAME`)
	if err != nil {
		logx.J("db", "collation_check_fail", map[string]any{"err": err.Error()})
		return
	}
	defer rows.Close()

	tablesOf := map[string]map[string]bool{} // collation -> 表集合
	for rows.Next() {
		var coll, table string
		var n int
		if rows.Scan(&coll, &table, &n) != nil {
			continue
		}
		if tablesOf[coll] == nil {
			tablesOf[coll] = map[string]bool{}
		}
		tablesOf[coll][table] = true
	}
	if len(tablesOf) <= 1 {
		return // 全库一致，正常情况，不打扰
	}

	// 少数派才是要看的：占表数最少的那些排序规则，通常就是"某几张表建歪了"
	colls := make([]string, 0, len(tablesOf))
	for c := range tablesOf {
		colls = append(colls, c)
	}
	sort.Slice(colls, func(i, j int) bool { return len(tablesOf[colls[i]]) > len(tablesOf[colls[j]]) })

	detail := map[string]any{
		"note": "库内排序规则不统一。跨这两组表做字符串等值比较（JOIN/WHERE a.name=b.name）会抛 Error 1267，" +
			"且失败方式隐蔽——查询报错常被当成'查不到数据'。新写跨表比较前先对照这份清单。",
	}
	for i, c := range colls {
		names := make([]string, 0, len(tablesOf[c]))
		for t := range tablesOf[c] {
			names = append(names, t)
		}
		sort.Strings(names)
		key := "majority"
		if i > 0 {
			key = "minority_" + itoa(i)
		}
		// 表可能很多，日志里只留前 20 张 + 总数，够定位是哪一批迁移建的
		shown := names
		if len(shown) > 20 {
			shown = shown[:20]
		}
		detail[key] = map[string]any{
			"collation": c, "table_count": len(names), "tables": strings.Join(shown, ","),
		}
	}
	logx.J("db", "collation_mismatch", detail)
}
