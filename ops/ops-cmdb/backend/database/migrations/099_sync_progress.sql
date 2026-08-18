-- 数据源同步进度：从进程内内存挪到库里。
--
-- 原来进度存在 `syncStore map[int]*syncState` 里。单副本没问题；
-- 多副本下前端轮询会打到**没跑这次同步的那个 Pod**，
-- 于是界面显示"没在同步"，而实际正跑着 —— 用户以为失败了，再点一次。
--
-- ⚠️ 带 tenant_id：同步的是某个租户的数据源，进度也只该给那个租户看。
CREATE TABLE IF NOT EXISTS sync_progress (
  tenant_id    BIGINT       NOT NULL DEFAULT 0,
  source_id    INT          NOT NULL COMMENT '数据源(registrar) id',
  running      TINYINT      NOT NULL DEFAULT 0,
  total        INT          NOT NULL DEFAULT 0,
  done         INT          NOT NULL DEFAULT 0,
  synced       INT          NOT NULL DEFAULT 0 COMMENT '已同步域名数',
  records      INT          NOT NULL DEFAULT 0 COMMENT '拉到的解析条数',
  imported     INT          NOT NULL DEFAULT 0 COMMENT '导入的业务解析数',
  stale        INT          NOT NULL DEFAULT 0 COMMENT '标记失效的域名数',
  err          VARCHAR(500) NOT NULL DEFAULT '',
  -- owner 记录是哪个副本在跑。排障时能直接对上 Pod 日志，
  -- 不用挨个 Pod 翻。
  owner        VARCHAR(255) NOT NULL DEFAULT '',
  started_at   DATETIME     NULL,
  finished_at  DATETIME     NULL,
  -- ⚠️ 心跳。副本中途被杀时 running 会永远停在 1，
  --    界面就一直显示"同步中"且点不了重试。
  --    判"是否真在跑"必须同时看 running 和这个字段的新鲜度。
  updated_at   TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (tenant_id, source_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='数据源同步进度(跨副本可见)';
