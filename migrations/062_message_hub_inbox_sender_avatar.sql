-- 062: M-2 客户头像链路存储半项（2026-11-11）
--
-- 背景：桥端昵称链路早已完备（sender_name 全链路落库），头像零实现——
-- content script 不提取、协议无字段、库表无列、管理台无处展示。本迁移补齐库表：
--   - message_hub.sender_avatar        消息行：这条消息发送者的头像 URL
--   - inbox_conversations.sender_avatar 会话行：客户头像（随 last message upsert
--     按「新值优先、空值保旧」刷新，与 customer_name 同语义）
--
-- 列宽 varchar(500)：与 media_url 同款（model.MaxMediaURLLen，PG 按字符计数），
-- 平台签名头像 URL（抖音 p3-sign…&x-expires=… 等）同样会超 500 字符，
-- 入库前由应用层 model.TruncateMediaURL 统一收口。
--
-- 无回填：头像只可能由本迁移之后的上行产生，历史行 sender_avatar 恒为空——
-- 读侧（管理台）空头像降级为既有默认形态，不报错。
--
-- 注意：生产 schema 事实源是 user-server AutoMigrate（模型字段已同步加列）；
-- 本文件供离线/人工执行路径使用，幂等可重入。
ALTER TABLE message_hub ADD COLUMN IF NOT EXISTS sender_avatar varchar(500);
ALTER TABLE inbox_conversations ADD COLUMN IF NOT EXISTS sender_avatar varchar(500);
