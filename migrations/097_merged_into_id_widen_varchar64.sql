-- Migration 097: 合并目标客户 ID 宽度与 customer_id 主链对齐。
--
-- 背景：045_customer_id_type_align.sql 统一 customer_id 为 VARCHAR(64)，
-- 096/v3.113 又对存量列做了幂等加宽；但 093_crm_entities.sql 新建的
-- customers.merged_into_id 仍是 VARCHAR(36)。合并目标一旦超过 36 字符
-- （例如带租户前缀的 OneID）会被截断或写入失败，导致合并审计链断裂。
--
-- 幂等：只在列存在且当前长度 < 64 时加宽，不改动已是 64 的列。
DO $$
BEGIN
  IF EXISTS (
    SELECT 1 FROM information_schema.columns
    WHERE table_schema = 'public'
      AND table_name = 'customers'
      AND column_name = 'merged_into_id'
      AND data_type = 'character varying'
      AND character_maximum_length < 64
  ) THEN
    ALTER TABLE customers
      ALTER COLUMN merged_into_id TYPE VARCHAR(64)
      USING merged_into_id::varchar(64);
    RAISE NOTICE 'Widened customers.merged_into_id to VARCHAR(64)';
  END IF;
END $$;
