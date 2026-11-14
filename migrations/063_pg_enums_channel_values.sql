-- =============================================================================
-- 063_pg_enums_channel_values.sql
-- 枚举值域补齐：platform_type_enum / source_type_enum 追平 Go 常量（渠道扩容）
--
-- 背景（2026-11-14 架构审计轮，check-enum-consistency.sh 红灯实证）：
--   v3.92.0 Go 迁移为渠道注册表扩容补了 channel_type_enum，但 047_pg_enums.sql
--   建的 platform_type_enum（message_hub.platform，离线 SQL 部署形态）没有跟上：
--   缺 bilibili / jd / pdd / personal_wx / weibo 五值 —— 离线部署库中这五个渠道
--   的 message_hub 行会被 PG 直接拒写（invalid input value for enum），
--   表现为「这些渠道的消息落不了库、AI 永不回复」。AutoMigrate 管理的库
--   （生产口径）该列是 VARCHAR 不受影响，但离线部署路径必须修复。
--   source_type_enum 同理缺 'manual'（kb_crawler.go QACandidateSourceTypeManual）。
--
-- 语义：
--   - ALTER TYPE ... ADD VALUE IF NOT EXISTS：幂等；已存在值跳过。
--   - PG 15 允许事务内 ADD VALUE（新值不得在同事务使用——本迁移只补值不写行）。
--   - 与 check-enum-consistency.sh [1/6][5/6] 的判据同源，补完即绿。
--
-- =============================================================================

-- 已被 047_pg_enums.sql 修正版覆盖的库（新离线部署）无需本迁移；
-- AutoMigrate 管理的库（生产口径，platform 列为 VARCHAR）没有这两个 ENUM
-- 类型，ALTER TYPE 会报错——按类型存在性守卫，不存在则整段跳过。
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_type WHERE typname = 'platform_type_enum') THEN
    -- platform_type_enum：渠道注册表 2026-11 扩容的五值（v3.92.0 Go 侧已有常量）
    ALTER TYPE platform_type_enum ADD VALUE IF NOT EXISTS 'bilibili';
    ALTER TYPE platform_type_enum ADD VALUE IF NOT EXISTS 'jd';
    ALTER TYPE platform_type_enum ADD VALUE IF NOT EXISTS 'pdd';
    ALTER TYPE platform_type_enum ADD VALUE IF NOT EXISTS 'personal_wx';
    ALTER TYPE platform_type_enum ADD VALUE IF NOT EXISTS 'weibo';
  END IF;
  IF EXISTS (SELECT 1 FROM pg_type WHERE typname = 'source_type_enum') THEN
    -- source_type_enum：QACandidateSourceTypeManual（kb_crawler.go）
    ALTER TYPE source_type_enum ADD VALUE IF NOT EXISTS 'manual';
  END IF;
END $$;
