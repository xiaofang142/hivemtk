-- 095_drop_bridge_token.sql
-- T063 / FR-028：识别码一次性替换旧 Bridge 通道凭证（三步切换第三步）。
-- T058 已把读路径切到 channel_identify_codes（旧 X-Bridge-Token 0% 接受）；
-- T061 已停用双写；本迁移移除旧凭证的 KV 字段，彻底清除写/存的残留。
-- 幂等：重复执行无副作用；不删业务表，不碰渠道/账号/识别码主数据。
--
-- 旧凭证曾同时存在于 platform_config_kv（实例级）与 system_config_kv（租户级），
-- 两处都清掉，避免升级后仍有运营面回显"bridge_ingest_token 已配置"的错觉。
DELETE FROM platform_config_kv WHERE key IN ('bridge_ingest_token', 'bridge_ingest_token_prev');
DELETE FROM system_config_kv WHERE key IN ('bridge_ingest_token', 'bridge_ingest_token_prev');

-- T061 重置在途窗口：旧码短时保留的字段（AutoMigrate 也会补，这里给纯 SQL 部署同源契约）。
ALTER TABLE channel_identify_codes ADD COLUMN IF NOT EXISTS prev_code VARCHAR(128) NOT NULL DEFAULT '';
ALTER TABLE channel_identify_codes ADD COLUMN IF NOT EXISTS prev_valid_until TIMESTAMPTZ;
