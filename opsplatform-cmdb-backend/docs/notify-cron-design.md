# CMDB 通知中心 + 可配置定时任务 — 设计

> 日期：2026-06-24 ｜ 状态：已确认，进入实现

## 1. 需求（brainstorm 确认）

- **飞书 = Lark**（同一产品，叫法不同）：完善现有 `notify.SendFeishu` webhook，不新写一套。
- **通知人 = @ 特定责任人**：存每人的 `open_id` + 姓名，发通知时拼 `<at user_id="ou_xxx"></at>` 精确 @。
- **4 个定时任务全部可配**（单独开关 + 频率 + 立即运行）：
  1. 刷新到期时间（WHOIS 域名注册到期 + 连 443 主域名证书到期）
  2. 证书自动续期（到期前 30 天重签）
  3. 到期提醒推送（命中阈值发飞书）
  4. 检测所有解析证书（逐条 443 巡检）
- **频率 = 预设下拉 + 自定义 cron**：预设映射成 cron 表达式存库。
- **前端 = Element Plus 风格**（与回退后整体一致，非 Tailwind）。

## 2. 数据模型

**新表 `scheduled_tasks`**（结构化存任务配置，不塞 settings KV）：

| 字段 | 说明 |
|---|---|
| task_key | 唯一键：refresh_expiry / auto_renew / remind / inspect |
| name | 显示名 |
| enabled | 0/1 开关 |
| schedule | cron 表达式（预设也转成 cron 存） |
| last_run_at / last_result | 上次运行时间 + 结果 |

**新表 `notify_users`**：`id`、`name`、`open_id`、`enabled`。

**settings 复用 + 新增 key**：`feishu_webhook`、`remind_days`、事件开关 `notify_cert_expiring` / `notify_renew_success` / `notify_renew_fail` / `notify_domain_expiring`。

## 3. 后端调度

- 引入 `github.com/robfig/cron/v3`。
- 启动时从 `scheduled_tasks` 读每个 enabled 任务的 cron，注册 entry；改配置后热重载（重建 cron）。
- 4 个任务函数：复用现有 `refreshAllDomains` / `renewDue` / `remindExpiry`，新增「检测所有解析证书」批量函数。
- 预设频率映射：每3h→`0 */3 * * *`、每6h→`0 */6 * * *`、每天一次→`0 3 * * *`、每天两次→`0 3,15 * * *` 等。
- 每次跑完写 `last_run_at` / `last_result`。

## 4. 通知增强

- `SendFeishu(webhook, text)` 增强为支持 @ 列表：消息尾部按 `notify_users` 拼 `<at user_id="ou_xxx"></at>`。
- 提醒 / 续期失败时带上 @；按事件开关决定发不发。

## 5. 前端（Element Plus）

- **通知页**（/notify）：机器人配置（webhook + 测试发送按钮）+ 通知人表格（name/open_id 增删）+ 通知规则（remind_days 阈值 + 4 事件开关）。
- **定时任务页**（/cron）：4 个任务卡，每个 = 开关 + 频率（预设下拉 + 自定义 cron 输入）+ 上次/下次运行 + 立即运行按钮。
- 路由 + 侧边栏菜单加这两页。

## 6. API

- `GET/PUT /scheduled-tasks`、`POST /scheduled-tasks/:key/run`
- `GET/POST/DELETE /notify-users`
- `POST /notify/test`
- settings 复用现有 `GET/PUT /settings`

## 7. 分阶段实现

1. **阶段①：定时任务调度** — cron 库 + scheduled_tasks 表 + 后端 4 任务接入 + 定时任务页。
2. **阶段②：通知增强** — notify_users 表 + @ open_id + 通知页。
