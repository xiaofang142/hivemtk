-- 096_customer_id_widen_varchar64.sql
-- FIX-5（Phase7 回归修复）：customer_id 全链路收敛到 VARCHAR(64)。
--
-- 背景：customer_id 是 OneID JOIN 键，045 已定调「本期统一为 VARCHAR(64)」，
-- 但 045 只统一了当时已存在的列；migrations/093（contacts/suppliers/
-- service_providers）与 094（clues）在 045 之后新建列，仍是 VARCHAR(36)，
-- 与 GORM tag type:varchar(64) 不一致，存在 JOIN / 写入截断风险。
--
-- 本迁移在 093/094 之后执行，对所有 customer_id 为 character varying 且
-- 长度 < 64 的列幂等加宽到 VARCHAR(64)。
-- 幂等：已是 64（或 > 64 / 非 varchar）的列不动，可重复执行。
-- 防截断：USING customer_id::varchar(64) 显式安全转换；源列宽 < 64 故无数据丢失。
-- 参照 045 的 DO 块幂等风格。

BEGIN;

DO $$
DECLARE
  tab RECORD;
BEGIN
  FOR tab IN
    SELECT c.table_name
    FROM information_schema.columns c
    WHERE c.table_schema = 'public'
      AND c.column_name = 'customer_id'
      AND c.data_type = 'character varying'
      AND c.character_maximum_length < 64
  LOOP
    EXECUTE format(
      'ALTER TABLE %I ALTER COLUMN customer_id TYPE VARCHAR(64) USING customer_id::varchar(64)',
      tab.table_name
    );
    RAISE NOTICE 'Widened customer_id to VARCHAR(64) in %', tab.table_name;
  END LOOP;
END $$;

COMMIT;
