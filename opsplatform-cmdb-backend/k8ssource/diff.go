package k8ssource

import (
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"opsplatform-cmdb-backend/logx"
)

// 增量同步：只把真正变化的行写进库，替代 replaceAll 的「全删全插」。
//
// 为什么要有这个文件：
// replaceAll 每轮 DELETE 整个集群的行再全量 INSERT 回去。row 格式 binlog 下，
// DELETE 记每行前镜像、INSERT 记每行后镜像，一轮的 binlog 写入量 = 2×全表数据量，
// 哪怕一个字节都没变。5 个集群约 1.3 万行/轮 × 720 轮/天 → 4.2 GB/天 binlog，
// 2 天撑爆 10Gi 盘，MySQL 挂在「等磁盘空间写 binlog」上不放 LOCK_log，
// 后端连接池被查询占满，整个 CMDB 对外表现为 Cloudflare 524（2026-07-31 生产故障）。
//
// syncDiff 先把本集群现有行读进内存（纯读，不产生任何 binlog），按业务键比对：
// 新出现的 INSERT、字段变了的按主键 UPDATE、已消失的按主键 DELETE。
// 稳态下 K8s 对象绝大多数轮次毫无变化，三组皆空 → 本轮零写入、零 binlog。
//
// 为什么不走「加 row_hash 列 + 唯一键」那条路：
// 17 张表里有 7 张压根没有唯一约束（endpoints/configmaps/secrets/virtualservices/
// pod_volumes/pod_config_refs/pod_security）——因为设计成全删全插时本就不需要。
// 其中 k8s_pod_config_refs 的业务键含 4 个 VARCHAR(253)，utf8mb4 下超过 InnoDB
// 3072 字节索引上限，唯一键根本加不上。改用主键 id 定位，零 schema 变更、可随时回退。

// syncWriteMode 控制写入策略：diff（默认，增量写）| replace（全删全插，回退用）。
// 由环境变量 K8S_SYNC_MODE 设置——生产上万一发现增量写有问题，改个环境变量
// 重启后端就回到旧行为，不必回滚镜像。
var syncWriteMode = resolveSyncMode()

func resolveSyncMode() string {
	m := strings.ToLower(strings.TrimSpace(os.Getenv("K8S_SYNC_MODE")))
	switch m {
	case "", "diff":
		return "diff"
	case "replace":
		logx.J("k8s_sync", "mode_replace", map[string]any{
			"warn": "已回退为全删全插模式，binlog 写入量约为增量模式的数十倍，仅供临时排障",
		})
		return "replace"
	default:
		logx.J("k8s_sync", "unknown_mode", map[string]any{
			"got": m, "fallback": "diff", "hint": "K8S_SYNC_MODE 只认 diff | replace",
		})
		return "diff"
	}
}

// writeRows 是所有 k8s_* 表的统一写入入口，按 syncWriteMode 分发。
//
// 注意 syncDiff 是「全量比对 + 增量写」而非「只追踪变化」：每一轮都把库里现存行
// 完整读出来和采集结果逐字段比对，所以它天然不会漂移——即使某一轮因故写漏了，
// 下一轮比对就会把差异补回去。不需要额外的周期性全量校验兜底。
// keyCols 为该表的业务键列名；一个都不传表示「整行即为键」，用于
// endpoints/pod_volumes/pod_config_refs 这类关系表——一行就是一条事实，
// 没有「字段被更新」的语义，只有存在与不存在。
func writeRows(db *sql.DB, table string, cols []string, cid int, rows [][]any, keyCols ...string) (int, error) {
	if syncWriteMode == "replace" {
		return replaceAll(db, table, cols, cid, rows)
	}
	n, st, err := syncDiff(db, table, cols, keyCols, cid, rows)
	if err != nil {
		return 0, err
	}
	// 稳态下三个数都是 0，此时不打日志以免刷屏；一有写入就记，便于回答
	// 「这一轮到底改了什么」而不用去查库。
	if !st.zero() {
		logx.J("k8s_sync", "diff_write", map[string]any{
			"table": table, "cluster_id": cid, "rows": n,
			"ins": st.Ins, "upd": st.Upd, "del": st.Del,
		})
	}
	return n, nil
}

// diffStat 一轮增量同步的写入明细。稳态下三个数都应该是 0——
// 日志里一直有非零值，说明要么集群真在变，要么比对逻辑把没变的行误判了。
type diffStat struct{ Ins, Upd, Del int }

func (s diffStat) zero() bool { return s.Ins == 0 && s.Upd == 0 && s.Del == 0 }

func (s diffStat) String() string {
	return fmt.Sprintf("ins=%d upd=%d del=%d", s.Ins, s.Upd, s.Del)
}

// normVal 把「准备写进库的 Go 值」和「刚从库里读回来的值」压成同一种字符串表示。
//
// 这是整个增量同步最容易出错的地方：driver 读回来的整数是 int64、字符串是 []byte、
// DATETIME 是 time.Time(Local)，而写入侧给的是 int32/string/time.Time(UTC)。
// 不做归一就会每行都判「变了」，增量退化成每轮全量 UPDATE——比全删全插还糟，
// 因为 UPDATE 的 binlog 同样要记前后镜像，却还多花了一次全表读。
func normVal(v any) string {
	switch x := v.(type) {
	case nil:
		return "\x00NULL" // 和空字符串区分开，NULL != ''
	case []byte:
		return string(x)
	case string:
		return x
	case time.Time:
		// DATETIME 不存亚秒且 MySQL 是四舍五入而非截断：10:00:00.7 会被存成 10:00:01，
		// 下一轮比对必然不等，该行就会每轮都被 UPDATE。写入前已统一 Truncate 到秒
		// (见 truncTimes)，这里再按 UTC 格式化，消除 loc=Local 往返带来的表示差异。
		return x.UTC().Format("2006-01-02 15:04:05")
	case bool:
		if x {
			return "1"
		}
		return "0"
	case int:
		return strconv.FormatInt(int64(x), 10)
	case int8:
		return strconv.FormatInt(int64(x), 10)
	case int16:
		return strconv.FormatInt(int64(x), 10)
	case int32:
		return strconv.FormatInt(int64(x), 10)
	case int64:
		return strconv.FormatInt(x, 10)
	case uint:
		return strconv.FormatUint(uint64(x), 10)
	case uint32:
		return strconv.FormatUint(uint64(x), 10)
	case uint64:
		return strconv.FormatUint(x, 10)
	case float32:
		return strconv.FormatFloat(float64(x), 'f', -1, 32)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	default:
		return fmt.Sprintf("%v", x)
	}
}

// truncTimes 把行里所有 time.Time 截到整秒后写回。
// 必须在写入前做：MySQL DATETIME(0) 存储时四舍五入，不截断的话
// 写进去的值和下一轮采集到的值永远对不上，该行会被无限 UPDATE。
func truncTimes(rows [][]any) {
	for _, r := range rows {
		for i, v := range r {
			if t, ok := v.(time.Time); ok {
				r[i] = t.Truncate(time.Second)
			}
		}
	}
}

// rowKey 按 keyIdx 指定的列拼出业务键。keyIdx 为空表示整行即为键
// （关系表如 pod_volumes/pod_config_refs：一行就是一条事实，没有「字段更新」语义，
// 只有存在与不存在，所以整行参与比对，差异表现为一删一增而非 UPDATE）。
func rowKey(row []any, keyIdx []int) string {
	var b strings.Builder
	if len(keyIdx) == 0 {
		for i, v := range row {
			if i > 0 {
				b.WriteByte(0x1f) // 单元分隔符，避免 "a|b" 和 "a" + "|b" 撞键
			}
			b.WriteString(normVal(v))
		}
		return b.String()
	}
	for i, ki := range keyIdx {
		if i > 0 {
			b.WriteByte(0x1f)
		}
		b.WriteString(normVal(row[ki]))
	}
	return b.String()
}

// colIdx 把列名解析成下标，供调用方用列名声明业务键。
func colIdx(cols []string, names []string) ([]int, error) {
	idx := make([]int, 0, len(names))
	for _, n := range names {
		pos := -1
		for i, c := range cols {
			if c == n {
				pos = i
				break
			}
		}
		if pos < 0 {
			return nil, fmt.Errorf("业务键列 %q 不在列清单里", n)
		}
		idx = append(idx, pos)
	}
	return idx, nil
}

// existRow 库里已有的一行：主键 id 用来精准定位，vals 用来判断字段有没有变。
type existRow struct {
	id   int64
	vals []string
}

// syncDiff 增量写入本集群数据，返回本轮行数和写入明细。
//
// keyCols 为该表的业务键列名（必须是 cols 的子集）；传 nil 表示整行为键。
// 事务语义与 replaceAll 保持一致：整轮原子提交，读方靠 InnoDB MVCC 只会看到
// 完整的上一轮或完整的这一轮，不存在写了一半的中间态。
func syncDiff(db *sql.DB, table string, cols []string, keyCols []string, cid int, rows [][]any) (int, diffStat, error) {
	var st diffStat

	for i, r := range rows {
		if len(r) != len(cols) {
			return 0, st, fmt.Errorf("%s 第 %d 行给了 %d 个值，期望 %d 个", table, i, len(r), len(cols))
		}
	}

	keyIdx, err := colIdx(cols, keyCols)
	if err != nil {
		return 0, st, fmt.Errorf("%s: %w", table, err)
	}

	truncTimes(rows)

	tx, err := db.Begin()
	if err != nil {
		return 0, st, err
	}
	defer tx.Rollback() // Commit 之后再 Rollback 是空操作

	// ---- 1. 读现存行（纯读，不产生 binlog）----
	colSQL := strings.Join(cols, ",")
	q := "SELECT id," + colSQL + " FROM " + table + " WHERE cluster_id=?"
	dbRows, err := tx.Query(q, cid)
	if err != nil {
		return 0, st, fmt.Errorf("读取 %s 现存行: %w", table, err)
	}
	// 同一业务键可能对应多行（历史全删全插模式下没有唯一约束，脏数据有可能存在）：
	// 第一行参与比对，多出来的一律删掉，顺带把历史重复数据收敛掉。
	exist := make(map[string][]existRow, len(rows)+16)
	scan := make([]any, len(cols)+1)
	holder := make([]any, len(cols)+1)
	for i := range scan {
		scan[i] = &holder[i]
	}
	for dbRows.Next() {
		if err := dbRows.Scan(scan...); err != nil {
			dbRows.Close()
			return 0, st, fmt.Errorf("扫描 %s: %w", table, err)
		}
		var id int64
		switch v := holder[0].(type) {
		case int64:
			id = v
		case []byte:
			id, _ = strconv.ParseInt(string(v), 10, 64)
		default:
			id, _ = strconv.ParseInt(normVal(v), 10, 64)
		}
		vals := make([]string, len(cols))
		raw := make([]any, len(cols))
		for i := 0; i < len(cols); i++ {
			vals[i] = normVal(holder[i+1])
			raw[i] = holder[i+1]
		}
		k := rowKeyStr(vals, keyIdx)
		exist[k] = append(exist[k], existRow{id: id, vals: vals})
	}
	if err := dbRows.Err(); err != nil {
		dbRows.Close()
		return 0, st, fmt.Errorf("遍历 %s: %w", table, err)
	}
	dbRows.Close()

	// ---- 2. 比对 ----
	var (
		toInsert [][]any
		delIDs   []int64
	)
	seen := make(map[string]bool, len(rows))

	for _, r := range rows {
		k := rowKey(r, keyIdx)
		if seen[k] {
			// 本轮采集里出现重复键（理论上不该有），跳过以免同一行被写两次
			continue
		}
		seen[k] = true

		got, ok := exist[k]
		if !ok {
			toInsert = append(toInsert, r)
			continue
		}
		// 同键多行：第一行留下比对，其余全删
		for _, extra := range got[1:] {
			delIDs = append(delIDs, extra.id)
		}
		cur := got[0]
		changed := make([]int, 0, 4)
		for i := range cols {
			if normVal(r[i]) != cur.vals[i] {
				changed = append(changed, i)
			}
		}
		if len(changed) > 0 {
			if err := updateRow(tx, table, cols, changed, r, cur.id); err != nil {
				return 0, st, err
			}
			st.Upd++
		}
	}
	// 库里有、本轮没采到 → 对象已消失
	for k, list := range exist {
		if seen[k] {
			continue
		}
		for _, e := range list {
			delIDs = append(delIDs, e.id)
		}
	}

	// ---- 3. 写入 ----
	if len(delIDs) > 0 {
		if err := deleteByIDs(tx, table, delIDs); err != nil {
			return 0, st, err
		}
		st.Del = len(delIDs)
	}
	if len(toInsert) > 0 {
		if err := insertRows(tx, table, cols, toInsert); err != nil {
			return 0, st, err
		}
		st.Ins = len(toInsert)
	}

	if err := tx.Commit(); err != nil {
		return 0, st, fmt.Errorf("提交 %s: %w", table, err)
	}
	return len(rows), st, nil
}

// rowKeyStr 与 rowKey 同义，但输入已是归一化字符串（读库路径用）。
func rowKeyStr(vals []string, keyIdx []int) string {
	var b strings.Builder
	if len(keyIdx) == 0 {
		for i, v := range vals {
			if i > 0 {
				b.WriteByte(0x1f)
			}
			b.WriteString(v)
		}
		return b.String()
	}
	for i, ki := range keyIdx {
		if i > 0 {
			b.WriteByte(0x1f)
		}
		b.WriteString(vals[ki])
	}
	return b.String()
}

// updateRow 只更新真正变化的列，按主键定位。
func updateRow(tx *sql.Tx, table string, cols []string, changed []int, row []any, id int64) error {
	sets := make([]string, 0, len(changed))
	args := make([]any, 0, len(changed)+1)
	for _, i := range changed {
		sets = append(sets, cols[i]+"=?")
		args = append(args, row[i])
	}
	args = append(args, id)
	q := "UPDATE " + table + " SET " + strings.Join(sets, ",") + " WHERE id=?"
	if _, err := tx.Exec(q, args...); err != nil {
		return fmt.Errorf("更新 %s id=%d: %w", table, id, err)
	}
	return nil
}

// deleteByIDs 按主键批量删除。
func deleteByIDs(tx *sql.Tx, table string, ids []int64) error {
	for start := 0; start < len(ids); start += replaceBatchSize {
		end := start + replaceBatchSize
		if end > len(ids) {
			end = len(ids)
		}
		batch := ids[start:end]
		args := make([]any, len(batch))
		for i, id := range batch {
			args[i] = id
		}
		q := "DELETE FROM " + table + " WHERE id IN (" +
			strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",") + ")"
		if _, err := tx.Exec(q, args...); err != nil {
			return fmt.Errorf("删除 %s: %w", table, err)
		}
	}
	return nil
}

// insertRows 批量插入，批大小与 replaceAll 保持一致。
func insertRows(tx *sql.Tx, table string, cols []string, rows [][]any) error {
	colSQL := strings.Join(cols, ",")
	one := "(" + strings.TrimSuffix(strings.Repeat("?,", len(cols)), ",") + ")"
	for start := 0; start < len(rows); start += replaceBatchSize {
		end := start + replaceBatchSize
		if end > len(rows) {
			end = len(rows)
		}
		batch := rows[start:end]
		args := make([]any, 0, len(batch)*len(cols))
		for _, r := range batch {
			args = append(args, r...)
		}
		q := "INSERT INTO " + table + " (" + colSQL + ") VALUES " +
			strings.TrimSuffix(strings.Repeat(one+",", len(batch)), ",")
		if _, err := tx.Exec(q, args...); err != nil {
			return fmt.Errorf("写入 %s 第 %d~%d 行: %w", table, start, end, err)
		}
	}
	return nil
}
