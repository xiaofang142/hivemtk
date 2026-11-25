-- Migration 099: SageX 非空安装标识在租户内唯一（Go v3.115.0 的离线对照）。
--
-- 语义：同一 tenant_id + installation_id 只允许一行；空 installation_id 不参与唯一键。
-- 安全：存量重复必须 fail-closed，由运维先修复；本脚本不删除、不合并、不挑选保留行。

BEGIN;

DO $$
DECLARE
    duplicate record;
BEGIN
    SELECT tenant_id, installation_id, COUNT(*) AS row_count
    INTO duplicate
    FROM sagex_installations
    WHERE installation_id <> ''
    GROUP BY tenant_id, installation_id
    HAVING COUNT(*) > 1
    ORDER BY tenant_id, installation_id
    LIMIT 1;

    IF FOUND THEN
        RAISE EXCEPTION
            'SageX installation duplicate data blocks unique index: tenant_id=%, installation_id=%, count=%',
            duplicate.tenant_id,
            duplicate.installation_id,
            duplicate.row_count;
    END IF;
END $$;

CREATE UNIQUE INDEX IF NOT EXISTS uq_sagex_installation_tenant_id
    ON sagex_installations (tenant_id, installation_id)
    WHERE installation_id <> '';

COMMIT;

-- Down（仅回滚本迁移新增索引，不删除安装数据）：
-- DROP INDEX IF EXISTS uq_sagex_installation_tenant_id;
