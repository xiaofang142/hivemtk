-- 057: api_logs 敏感字段加密（OPT-SEC-04）
--
-- 存量数据策略说明：
--   ip_address / user_agent 的新写入在应用层（internal/ops/repository/stats.go
--   CreateAPILog）经 internal/pkg/dbencrypt 加密为 `enc:v1:{base64}` 密文。
--   存量明文行**不做批量改写**：纯 SQL 无法实现 AES-256-GCM（nonce 随机生成、
--   密钥仅存在于应用侧 MASTER_KEY，严禁下发到数据库），故保留明文、依赖读取
--   出口 Decrypt 的透传兼容（非 enc:v1: 前缀原样返回）。新写入一律加密。
--
-- 本迁移仅做字段长度保护：密文 = "enc:v1:" + base64(nonce|ciphertext|tag)，
-- 比明文长约 2~4 倍，varchar(45) 无法容纳加密后的 IP，需扩容。全部幂等写法。

-- 1) user_agent 保证为 text（无长度上限）；仅当当前类型非 text 时变更，避免无谓重写
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'api_logs'
          AND column_name = 'user_agent'
          AND data_type <> 'text'
    ) THEN
        ALTER TABLE api_logs ALTER COLUMN user_agent TYPE text;
    END IF;
END $$;

-- 2) ip_address 扩容至 varchar(255) 以容纳 enc:v1: 密文；仅当当前长度不足时变更
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'api_logs'
          AND column_name = 'ip_address'
          AND (character_maximum_length IS NULL OR character_maximum_length < 255)
    ) THEN
        ALTER TABLE api_logs ALTER COLUMN ip_address TYPE varchar(255);
    END IF;
END $$;
