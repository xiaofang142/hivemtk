-- 059: ai_agents.max_tokens 默认 800 → 2048（T1 线上验收 BUG-10）
--
-- 现象：网页客服 AI 回复在 ~200 汉字处截断。根因是创建智能体时 max_tokens
-- 落库 800 token（列默认 800，前端表单默认 800）——中文 1 token≈0.6-0.7 字，
-- 800 token ≈ 200 字，长回答必然被生成端掐断。LLM 客户端兜底默认本就是
-- 2048（internal/aiagent/llm/llm.go），把列默认与前端默认对齐到 2048。
--
-- 存量行一并抬到 2048：只调大、不改小；显式设过更小值的商户按「所见即所得」
-- 原则不动（本库当前只有验收租户两行 800）。

ALTER TABLE ai_agents ALTER COLUMN max_tokens SET DEFAULT 2048;

UPDATE ai_agents SET max_tokens = 2048 WHERE max_tokens = 800;
