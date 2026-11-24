-- 080_customer_source_tracking.sql
-- PR-3/P-01/S-03: 客户来源追踪字段。幂等。
ALTER TABLE customers ADD COLUMN IF NOT EXISTS source_account_id varchar(64) NOT NULL DEFAULT '';
ALTER TABLE customers ADD COLUMN IF NOT EXISTS source_channel varchar(32) NOT NULL DEFAULT '';
ALTER TABLE customers ADD COLUMN IF NOT EXISTS source_conversation_id varchar(128) NOT NULL DEFAULT '';
ALTER TABLE customers ADD COLUMN IF NOT EXISTS source_message_id varchar(128) NOT NULL DEFAULT '';
ALTER TABLE customers ADD COLUMN IF NOT EXISTS first_touch_at timestamptz;
ALTER TABLE customers ADD COLUMN IF NOT EXISTS last_touch_at timestamptz;
CREATE INDEX IF NOT EXISTS idx_customers_source_account ON customers(source_account_id);
CREATE INDEX IF NOT EXISTS idx_customers_source_channel ON customers(source_channel);
CREATE INDEX IF NOT EXISTS idx_customers_source_conversation ON customers(source_conversation_id);
CREATE INDEX IF NOT EXISTS idx_customers_first_touch ON customers(first_touch_at);
CREATE INDEX IF NOT EXISTS idx_customers_last_touch ON customers(last_touch_at);
