package handlers

import (
	"database/sql"
	"strings"

	"github.com/gin-gonic/gin"
)

// 🔴 cis 表 (type, name) 上**没有**唯一索引（见 002_cmdb_core.sql）。
//
// 所以"同名 CI 不能重复"这条约束目前只由 handler 保证。数据库不兜底，
// 意味着任何一处 INSERT INTO cis 漏了查重，就能造出两条同名 CI ——
// 而重复 CI 不会报错、不会变空，只会让所有按 name 关联的逻辑
// （成本归属、指标匹配、拓扑连边）随机命中其中一条。
// 实测：POST /api/cis 和 POST /api/domains 都能造出重复（本轮验收发现）。
//
// 🔴🔴 不要"顺手把那个漏掉的唯一索引补上" —— 唯一性是**按 CI 类型**的，不是全表的：
//
//	host / domain  同名即重复，必须挡；
//	certificate    同一个 CN **天然会有多张**（续期轮换期间新旧并存），挡了就把续期搞坏。
//
//	MySQL 没有部分唯一索引（PostgreSQL 的 WHERE 子句在这里用不了），
//	所以 `ALTER TABLE cis ADD UNIQUE (type, name)` 是错的，
//	它会在证书续期那天以"重复"为由拒绝写入 —— 而那天没人会想到是这个索引。
//	handler 层查重不是权宜之计，是这张表结构下**唯一**可行的做法。
//
//	新增任何 INSERT INTO cis 的地方，都要想清楚这个类型允不允许重名：
//	允许就跳过（像 certs.go），不允许就先调 ciExists。

// ciExists 判断同类型同名的 CI 是否已存在。
//
// q 可以是 *sql.DB 或 *sql.Tx —— 事务里创建时必须传 tx，
// 否则查的是事务外的快照，并发下两个请求会同时认为"不存在"。
func ciExists(q interface {
	QueryRow(string, ...any) *sql.Row
}, ciType, name string) (int64, bool) {
	var id int64
	err := q.QueryRow(`SELECT id FROM cis WHERE type = ? AND name = ? LIMIT 1`, ciType, name).Scan(&id)
	if err != nil {
		return 0, false
	}
	return id, true
}

// failDuplicate 回 409。
//
// ⚠️ 不能用 500：重名是**用户输错了**，改个名字就能继续。
//
//	报成"服务端出错了"会让人去找运维，而运维查日志会发现什么事都没有。
//	也不能把原始的 MySQL 错误直出（"Duplicate entry 'x' for key 'environments.code'"
//	泄露库表结构，且对用户毫无意义）。
func failDuplicate(c *gin.Context, what, name string, existingID int64) {
	// ⚠️ 必须发**结构化**错误体（有 code 才符合前端的 isApiErrorBody）。
	//	只发 {"error": "…"} 的话前端会落到"按状态码兜底"那一档，
	//	把这句具体文案丢掉、只显示通用的"资源冲突" ——
	//	用户就不知道是哪一个重名了（实测过，见 packages/api/src/errors.ts 的分支 3）。
	//	error 字段保留，给直接调 API 的人和旧前端看。
	body := gin.H{
		"code":        "duplicate_name",
		"message_key": "error.duplicateName",
		"params":      gin.H{"what": what, "name": name},
		"error":       what + "「" + name + "」已存在",
	}
	// ⚠️ 0 不是"没有"，前端拿到 existing_id:0 会当成一条真实记录去跳转。
	//	不知道 id 时就别发这个字段（三态里的 null，不是 0）。
	if existingID > 0 {
		body["existing_id"] = existingID
	}
	c.JSON(409, body)
}

// isDupKeyErr 识别 MySQL 的唯一键冲突（错误码 1062）。
//
// 给**有**唯一索引的表用（environments / users / local_roles 等）：
// 那些表不需要先查一遍，让数据库判就行，但错误要翻译过来。
func isDupKeyErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "Error 1062")
}
