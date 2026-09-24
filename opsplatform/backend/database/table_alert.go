package database

import (
	"log"
)

// ============================================================================
// 桌台维护告警（新菜单 table-alert）
//
// 与「桌台维护记录 / 桌台配置 / 桌台管理」三个老菜单完全独立，不共用任何表。
//
// 数据来源：中台 GET /gameRoom/list?curPage=N&pageSize=M
//   返回 {"msg":"Success","data":{"records":[...],"total":140}}
//   实测结论（2026-09-24 在 UAT 验证）：
//     - status              : Enable / Disable   = 桌台启用/关闭，与维护无关
//     - gameRoomMaintainList: null=正常，非空数组=维护中（数组元素 {siteId, source}）
//     - operator/updateTime : 最后操作人 / 操作时间
//   所以「是否维护」判定的是 gameRoomMaintainList，不是 status。
//
// 地址 / token / webhook 一律不写死，建表只给出已验证的解析默认值，
// 敏感信息由使用者在页面上填。
// ============================================================================

// InitTableAlertTables 建表 + 预置空环境模板。由 InitDB 末尾调用。
func InitTableAlertTables() error {
	stmts := []struct {
		name string
		ddl  string
	}{
		// ---------------- 环境配置：一个环境一条，地址各填各的 ----------------
		{"table_alert_envs", `
		CREATE TABLE IF NOT EXISTS table_alert_envs (
			id VARCHAR(36) PRIMARY KEY,
			name VARCHAR(64) NOT NULL COMMENT '环境名，如 UAT / PROD / DEV',
			enabled TINYINT(1) NOT NULL DEFAULT 0 COMMENT '是否参与定时采集',
			sort_order INT NOT NULL DEFAULT 0,

			-- ===== 请求配置（地址不写死，由使用者填）=====
			url TEXT COMMENT '完整请求地址，如 http://10.x.x.x/gameRoom/list',
			method VARCHAR(10) NOT NULL DEFAULT 'GET' COMMENT 'GET / POST',
			host_header VARCHAR(255) NOT NULL DEFAULT '' COMMENT '按 IP 访问网关时必填，否则 Istio 匹配不到路由返回 404',
			request_body TEXT COMMENT 'POST 时的 JSON body 模板',
			extra_headers TEXT COMMENT '额外请求头，JSON 对象 {"K":"V"}',
			token TEXT COMMENT '认证 token，可留空',
			token_place VARCHAR(32) NOT NULL DEFAULT 'none' COMMENT 'none/bearer/raw/token_header/query',
			skip_tls_verify TINYINT(1) NOT NULL DEFAULT 0 COMMENT 'https 且证书过期时打开',
			timeout_sec INT NOT NULL DEFAULT 10,
			cur_page INT NOT NULL DEFAULT 1 COMMENT 'curPage 不传接口会 NPE 返回 9999',
			page_size INT NOT NULL DEFAULT 500,

			-- ===== 响应解析（默认值已在 UAT 实测验证）=====
			data_path VARCHAR(128) NOT NULL DEFAULT 'data.records',
			total_path VARCHAR(128) NOT NULL DEFAULT 'data.total',
			f_room_id VARCHAR(64) NOT NULL DEFAULT 'id',
			f_table_no VARCHAR(64) NOT NULL DEFAULT 'tableNo',
			f_room_no VARCHAR(64) NOT NULL DEFAULT 'roomNo',
			f_platform_id VARCHAR(64) NOT NULL DEFAULT 'gamePlatformId',
			f_status VARCHAR(64) NOT NULL DEFAULT 'status',
			f_maintain VARCHAR(64) NOT NULL DEFAULT 'gameRoomMaintainList',
			f_operator VARCHAR(64) NOT NULL DEFAULT 'operator',
			f_update_time VARCHAR(64) NOT NULL DEFAULT 'updateTime',
			f_online_total VARCHAR(64) NOT NULL DEFAULT 'onlineUserTotal',

			-- ===== 维护判定 =====
			maintain_rule VARCHAR(32) NOT NULL DEFAULT 'list_not_empty' COMMENT 'list_not_empty=维护字段非空即维护中；status_equals=状态字段等于某值',
			maintain_status_value VARCHAR(64) NOT NULL DEFAULT '' COMMENT 'maintain_rule=status_equals 时的目标值',

			-- ===== 采集 =====
			interval_sec INT NOT NULL DEFAULT 60 COMMENT '采集间隔秒；告警间隔不得小于它',
			log_raw_response TINYINT(1) NOT NULL DEFAULT 1 COMMENT '调试期原样记录完整响应体',

			-- ===== 运行态 =====
			last_collect_at DATETIME NULL,
			last_collect_ok TINYINT(1) NOT NULL DEFAULT 0,
			last_collect_error TEXT,
			last_collect_count INT NOT NULL DEFAULT 0,
			last_duration_ms INT NOT NULL DEFAULT 0,
			last_http_status INT NOT NULL DEFAULT 0,

			created_by VARCHAR(64) NOT NULL DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			UNIQUE KEY uk_ta_env_name (name)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`},

		// ---------------- 桌台当前快照：每次采集覆盖 ----------------
		{"table_alert_rooms", `
		CREATE TABLE IF NOT EXISTS table_alert_rooms (
			id VARCHAR(36) PRIMARY KEY,
			env_id VARCHAR(36) NOT NULL,
			room_id VARCHAR(64) NOT NULL COMMENT '接口返回的 id',
			table_no VARCHAR(64) NOT NULL DEFAULT '',
			room_no VARCHAR(64) NOT NULL DEFAULT '',
			platform_id VARCHAR(64) NOT NULL DEFAULT '',
			status VARCHAR(32) NOT NULL DEFAULT '' COMMENT 'Enable / Disable，启停，与维护无关',
			maintaining TINYINT(1) NOT NULL DEFAULT 0 COMMENT '是否维护中',
			maintain_site_count INT NOT NULL DEFAULT 0 COMMENT '维护影响的站点数',
			online_user_total INT NOT NULL DEFAULT 0,
			operator VARCHAR(128) NOT NULL DEFAULT '',
			remote_update_time VARCHAR(64) NOT NULL DEFAULT '' COMMENT '接口给的 updateTime，原样存',
			maintain_since DATETIME NULL COMMENT '本地首次观测到维护中的时刻，告警时长以它为准',
			first_seen_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			last_seen_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			UNIQUE KEY uk_ta_room (env_id, room_id),
			INDEX idx_ta_room_env (env_id),
			INDEX idx_ta_room_maintaining (maintaining),
			INDEX idx_ta_room_table_no (table_no)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`},

		// ---------------- Lark 机器人：支持发多个群 ----------------
		{"table_alert_lark_bots", `
		CREATE TABLE IF NOT EXISTS table_alert_lark_bots (
			id VARCHAR(36) PRIMARY KEY,
			name VARCHAR(64) NOT NULL COMMENT '群名，便于识别',
			webhook VARCHAR(512) NOT NULL DEFAULT '' COMMENT 'Lark 机器人 webhook，由使用者填',
			secret VARCHAR(256) NOT NULL DEFAULT '' COMMENT '签名校验密钥，可空',
			description VARCHAR(500) NOT NULL DEFAULT '',
			enabled TINYINT(1) NOT NULL DEFAULT 1,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			UNIQUE KEY uk_ta_bot_name (name)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`},

		// ---------------- 通知人：Lark 艾特用 ----------------
		{"table_alert_contacts", `
		CREATE TABLE IF NOT EXISTS table_alert_contacts (
			id VARCHAR(36) PRIMARY KEY,
			name VARCHAR(64) NOT NULL,
			lark_id VARCHAR(128) NOT NULL DEFAULT '' COMMENT 'Lark open_id / user_id，由使用者填',
			remark VARCHAR(500) NOT NULL DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			UNIQUE KEY uk_ta_contact_name (name)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`},

		// ---------------- 告警规则：一个环境一条 ----------------
		{"table_alert_rules", `
		CREATE TABLE IF NOT EXISTS table_alert_rules (
			id VARCHAR(36) PRIMARY KEY,
			env_id VARCHAR(36) NOT NULL,
			enabled TINYINT(1) NOT NULL DEFAULT 1,
			threshold_min INT NOT NULL DEFAULT 10 COMMENT '维护超过多少分钟开始告警',
			interval_min INT NOT NULL DEFAULT 10 COMMENT '每隔多少分钟重复告警一次',
			max_times INT NOT NULL DEFAULT 6 COMMENT '最多告警几次',
			escalate TINYINT(1) NOT NULL DEFAULT 1 COMMENT '达到上限后升级还是停止',
			escalate_interval_min INT NOT NULL DEFAULT 30,
			notify_on_recover TINYINT(1) NOT NULL DEFAULT 1 COMMENT '恢复正常时发一条通知',
			at_lark_ids VARCHAR(1000) NOT NULL DEFAULT '' COMMENT '固定艾特人的 lark_id，逗号分隔',
			escalate_at_lark_ids VARCHAR(1000) NOT NULL DEFAULT '' COMMENT '升级时追加艾特',
			reat_every_time TINYINT(1) NOT NULL DEFAULT 1 COMMENT '未确认则每次都重新艾特',
			silence_after_ack_min INT NOT NULL DEFAULT 30 COMMENT '确认后静默分钟数',
			quiet_enabled TINYINT(1) NOT NULL DEFAULT 0,
			quiet_start VARCHAR(8) NOT NULL DEFAULT '03:00',
			quiet_end VARCHAR(8) NOT NULL DEFAULT '08:00',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			UNIQUE KEY uk_ta_rule_env (env_id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`},

		// ---------------- 规则 ↔ 机器人：多对多，一条规则可发多个群 ----------------
		{"table_alert_rule_bots", `
		CREATE TABLE IF NOT EXISTS table_alert_rule_bots (
			rule_id VARCHAR(36) NOT NULL,
			bot_id VARCHAR(36) NOT NULL,
			PRIMARY KEY (rule_id, bot_id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`},

		// ---------------- 告警事件：一次维护一条，贯穿告警→确认→恢复 ----------------
		{"table_alert_events", `
		CREATE TABLE IF NOT EXISTS table_alert_events (
			id VARCHAR(36) PRIMARY KEY,
			env_id VARCHAR(36) NOT NULL,
			env_name VARCHAR(64) NOT NULL DEFAULT '',
			room_id VARCHAR(64) NOT NULL,
			table_no VARCHAR(64) NOT NULL DEFAULT '',
			room_no VARCHAR(64) NOT NULL DEFAULT '',
			platform_id VARCHAR(64) NOT NULL DEFAULT '',
			maintain_start_at DATETIME NOT NULL COMMENT '首次观测到维护中的时刻',
			maintain_end_at DATETIME NULL,
			site_count INT NOT NULL DEFAULT 0,
			operator VARCHAR(128) NOT NULL DEFAULT '',
			alert_count INT NOT NULL DEFAULT 0,
			last_alert_at DATETIME NULL,
			next_alert_at DATETIME NULL,
			escalated TINYINT(1) NOT NULL DEFAULT 0,
			state VARCHAR(20) NOT NULL DEFAULT 'pending' COMMENT 'pending未到阈值/alerting告警中/acked已确认/stopped停止/recovered已恢复',
			acked_by VARCHAR(64) NOT NULL DEFAULT '',
			acked_at DATETIME NULL,
			silence_until DATETIME NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			INDEX idx_ta_ev_env (env_id),
			INDEX idx_ta_ev_state (state),
			INDEX idx_ta_ev_room (env_id, room_id),
			INDEX idx_ta_ev_start (maintain_start_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`},

		// ---------------- 采集日志：调试期存全量响应，稳定后靠开关收敛 ----------------
		{"table_alert_collect_logs", `
		CREATE TABLE IF NOT EXISTS table_alert_collect_logs (
			id VARCHAR(36) PRIMARY KEY,
			env_id VARCHAR(36) NOT NULL,
			env_name VARCHAR(64) NOT NULL DEFAULT '',
			started_at DATETIME NOT NULL,
			duration_ms INT NOT NULL DEFAULT 0,
			http_status INT NOT NULL DEFAULT 0,
			ok TINYINT(1) NOT NULL DEFAULT 0,
			error_msg TEXT,
			request_url TEXT COMMENT '实际请求的 URL（含参数）',
			record_count INT NOT NULL DEFAULT 0,
			total_count INT NOT NULL DEFAULT 0,
			enable_count INT NOT NULL DEFAULT 0,
			disable_count INT NOT NULL DEFAULT 0,
			maintain_count INT NOT NULL DEFAULT 0,
			change_count INT NOT NULL DEFAULT 0,
			changes LONGTEXT COMMENT '状态变化明细 JSON，只比对关心的字段',
			raw_response LONGTEXT COMMENT '原始响应体，log_raw_response 打开时才存',
			raw_size INT NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			INDEX idx_ta_log_env (env_id),
			INDEX idx_ta_log_started (started_at),
			INDEX idx_ta_log_ok (ok)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`},

		// ---------------- 多副本选主：同一时刻只允许一个实例采集/告警 ----------------
		// 后端是多副本部署，如果每个副本都跑调度器，会重复请求中台、
		// 更糟的是同一条告警被重复 @ 到群里。用一行租约做选主。
		{"table_alert_leader", `
		CREATE TABLE IF NOT EXISTS table_alert_leader (
			role VARCHAR(32) PRIMARY KEY COMMENT 'collector / alerter',
			holder VARCHAR(191) NOT NULL DEFAULT '' COMMENT '持有者实例标识',
			expires_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '租约到期时间，过期后其他实例可抢',
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`},

		// ---------------- 通知发送记录：扯皮时的证据 ----------------
		{"table_alert_notify_logs", `
		CREATE TABLE IF NOT EXISTS table_alert_notify_logs (
			id VARCHAR(36) PRIMARY KEY,
			event_id VARCHAR(36) NOT NULL DEFAULT '',
			env_name VARCHAR(64) NOT NULL DEFAULT '',
			table_no VARCHAR(64) NOT NULL DEFAULT '',
			bot_id VARCHAR(36) NOT NULL DEFAULT '',
			bot_name VARCHAR(64) NOT NULL DEFAULT '',
			kind VARCHAR(20) NOT NULL DEFAULT 'alert' COMMENT 'alert/escalate/recover/test',
			seq INT NOT NULL DEFAULT 0 COMMENT '第几次告警',
			at_lark_ids VARCHAR(1000) NOT NULL DEFAULT '',
			title VARCHAR(500) NOT NULL DEFAULT '',
			body LONGTEXT,
			ok TINYINT(1) NOT NULL DEFAULT 0,
			error_msg TEXT,
			sent_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			INDEX idx_ta_notify_event (event_id),
			INDEX idx_ta_notify_sent (sent_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`},
	}

	for _, s := range stmts {
		if _, err := DB.Exec(s.ddl); err != nil {
			log.Printf("[table-alert] 建表 %s 失败: %v", s.name, err)
			return err
		}
	}

	// 选主用的两行，缺了就补
	for _, role := range []string{"collector", "alerter"} {
		DB.Exec(`INSERT IGNORE INTO table_alert_leader (role, holder, expires_at) VALUES (?, '', NOW())`, role)
	}

	seedTableAlertEnvs()
	log.Println("[table-alert] 数据表初始化完成")
	return nil
}

// seedTableAlertEnvs 预置 UAT / PROD 两条**禁用且地址为空**的环境模板。
// 只预置已验证过的解析默认值，地址和 token 一律留空由使用者填。
func seedTableAlertEnvs() {
	var n int
	if err := DB.QueryRow(`SELECT COUNT(*) FROM table_alert_envs`).Scan(&n); err != nil || n > 0 {
		return
	}
	for i, name := range []string{"UAT", "PROD"} {
		_, err := DB.Exec(`
			INSERT INTO table_alert_envs (id, name, enabled, sort_order, url, method, host_header, token_place, created_by)
			VALUES (UUID(), ?, 0, ?, '', 'GET', '', 'none', 'system')`, name, i)
		if err != nil {
			log.Printf("[table-alert] 预置环境 %s 失败: %v", name, err)
		}
	}
	log.Println("[table-alert] 已预置 UAT / PROD 空环境模板（未启用，地址待填）")
}
