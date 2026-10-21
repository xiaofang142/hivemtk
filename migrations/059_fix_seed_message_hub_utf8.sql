-- =============================================================================
-- Migration: 059_fix_seed_message_hub_utf8.sql
-- Description: Idempotent fix for seed-hub-conv- messages corrupted by PowerShell pipe encoding
-- Author: @debugger
-- Date: 2026-10-21
-- =============================================================================

BEGIN;

UPDATE message_hub
SET 
    receiver_name = '客服坐席',
    content = '您好，想咨询一下贵公司专业私域 AI 营销矩阵的部署方案 (' || sender_name || ' · ' || platform || ') [seed-demo]'
WHERE msg_id LIKE 'seed-hub-conv-%-1';

UPDATE message_hub
SET 
    sender_name = 'AI主管',
    content = '您好，HiveMtk 支持本地化私有部署，包含蜂桥接入、ReAct 智能体与 1024 维知识库，稍后为您提供详细方案 (' || receiver_name || ' · ' || platform || ') [seed-demo]'
WHERE msg_id LIKE 'seed-hub-conv-%-2';

UPDATE message_hub
SET 
    receiver_name = '客服坐席',
    content = '好的，已经收到，预约演示 (' || sender_name || ' · ' || platform || ') [seed-demo]'
WHERE msg_id LIKE 'seed-hub-conv-%-3';

UPDATE inbox_conversations ic
SET 
    last_message_preview = mh.content,
    last_message_at = mh.sent_at
FROM message_hub mh
WHERE mh.msg_id = 'seed-hub-conv-' || ic.conversation_id || '-3'
  AND ic.conversation_id IN ('seed-conv-9','seed-conv-10','seed-conv-11','seed-conv-12','seed-conv-13','seed-conv-14','seed-conv-15');

COMMIT;
