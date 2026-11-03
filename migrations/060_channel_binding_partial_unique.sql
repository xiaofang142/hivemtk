-- 060: channel_agent_bindings 唯一索引 partial 化（T1 二次回归高危项）
--
-- 现象：换绑智能体时 POST 409 / PUT 404——软删除行（deleted_at NOT NULL）
-- 仍占用 idx_channel_binding_unique (tenant_id, channel_type, account_id,
-- COALESCE(chat_id,''))，而应用层没有任何「复活软删行」的路径，绑定死锁。
--
-- 修复：唯一索引加 WHERE deleted_at IS NULL——活跃行之间仍唯一，软删行
-- 让出键位，新建绑定直接成功。语义与 GORM 查询（全部带 deleted_at IS NULL）
-- 完全对齐。

DROP INDEX IF EXISTS idx_channel_binding_unique;

CREATE UNIQUE INDEX idx_channel_binding_unique
    ON channel_agent_bindings (tenant_id, channel_type, account_id, COALESCE(chat_id, ''::character varying))
    WHERE deleted_at IS NULL;
