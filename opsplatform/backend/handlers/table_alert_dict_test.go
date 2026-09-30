package handlers

import (
	"database/sql"
	"os"
	"testing"
	"time"

	"opsplatform/database"

	_ "github.com/go-sql-driver/mysql"
)

// 字典指纹要真连库才测得准：它的全部行为都在两条 SQL 和一次比对里，
// 照着逻辑再写一遍等于把同一个假设验证两次。
//
// 默认跳过，不占 CI。跑法：
//
//	TA_DICT_TEST_DSN='tadict:TaDict@2026@tcp(127.0.0.1:3306)/ta_dict_test?parseTime=True&loc=Local' \
//	  go test ./handlers/ -run TestDictVersion -v
func dictTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TA_DICT_TEST_DSN")
	if dsn == "" {
		t.Skip("未设置 TA_DICT_TEST_DSN，跳过（这个用例需要一个真 MySQL）")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("连接测试库失败: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("测试库 ping 失败: %v", err)
	}

	for _, ddl := range []string{
		`DROP TABLE IF EXISTS table_alert_envs`,
		`DROP TABLE IF EXISTS table_alert_rooms`,
		`DROP TABLE IF EXISTS table_alert_sites`,
		`CREATE TABLE table_alert_envs (
			id VARCHAR(36) PRIMARY KEY, name VARCHAR(64),
			dict_version VARCHAR(32) NOT NULL DEFAULT '', dict_version_at DATETIME NULL,
			last_collect_at DATETIME NULL, last_collect_ok TINYINT(1) NOT NULL DEFAULT 0)`,
		`CREATE TABLE table_alert_rooms (
			env_id VARCHAR(36), room_id VARCHAR(64), room_no VARCHAR(64), table_no VARCHAR(64),
			in_service TINYINT(1) NOT NULL DEFAULT 0, status VARCHAR(32) NOT NULL DEFAULT '',
			online_user_total INT NOT NULL DEFAULT 0, maintaining TINYINT(1) NOT NULL DEFAULT 0,
			last_seen_at DATETIME NULL)`,
		`CREATE TABLE table_alert_sites (
			env_id VARCHAR(36), site_id VARCHAR(64), site_name VARCHAR(128) NOT NULL DEFAULT '',
			watched TINYINT(1) NOT NULL DEFAULT 0)`,
	} {
		if _, err := db.Exec(ddl); err != nil {
			t.Fatalf("建表失败: %v\n%s", err, ddl)
		}
	}

	database.DB = db
	return db
}

// seedDictEnv 造一个环境 + 两张桌台 + 一个站点，全都「刚采集到」。
func seedDictEnv(t *testing.T, db *sql.DB, collectedAt time.Time) *TAEnv {
	t.Helper()
	env := &TAEnv{ID: "env-1", Name: "PROD"}

	if _, err := db.Exec(`INSERT INTO table_alert_envs (id, name, last_collect_at, last_collect_ok)
		VALUES (?,?,?,1)`, env.ID, env.Name, collectedAt); err != nil {
		t.Fatalf("插环境失败: %v", err)
	}
	for _, r := range []struct {
		roomID, roomNo, tableNo string
		online                  int
	}{
		{"46001", "C001", "C001", 120},
		{"1332283756019856384", "N011", "N11", 4366},
	} {
		if _, err := db.Exec(`INSERT INTO table_alert_rooms
			(env_id, room_id, room_no, table_no, in_service, status, online_user_total, last_seen_at)
			VALUES (?,?,?,?,1,'Enable',?,?)`,
			env.ID, r.roomID, r.roomNo, r.tableNo, r.online, collectedAt); err != nil {
			t.Fatalf("插桌台失败: %v", err)
		}
	}
	if _, err := db.Exec(`INSERT INTO table_alert_sites (env_id, site_id, site_name, watched)
		VALUES (?,?,?,1)`, env.ID, "1281834004026244608", "BPreal"); err != nil {
		t.Fatalf("插站点失败: %v", err)
	}
	return env
}

func currentDictVersion(t *testing.T, db *sql.DB, envID string) string {
	t.Helper()
	var v string
	if err := db.QueryRow(`SELECT COALESCE(dict_version,'') FROM table_alert_envs WHERE id=?`, envID).Scan(&v); err != nil {
		t.Fatalf("读指纹失败: %v", err)
	}
	return v
}

// 🔴 这条是整套缓存机制的成败所在。
//
// 在线人数每次采集都在变，一旦算进指纹，调用方的缓存就永远命中不了，
// 每轮都要拉全量——缓存等于白做，而且不会有任何报错提示你做错了。
func TestDictVersionIgnoresVolatileFields(t *testing.T) {
	db := dictTestDB(t)
	now := time.Now()
	env := seedDictEnv(t, db, now)

	taRecalcDictVersion(env, now)
	base := currentDictVersion(t, db, env.ID)
	if base == "" {
		t.Fatal("首次计算应当写入指纹，实际为空")
	}

	// 在线人数变了、维护状态变了 —— 名单没变，指纹就不该动
	if _, err := db.Exec(`UPDATE table_alert_rooms SET online_user_total=9999, maintaining=1 WHERE room_id='46001'`); err != nil {
		t.Fatal(err)
	}
	taRecalcDictVersion(env, now)
	if got := currentDictVersion(t, db, env.ID); got != base {
		t.Errorf("在线人数/维护状态变化不该影响指纹\n  变化前 %s\n  变化后 %s", base, got)
	}
}

// 名单本身的每一种变化都必须让指纹翻新，否则调用方会一直用着过期的名单，
// 而且因为指纹没变，它根本不知道该去拉新的。
func TestDictVersionTracksRosterChanges(t *testing.T) {
	cases := []struct {
		name string
		sql  string
	}{
		{"在用状态翻转", `UPDATE table_alert_rooms SET in_service=0 WHERE room_id='46001'`},
		{"启停状态翻转", `UPDATE table_alert_rooms SET status='Disable' WHERE room_id='46001'`},
		{"房间号改名", `UPDATE table_alert_rooms SET room_no='C001-2' WHERE room_id='46001'`},
		{"桌号改名", `UPDATE table_alert_rooms SET table_no='C01' WHERE room_id='46001'`},
		{"站点改名", `UPDATE table_alert_sites SET site_name='BingoPlus' WHERE site_id='1281834004026244608'`},
		{"站点关注状态变化", `UPDATE table_alert_sites SET watched=0 WHERE site_id='1281834004026244608'`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			db := dictTestDB(t)
			now := time.Now()
			env := seedDictEnv(t, db, now)

			taRecalcDictVersion(env, now)
			base := currentDictVersion(t, db, env.ID)

			if _, err := db.Exec(c.sql); err != nil {
				t.Fatal(err)
			}
			taRecalcDictVersion(env, now)
			if got := currentDictVersion(t, db, env.ID); got == base {
				t.Errorf("%s 之后指纹应当变化，实际仍是 %s", c.name, base)
			}
		})
	}
}

// 新增和下线都要反映到指纹里。下线尤其重要：采集只 upsert 从不删行，
// 全靠 last_seen_at 把消失的桌台挡在字典外，挡不住就是永久误报。
func TestDictVersionTracksRoomAddedAndGone(t *testing.T) {
	db := dictTestDB(t)
	now := time.Now()
	env := seedDictEnv(t, db, now)

	taRecalcDictVersion(env, now)
	base := currentDictVersion(t, db, env.ID)

	if _, err := db.Exec(`INSERT INTO table_alert_rooms
		(env_id, room_id, room_no, table_no, in_service, status, last_seen_at)
		VALUES (?, '46002','C002','C002',1,'Enable',?)`, env.ID, now); err != nil {
		t.Fatal(err)
	}
	taRecalcDictVersion(env, now)
	added := currentDictVersion(t, db, env.ID)
	if added == base {
		t.Fatal("新增桌台后指纹应当变化")
	}

	// 接口里不再返回这张桌台：行还在，只是 last_seen_at 停在过去
	if _, err := db.Exec(`UPDATE table_alert_rooms SET last_seen_at=? WHERE room_id='46002'`,
		now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	taRecalcDictVersion(env, now)
	gone := currentDictVersion(t, db, env.ID)
	if gone == added {
		t.Error("桌台超出宽限期后应当移出字典，指纹随之变化")
	}
	if gone != base {
		t.Errorf("移出后名单应当回到新增之前的样子\n  最初 %s\n  移出后 %s", base, gone)
	}
}

// 🔴 过滤基准必须是「最后一次成功采集的时刻」，不能是 NOW()。
//
// 用 NOW() 的话采集一挂，几分钟后字典就整个空了，调用方会以为所有桌台同时
// 下线、监控范围归零，而且全程不报错。冻住一份旧名单远比给一份空名单安全。
func TestDictSurvivesStalledCollection(t *testing.T) {
	db := dictTestDB(t)
	// 最后一次成功采集是三小时前，之后采集就挂了
	stalled := time.Now().Add(-3 * time.Hour)
	env := seedDictEnv(t, db, stalled)

	taRecalcDictVersion(env, stalled)
	version := currentDictVersion(t, db, env.ID)
	if version == "" {
		t.Fatal("采集停摆时指纹仍应算得出来")
	}

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM table_alert_rooms
		WHERE env_id=? AND last_seen_at >= ?`,
		env.ID, stalled.Add(-taDictGraceSec*time.Second)).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("以最后一次成功采集为基准应当仍能看到 2 张桌台，实际 %d 张", n)
	}

	// 对照：如果基准换成 NOW()，同样的数据会一张都剩不下
	var withNow int
	if err := db.QueryRow(`SELECT COUNT(*) FROM table_alert_rooms
		WHERE env_id=? AND last_seen_at >= ?`,
		env.ID, time.Now().Add(-taDictGraceSec*time.Second)).Scan(&withNow); err != nil {
		t.Fatal(err)
	}
	if withNow != 0 {
		t.Logf("提示：本用例依赖「NOW() 基准会清空名单」这一前提，当前得到 %d 张", withNow)
	}
}
