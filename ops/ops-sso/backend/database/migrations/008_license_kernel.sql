-- 授权切换到共享内核 ops-kit/licensekit。规范见 ops/LICENSING.md。
--
-- 本产品原先自带一份授权实现（自己验签、自己算指纹、自己定状态机），
-- 与共享内核在三处实质分叉：
--
--   宽限期     30 天  vs 内核的 14 天
--   指纹算法   sha256("oap-install|" + @@server_uuid) 取前 16 字节
--              vs 内核的 HMAC-SHA256(install_uuid | @@server_id)
--   指纹宽限   从**签发日**起算 vs 内核的从「首次发现不匹配」起算
--
-- 指纹算法不同意味着同一套环境里两个产品算出两个不同指纹，
-- 而 §4 要求整份 license 只有一个 install_id —— 这条从根上就对不上。
-- 本迁移补齐内核需要的两张东西，实现随之统一。
--
-- ⚠️ 无兼容负担：切换前内置公钥是全 0 占位，本产品从未验过任何一张真 license。

-- 安装标识：安装指纹的两个输入之一（另一个是数据库的 system identifier）。
--
-- ⚠️ 必须来自数据库，**绝不能取宿主机 MAC / hostname / machine-id**。
--    多副本下每个 Pod 的宿主机标识都不同，用宿主机派生会让绑定安装的授权
--    在部分副本上校验失败 —— 表现是「重启后偶发提示未授权」，
--    只在一部分请求上出现，是最难排查的一类故障。
CREATE TABLE IF NOT EXISTS install_identity (
  id           TINYINT     NOT NULL DEFAULT 1 COMMENT '恒为 1，单行表',
  install_uuid VARCHAR(64) NOT NULL COMMENT '本次安装的 UUID，生成一次后永不变',
  created_at   DATETIME    NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='安装标识，单行';

-- 种一行。UUID() 由数据库生成，保证同一套库里所有副本拿到的是同一个值 ——
-- 应用侧生成的话，两个副本同时启动会各生成一个，谁写进去看运气。
INSERT IGNORE INTO install_identity (id, install_uuid) VALUES (1, UUID());

-- 首次发现安装指纹不匹配的时刻。
--
-- ⚠️ 这一列是 §4 指纹防拷贝那道防线**能不能生效**的关键。
--    宽限期必须从「首次发现不匹配」起算。不落库、每次在内存里用 now() 现取的话，
--    进程一重启宽限期就重置一次 —— 等于永久宽限，而且它不报错、
--    状态一直显示"宽限期内"，没有任何迹象表明防线没在工作。
--
-- NULL = 从未发现过不匹配（不是"刚刚发现"）。这两者绝不能混：
-- 用零值当"没发现"，第一次真的发现时会被当成 1970 年就发现了，直接判过期。
ALTER TABLE licenses ADD COLUMN mismatch_since DATETIME NULL
  COMMENT '首次发现安装指纹不匹配的时刻；NULL=从未不匹配';

-- Watch 的 Revision 读这一列判断"要不要重新装载"，每个副本每 20s 读一次。
-- 必须廉价 —— 主键/索引查询即可。
ALTER TABLE licenses ADD COLUMN updated_at DATETIME(6) NOT NULL
  DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6);
