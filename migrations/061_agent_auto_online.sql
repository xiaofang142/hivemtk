-- 061: 坐席「回复时自动上线」独立记账（J-13 选项 A）
--
-- 背景：新租户坐席默认 offline，管理员点「回复访客」被「坐席已离线」挡住；
-- 上一轮修法是把离线坐席直接置 online，但多坐席团队里「真离线的人点了
-- 一次回复」会污染在线率与派单统计。
--
-- 语义：Reply 触发的接管放行离线坐席，但只写 auto_online_at/source，
-- **不改 status**；看板与派单继续按 status（真在线）统计，管理端可显示
-- 「回复时自动上线」提示。AI 不抢答由「会话已是 human 且有人工回复」保证。
ALTER TABLE agent_statuses ADD COLUMN IF NOT EXISTS auto_online_at timestamptz;
ALTER TABLE agent_statuses ADD COLUMN IF NOT EXISTS auto_online_source varchar(32) NOT NULL DEFAULT '';
