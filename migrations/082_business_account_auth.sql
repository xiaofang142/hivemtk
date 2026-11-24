-- 082_business_account_auth.sql
-- PR-5/D-03: 业务账号授权矩阵。幂等。
CREATE TABLE IF NOT EXISTS business_account_authorizations (
  id bigserial PRIMARY KEY,
  tenant_id bigint NOT NULL DEFAULT 1,
  user_id bigint NOT NULL,
  channel varchar(32) NOT NULL DEFAULT '',
  account_id varchar(128) NOT NULL DEFAULT '',
  scope varchar(16) NOT NULL DEFAULT 'read',
  granted_by bigint,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  deleted_at timestamptz
);
CREATE UNIQUE INDEX IF NOT EXISTS uk_baa_tenant_user_channel_account ON business_account_authorizations(tenant_id, user_id, channel, account_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_baa_tenant_user ON business_account_authorizations(tenant_id, user_id);
CREATE INDEX IF NOT EXISTS idx_baa_tenant_account ON business_account_authorizations(tenant_id, channel, account_id);
