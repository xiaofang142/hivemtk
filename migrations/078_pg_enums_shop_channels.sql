-- 078_pg_enums_shop_channels.sql
-- 电商店铺五渠道（CH-15/18/19/28/35）：platform_type_enum 追平 Go ChannelType 常量
-- （taobao/douyin_shop/kuaishou_shop/shopee/walmart_us，2026-11-19 二次审核批）。
--
-- 与 063/076 同口径：只有离线 SQL 部署路径存在 ENUM 类型；AutoMigrate 管理的库
-- platform 列是 VARCHAR，按类型存在性守卫，不存在则整段跳过。
-- Go 侧 channel_type_enum 由 v3.98.0 Go 迁移同批补值（两枚举同值域）。
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_type WHERE typname = 'platform_type_enum') THEN
    ALTER TYPE platform_type_enum ADD VALUE IF NOT EXISTS 'taobao';
    ALTER TYPE platform_type_enum ADD VALUE IF NOT EXISTS 'douyin_shop';
    ALTER TYPE platform_type_enum ADD VALUE IF NOT EXISTS 'kuaishou_shop';
    ALTER TYPE platform_type_enum ADD VALUE IF NOT EXISTS 'shopee';
    ALTER TYPE platform_type_enum ADD VALUE IF NOT EXISTS 'walmart_us';
  END IF;
END $$;
