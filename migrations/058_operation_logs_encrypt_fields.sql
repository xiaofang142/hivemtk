-- 058: operation_logs 敏感字段加密（OPT-SEC-04）—— 与 057_api_logs_encrypt_fields.sql 同策略
--
-- 存量数据策略说明：
--   ip / user_agent 的新写入在应用层（internal/repository/operation_log.go Create）
--   经 internal/pkg/dbencrypt 加密为 `enc:v1:{base64}` 密文。
--   存量明文行**不做批量改写**：纯 SQL 无法实现 AES-256-GCM（nonce 随机生成、
--   密钥仅存在于应用侧 MASTER_KEY，严禁下发到数据库），故保留明文、依赖读取
--   出口 Decrypt 的透传兼容（非 enc:v1: 前缀原样返回）。新写入一律加密。
--
-- 本迁移仅做字段长度保护：密文 = "enc:v1:" + base64(nonce|ciphertext|tag)，
--   - IPv6 明文最长 45 字符 → 密文约 109 字符，超原 varchar(50)；
--   - User-Agent 明文上限 255 → 密文约 380 字符，超原 varchar(255)。
-- 故两列统一放宽为 text（无上限），与 model.OperationLog 的 `type:text` 标签严格对齐。
-- 全部幂等写法：仅当当前类型非 text 时变更，避免无谓重写。

-- 1) ip：varchar(50) → text（仅当当前类型非 text 时变更）
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'operation_logs'
          AND column_name = 'ip'
          AND data_type <> 'text'
    ) THEN
        ALTER TABLE operation_logs ALTER COLUMN ip TYPE text;
    END IF;
END $$;

-- 2) user_agent：varchar(255) → text（仅当当前类型非 text 时变更）
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'operation_logs'
          AND column_name = 'user_agent'
          AND data_type <> 'text'
    ) THEN
        ALTER TABLE operation_logs ALTER COLUMN user_agent TYPE text;
    END IF;
END $$;
