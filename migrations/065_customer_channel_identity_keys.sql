-- 065_customer_channel_identity_keys.sql
-- specs/024 T501：customer_channels 双层唯一键（身份锚 + 业务键）。
--
-- 为什么两条键而不是一条：
--   业务键 (tenant_id, one_id, channel, account_id) 保证「同一客户在同一渠道同一账号只有一行」；
--   身份锚 (tenant_id, channel, account_id, channel_user_id) 保证「同一平台账号不会落到两个客户」。
--   单键只能表达一种唯一：只留业务键时，同一 channel_user_id 先后被归到两个 OneID 会写出两行，
--   工作台里同一个客户出现两遍；只留身份锚时，同一 OneID 在同一账号重复绑定又拦不住。
--
-- 两条索引都带 WHERE deleted_at IS NULL：软删行不再占键。否则「删了再插同一身份」永远撞唯一键，
-- 而 upsert 的 ON CONFLICT 目标必须与索引谓词完全一致才成立。
--
-- 执行要求：本文件必须在**非事务**连接下执行（CREATE INDEX CONCURRENTLY 不能在事务块内）。
--   psql -v ON_ERROR_STOP=1 -f 065_customer_channel_identity_keys.sql
-- scripts/bootstrap.sh 正是逐语句执行，满足该前提。

-- ---------------------------------------------------------------------------
-- 0) NULL 归一：历史行 account_id / channel_user_id 可能为 NULL，
--    部分索引谓词 `channel_user_id <> ''` 对 NULL 求值为 NULL（不索引），会漏掉真正的重复。
-- ---------------------------------------------------------------------------
UPDATE customer_channels SET account_id = '' WHERE account_id IS NULL;
UPDATE customer_channels SET channel_user_id = '' WHERE channel_user_id IS NULL;

-- ---------------------------------------------------------------------------
-- 1) 身份锚去重（先跑）：同 (tenant, channel, account, channel_user_id) 只保留
--    last_seen_at 最新的一行，其余软删。不复活：复活会重新制造冲突。
-- ---------------------------------------------------------------------------
WITH ranked AS (
    SELECT id,
           row_number() OVER (
               PARTITION BY tenant_id, channel, account_id, channel_user_id
               ORDER BY last_seen_at DESC NULLS LAST, id DESC
           ) AS rn
    FROM customer_channels
    WHERE deleted_at IS NULL
      AND channel_user_id <> ''
)
UPDATE customer_channels cc
SET deleted_at = now()
FROM ranked r
WHERE cc.id = r.id
  AND r.rn > 1;

-- ---------------------------------------------------------------------------
-- 2) 业务键去重（后跑）：同 (tenant, one_id, channel, account_id) 只保留最新一行。
--    必须在身份锚之后：先把身份重复收敛掉，业务键冲突组里剩下的才是真冲突。
-- ---------------------------------------------------------------------------
WITH ranked AS (
    SELECT id,
           row_number() OVER (
               PARTITION BY tenant_id, one_id, channel, account_id
               ORDER BY last_seen_at DESC NULLS LAST, id DESC
           ) AS rn
    FROM customer_channels
    WHERE deleted_at IS NULL
)
UPDATE customer_channels cc
SET deleted_at = now()
FROM ranked r
WHERE cc.id = r.id
  AND r.rn > 1;

-- ---------------------------------------------------------------------------
-- 3) 建索引（CONCURRENTLY：不锁写入）。两条都 IF NOT EXISTS，可重跑。
-- ---------------------------------------------------------------------------
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_channel_identity
    ON customer_channels (tenant_id, channel, account_id, channel_user_id)
    WHERE deleted_at IS NULL AND channel_user_id <> '';

CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_oneid_channel_v2
    ON customer_channels (tenant_id, one_id, channel, account_id)
    WHERE deleted_at IS NULL;

-- 4) 用新键替换旧键（旧 idx_oneid_channel 只有 (one_id, channel) —— 应用层 tenancyidx
--    启动时会把 SQL 侧从未建过的旧索引「想象」成 (tenant_id, one_id, channel)，
--    所以这里必须判存在再删）。
DROP INDEX CONCURRENTLY IF EXISTS idx_oneid_channel;

ALTER INDEX IF EXISTS idx_oneid_channel_v2 RENAME TO idx_oneid_channel;

-- ---------------------------------------------------------------------------
-- 5) 人工可核的输出：软删行数 + 冲突组样例（手工执行时 psql 会打印）。
-- ---------------------------------------------------------------------------
SELECT count(*) FILTER (WHERE deleted_at IS NOT NULL) AS soft_deleted_total,
       count(*) FILTER (WHERE deleted_at IS NULL)     AS alive_total
FROM customer_channels;

SELECT tenant_id, channel, account_id, channel_user_id, count(*) AS dup_rows
FROM customer_channels
WHERE deleted_at IS NULL AND channel_user_id <> ''
GROUP BY 1, 2, 3, 4
HAVING count(*) > 1
ORDER BY dup_rows DESC
LIMIT 10;

SELECT tenant_id, one_id, channel, account_id, count(*) AS dup_rows
FROM customer_channels
WHERE deleted_at IS NULL
GROUP BY 1, 2, 3, 4
HAVING count(*) > 1
ORDER BY dup_rows DESC
LIMIT 10;
