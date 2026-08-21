-- 授权落库 + 安装标识：把 001 里预留的两张表**重建成 licensekit 要求的形状**。
--
-- # 为什么是重建而不是 CREATE IF NOT EXISTS
--
-- 🔴 `CREATE TABLE IF NOT EXISTS` 对「表在、但结构不同」是**静默通过**的。
--    001_platform.sql 早就建过这两张表（当时是预留，从没接线），列名和形状都不一样：
--      install_identity  install_id CHAR(36)      ≠ install_uuid VARCHAR(64)
--      licenses          多行自增 + status/checked_at ≠ 单行 + mismatch_since/updated_by
--    第一版这个迁移写成 CREATE IF NOT EXISTS，于是建表那步"成功"了，
--    紧接着的 INSERT 才报 `Unknown column 'install_uuid'` —— 报错点离真正的原因隔了三行。
--    更糟的情况是根本不报错，代码照着不存在的语义跑下去。
--
-- # 为什么 DROP 是安全的
--
-- 两张表**从来没有任何代码写过**（001 里是预留，授权模块此前只有只读的状态接口），
-- 实测两张表都是 0 行。所以这里没有数据要迁。
--
-- ⚠️ 如果将来发现某个部署里 licenses 有数据，**不要照抄这个迁移**：
--    install_identity 一旦重建，install_uuid 就变了 → 安装指纹变了 →
--    已签发的授权全部对不上，表现是"好好的突然提示指纹不符"。
--    那种情况要写成 ALTER + 数据搬迁。

DROP TABLE IF EXISTS licenses;
DROP TABLE IF EXISTS install_identity;

-- ⚠️ 平台级表，不带 tenant_id：授权买的是这套部署，不是某个租户。
--    对应 internal/store/whitelist.go 里的白名单条目。
--
-- ⚠️ 单行表（id 恒为 1）。做成多行"历史记录"的话，「当前生效的是哪一条」
--    就成了一个需要判断的问题，而判断错的方向是**多给权限**。
--    换授权就是覆盖这一行；要留痕去看审计表。
CREATE TABLE licenses (
  id             TINYINT      NOT NULL DEFAULT 1 COMMENT '恒为 1，单行表',
  token          MEDIUMTEXT   NOT NULL COMMENT '签名后的授权串，原样存；验签在内存里做',

  -- ⚠️ 这一列是指纹防拷贝那道防线**能不能生效**的关键。
  --
  -- 宽限期必须从「首次发现指纹不匹配」起算。若不落库、每次在内存里用 now() 现取，
  -- 那么进程一重启宽限期就重置一次 —— 等于永久宽限，防线形同虚设，
  -- 而且它不报错、状态一直显示「宽限期内」，没有任何迹象表明防线没工作。
  --
  -- NULL = 从未发现过不匹配（不是「刚刚发现」）。这两者绝不能混：
  -- 用零值当「没发现」的话，第一次真的发现时会被当成 1970 年就发现了，直接判过期。
  mismatch_since DATETIME     NULL COMMENT '首次发现安装指纹不匹配的时刻；NULL=从未不匹配',

  -- Watch 的 Revision 读这一列判断「要不要重新装载」，每个副本每 20s 读一次。
  -- 必须廉价 —— 单行表主键查询，够廉价。
  updated_at     DATETIME(6)  NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
  updated_by     VARCHAR(255) NOT NULL DEFAULT '' COMMENT '谁激活的，审计用',
  PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='授权，单行';

-- 安装标识：安装指纹的两个输入之一（另一个是数据库的 system identifier）。
--
-- ⚠️ 必须来自数据库，**绝不能取宿主机 MAC / hostname / machine-id**。
--    多副本下每个 Pod 的宿主机标识都不同，用宿主机派生会让绑定安装的授权
--    在部分副本上校验失败 —— 表现是「重启后偶发提示未授权」，
--    只在一部分请求上出现，是最难排查的一类故障。
CREATE TABLE install_identity (
  id           TINYINT     NOT NULL DEFAULT 1 COMMENT '恒为 1，单行表',
  install_uuid VARCHAR(64) NOT NULL COMMENT '本次安装的 UUID，生成一次后永不变',
  created_at   DATETIME    NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='安装标识，单行';

-- 种一行。UUID() 由数据库生成，保证同一套库里所有副本拿到的是同一个值 ——
-- 应用侧生成的话，两个副本同时启动会各生成一个，谁写进去看运气。
INSERT IGNORE INTO install_identity (id, install_uuid) VALUES (1, UUID());
