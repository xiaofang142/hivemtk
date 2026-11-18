-- 076_pg_enums_im_platforms.sql
-- specs/025：platform_type_enum 追平新平台渠道常量（wecomcs/onebot/slack/wecombot）。
--
-- 与 063 同口径：只有离线 SQL 部署路径存在 ENUM 类型；AutoMigrate 管理的库
-- platform 列是 VARCHAR，按类型存在性守卫，不存在则整段跳过。
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_type WHERE typname = 'platform_type_enum') THEN
    ALTER TYPE platform_type_enum ADD VALUE IF NOT EXISTS 'wecomcs';
    ALTER TYPE platform_type_enum ADD VALUE IF NOT EXISTS 'onebot';
    ALTER TYPE platform_type_enum ADD VALUE IF NOT EXISTS 'slack';
    ALTER TYPE platform_type_enum ADD VALUE IF NOT EXISTS 'wecombot';
  END IF;
END $$;
