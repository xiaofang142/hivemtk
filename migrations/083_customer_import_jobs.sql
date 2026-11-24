-- 083_customer_import_jobs.sql
-- PR-6/R-05: 统一客户导入任务。幂等。
CREATE TABLE IF NOT EXISTS customer_import_jobs (
  id bigserial PRIMARY KEY,
  tenant_id bigint NOT NULL DEFAULT 1,
  job_id varchar(32) NOT NULL,
  entity_type varchar(32) NOT NULL DEFAULT 'customer',
  file_name varchar(255) NOT NULL DEFAULT '',
  file_path varchar(512) NOT NULL DEFAULT '',
  status varchar(32) NOT NULL DEFAULT 'pending',
  total_rows int NOT NULL DEFAULT 0,
  success_rows int NOT NULL DEFAULT 0,
  failed_rows int NOT NULL DEFAULT 0,
  skipped_rows int NOT NULL DEFAULT 0,
  updated_rows int NOT NULL DEFAULT 0,
  dedup_strategy varchar(32) NOT NULL DEFAULT 'skip',
  mapping_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  precheck_report text NOT NULL DEFAULT '',
  error_report text NOT NULL DEFAULT '',
  created_by bigint,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  deleted_at timestamptz
);
ALTER TABLE customer_import_jobs ADD COLUMN IF NOT EXISTS file_path varchar(512) NOT NULL DEFAULT '';
ALTER TABLE customer_import_jobs ADD COLUMN IF NOT EXISTS precheck_report text NOT NULL DEFAULT '';
ALTER TABLE customer_import_jobs ADD COLUMN IF NOT EXISTS skipped_rows int NOT NULL DEFAULT 0;
ALTER TABLE customer_import_jobs ADD COLUMN IF NOT EXISTS updated_rows int NOT NULL DEFAULT 0;
CREATE UNIQUE INDEX IF NOT EXISTS uk_customer_import_jobs_tenant_job ON customer_import_jobs(tenant_id, job_id);
CREATE INDEX IF NOT EXISTS idx_customer_import_jobs_tenant_status ON customer_import_jobs(tenant_id, status);
