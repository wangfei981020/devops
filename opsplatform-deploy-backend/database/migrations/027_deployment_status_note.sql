-- Migration 027: deployment 增加 status_note 列
--
-- 对账/中断这类"给人看的说明"（如"后端重启后自动对账确认已就绪"、"服务未受影响，请重试"）
-- 原来都塞进 error_msg，但 error_msg 对非 admin 会整段脱敏成"（失败详情已隐藏）"——
-- 导致成功记录挂"错误信息"、中断记录的友好文案也被吞掉。
--
-- status_note 专放干净、可安全展示的中文说明，列表接口不脱敏；
-- error_msg 只保留真失败的技术细节（可能含 git stderr/路径，继续对非 admin 脱敏）。

ALTER TABLE deployment
  ADD COLUMN status_note VARCHAR(500) NOT NULL DEFAULT '' AFTER error_msg;
