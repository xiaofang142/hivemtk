-- 087_lead_mining_config_ext.sql
-- LM-PR1: 线索发掘配置扩展（负向关键词/敏感词/账号范围/去重窗口）。幂等。
ALTER TABLE lead_mining_configs ADD COLUMN IF NOT EXISTS negative_keywords jsonb NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE lead_mining_configs ADD COLUMN IF NOT EXISTS sensitive_words jsonb NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE lead_mining_configs ADD COLUMN IF NOT EXISTS account_scope jsonb NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE lead_mining_configs ADD COLUMN IF NOT EXISTS dedup_window_minutes int NOT NULL DEFAULT 60;
ALTER TABLE lead_mining_configs ADD COLUMN IF NOT EXISTS judge_mode varchar(32) NOT NULL DEFAULT 'llm';
ALTER TABLE lead_mining_configs ADD COLUMN IF NOT EXISTS notify_external_enabled boolean NOT NULL DEFAULT false;
