# intent_records 与 intent_logs 整合方案（OPT-DB-10）

> **整合时间**：二期（建议 v3.20.0）  
> **本期**：建立过渡视图 `intent_view`，统一查询入口
> **风险评估**：LOW（两表结构相似，废弃其中一张即可）

---

## 一、当前现状

| 表 | 模型位置 | 用途 | 写入方 |
|---|---|---|---|
| `intent_records` | [model/intent_log.go](../../user-server/internal/model/intent_log.go) | AI 销冠意图识别结果 | sales_engine |
| `intent_logs` | [model/intent_log.go](../../user-server/internal/model/intent_log.go) | 智能体意图分类日志 | smart_cs_orchestrator |

### 结构对比

| 字段 | intent_records | intent_logs |
|------|----------------|-------------|
| ID | BIGSERIAL | BIGSERIAL |
| SessionID | VARCHAR(50) | VARCHAR(50) |
| CustomerID | VARCHAR(64) | VARCHAR(64) |
| IntentType | VARCHAR(50) | VARCHAR(50) |
| IntentSubtype | VARCHAR(50) | VARCHAR(50) |
| Confidence | DECIMAL(4,3) | DECIMAL(4,3) |
| Method | VARCHAR(20) | VARCHAR(20) |
| CreatedAt | TIMESTAMP | TIMESTAMP |

**结论**：两表结构 95% 相同，**完全可以合并为一张表**。

---

## 二、整合方案

### 2.1 阶段一（本期）：视图兼容层

```sql
-- 045_intent_unify_view.sql
CREATE OR REPLACE VIEW intent_view AS
  SELECT 
    'records' AS source,
    id, session_id, customer_id,
    intent_type, intent_subtype, confidence, method, created_at
  FROM intent_records
  UNION ALL
  SELECT 
    'logs' AS source,
    id, session_id, customer_id,
    intent_type, intent_subtype, confidence, method, created_at
  FROM intent_logs;
```

应用层查询改用 `intent_view`：
```go
db.Table("intent_view").Where("customer_id = ?", cid).Find(&results)
```

### 2.2 阶段二（二期）：废弃 intent_logs

```sql
-- 046_drop_intent_logs.sql
BEGIN;
  -- 1) 迁移数据
  INSERT INTO intent_records (session_id, customer_id, intent_type, intent_subtype, confidence, method, created_at)
    SELECT session_id, customer_id, intent_type, intent_subtype, confidence, method, created_at
    FROM intent_logs
    ON CONFLICT (id) DO NOTHING;
  
  -- 2) 改写应用层，让所有写入都走 intent_records
  -- 3) 停服 5 分钟
  -- 4) 验证数据完整性
  SELECT COUNT(*) FROM intent_records; -- 应等于原两表之和
  
  -- 5) 删除 intent_logs
  DROP TABLE intent_logs;
COMMIT;
```

### 2.3 应用层变更清单

| 文件 | 变更 |
|---|---|
| [model/intent_log.go](../../user-server/internal/model/intent_log.go) | 增加 source 字段 (enum: records/logs) |
| `service/intent_recognition.go` | 写入统一改到 intent_records，读取用 view |
| `service/intent_recognition_fine.go` | 同上 |
| `service/smart_cs_orchestrator.go` | 同上 |

---

## 三、回滚方案

```sql
-- 阶段一回滚
DROP VIEW IF EXISTS intent_view;

-- 阶段二回滚（在 DROP TABLE 前）
CREATE TABLE intent_logs AS SELECT * FROM intent_records WHERE source = 'logs';
```

---

## 四、为什么本期不直接合并

| 原因 | 说明 |
|---|---|
| 数据回写风险 | 两表历史数据可能存在 ID 冲突，需要业务确认 |
| 跨表事务 | sales_engine 与 smart_cs_orchestrator 共用事务时锁定顺序需重新设计 |
| 索引优化 | 合并后索引策略需重新评估 |
| 二期更安全 | 一次性合并比渐进式更稳定 |

---

## 五、修订历史

| 版本 | 日期 | 修订人 | 内容 |
|------|------|--------|------|
| v1.0 | 2026-08-16 | audit-agent | 初版（OPT-DB-10 实施） |
