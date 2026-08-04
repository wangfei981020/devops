package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"opsplatform-cmdb-backend/crypto"
	"opsplatform-cmdb-backend/logx"
	"opsplatform-cmdb-backend/n9e"
)

// 夜莺告警接入。
//
//	为什么 CMDB 要接告警：排障时最费时间的不是"看告警"，而是在告警系统和
//	资产系统之间来回切、自己在脑子里做关联——「这条告警的机器属于哪个项目」
//	「这个域名归谁」。事件中心已经聚合了到期/变更/同步失败/K8s Warning，
//	告警是缺的最后一块。补上之后，AI 经 MCP 一次问答就能同时拿到告警和资产关联。
//
//	接入方式复用 obs_endpoints（type='n9e'），和 Prometheus/Loki/KubeSphere
//	同一套接入管理、同一套凭据加密，不另造一张表。

type AlertHandler struct {
	DB     *sql.DB
	Cipher *crypto.Cipher
}

func NewAlertHandler(db *sql.DB, cipher *crypto.Cipher) *AlertHandler {
	return &AlertHandler{DB: db, Cipher: cipher}
}

func (h *AlertHandler) Register(r *gin.RouterGroup) {
	r.GET("/alerts", h.List)
}

// n9eClient 按环境解析夜莺接入。
//
//	**没配置返回 (nil, nil)，不是错误**。这两种状态给用户看到的意思完全不同：
//	  未接入   → "去接入管理配一个"
//	  接入了但坏 → "连不上/token 失效，去查"
//	把前者当错误报，用户会以为系统出问题了；反过来把后者当"无告警"，
//	更糟——那是把故障伪装成正常。
//	resolveEndpointFull 找不到数据源时返回 error，这里把它翻译成"未配置"。
func n9eClient(db *sql.DB, cipher *crypto.Cipher, env string) (*n9e.Client, error) {
	url, token, _, err := resolveEndpointFull(db, cipher, "n9e", env, 0)
	if err != nil {
		logx.Line("alerts", fmt.Sprintf("夜莺接入未就绪（env=%s）：%v", env, err))
		return nil, nil
	}
	if url == "" {
		return nil, nil
	}
	return n9e.New(url, token), nil
}

// List GET /api/alerts?state=current|history&env=&hours=&limit=
func (h *AlertHandler) List(c *gin.Context) {
	env := c.Query("env")
	state := c.DefaultQuery("state", "current")
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "200"))

	cli, err := n9eClient(h.DB, h.Cipher, env)
	if err != nil {
		c.JSON(500, gin.H{"error": "解析夜莺接入失败：" + err.Error()})
		return
	}
	if cli == nil {
		// 没配不是错误，但必须说清楚——否则空列表会被当成"当前无告警"，
		// 那是把"没接入"伪装成"一切正常"
		c.JSON(200, gin.H{
			"list": []gin.H{}, "configured": false,
			"hint": "尚未接入夜莺。请到「接入管理 → 数据源」添加一个类型为 n9e 的接入点",
		})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 25*time.Second)
	defer cancel()

	var events []n9e.AlertEvent
	var total int64
	if state == "history" {
		hours, _ := strconv.Atoi(c.DefaultQuery("hours", "24"))
		if hours < 1 || hours > 24*30 {
			hours = 24
		}
		etime := time.Now().Unix()
		stime := etime - int64(hours)*3600
		events, total, err = cli.HistoryAlerts(ctx, stime, etime, limit)
	} else {
		events, err = cli.CurrentAlerts(ctx, limit)
		total = int64(len(events))
	}
	if err != nil {
		logx.Line("alerts", fmt.Sprintf("WARN 拉夜莺告警失败 env=%s state=%s: %v", env, state, err))
		c.JSON(502, gin.H{"error": "拉取告警失败：" + err.Error(), "configured": true})
		return
	}

	list := make([]gin.H, 0, len(events))
	for _, e := range events {
		item := gin.H{
			"id": e.ID, "rule_name": e.RuleName, "rule_note": e.RuleNote,
			"severity": e.SeverityLabel(), "object": e.Object(),
			"cluster": e.Cluster, "group": e.GroupName,
			"trigger_value": e.TriggerValue, "tags": e.Tags,
			"recovered":    e.IsRecovered == 1,
			"trigger_time": tsFmt(e.TriggerTime),
		}
		if e.RecoverTime > 0 {
			item["recover_time"] = tsFmt(e.RecoverTime)
		}
		list = append(list, item)
	}
	c.JSON(200, gin.H{"list": list, "total": total, "configured": true, "state": state})
}

func tsFmt(ts int64) string {
	if ts <= 0 {
		return ""
	}
	return time.Unix(ts, 0).Format("2006-01-02 15:04:05")
}

// collectAlerts 把夜莺告警并进事件中心的时间线。
//
//	只取**当前活跃**的：事件中心是"最近发生了什么"的视图，
//	把几千条已恢复的历史告警灌进去会把其他来源全淹掉。
//	要翻历史去告警页按时间筛。
func (h *EventCenterHandler) collectAlerts(ctx context.Context, cipher *crypto.Cipher, add func(evt)) {
	cli, err := n9eClient(h.DB, cipher, "")
	if err != nil || cli == nil {
		return // 没接入就跳过，不影响其他来源
	}
	events, err := cli.CurrentAlerts(ctx, 200)
	if err != nil {
		logx.Line("event_center", fmt.Sprintf("WARN 拉夜莺告警失败，事件中心少了告警这一路: %v", err))
		return
	}
	for _, e := range events {
		if e.IsRecovered == 1 {
			continue
		}
		msg := e.RuleNote
		if e.TriggerValue != "" {
			msg = fmt.Sprintf("%s（当前值 %s）", msg, e.TriggerValue)
		}
		t := time.Unix(e.TriggerTime, 0)
		add(evt{
			Time:    t.Format("2006-01-02 15:04:05"),
			Source:  "alert",
			Level:   e.SeverityLabel(),
			Object:  e.Object(),
			Title:   e.RuleName,
			Message: msg,
			Cluster: e.Cluster,
			sortTs:  t,
		})
	}
}
