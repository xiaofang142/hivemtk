#!/usr/bin/env python3
"""把 hardcode-sweep.json 的 CONFIG 原始命中做「真/假阳性」切分 + 三分归类。

上一轮扫描器只认 `Ident = 数字` 形态，会把结构体字面量字段（Confidence=0.95、
Enabled=true）、map 键、JSON 字段（"timeout": 30 之外的赋值）一并算进 CONFIG。
本脚本用轻量词法把命中分成：

  CONST_DECL —— 真·声明型常量（const(...) 块 / const X= / var X= / 小写带类型）
                → 才可能进参数中心
  FIELD_LIT  —— 结构体字面量字段（Confidence= / Enabled= / Score= / Limit= ...）
                → 业务数据，不是阈值
  ELSE       —— 其他（map 键、比较右值、函数实参等）

再对 CONST_DECL 做四分归类：
  TUNABLE   阈值/上限/超时/批量/TTL/权重/开关 —— 应入库（config_params）
  PROTOCOL  错误码、状态枚举、协议版本、十六进制长度 —— 不迁
  DEPLOY    端口、连接池、DB 主机 —— 部署配置（env/yaml），不进参数中心
  INFRA     算法参数（HNSW 维度、embedding 维度）—— 需重建索引，单独通道
"""

from __future__ import annotations

import argparse
import json
import re
from collections import Counter, defaultdict
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent
SERVER = REPO_ROOT / "user-server"

PROTOCOL_HINT = re.compile(
    r"^(Code|ProtocolVersion|Status|Type|SpanIDHexLen|TraceIDHexLen|RDays|Version|MFAEnabled$)"
)
DEPLOY_HINT = re.compile(
    r"(Port|Ports$|MaxOpenConns|MaxIdleConns|ConnLifetime|Dsn|DSN|Host$|SSL$|ImplicitTLSPort|"
    r"DBPort|LLMPort|LayaPort|EmbeddingPort|RerankPort|POSTGRES_TEST_PORT|AGENT_RUNTIME_BUS)"
)
INFRA_HINT = re.compile(r"(HNSW|Dimension|TopLogprobs|SPLICE|SourceDim|BGEM3|Vector)")

# 结构体字面量字段的典型名字（跨项目共性）
FIELD_NAMES = {
    "Enabled", "Confidence", "Score", "Limit", "Size", "Status", "Priority", "Quantity",
    "AttemptCount", "SuccessCount", "ConsecutiveFailures", "UnreadCount", "Page", "PageSize",
    "Depth", "MaxElapsedTime", "MaxHistoryMessages", "Remaining", "DailyRemain", "Accepted",
    "Active", "Allowed", "Authorized", "Blocked", "Configured", "Duplicate", "Found", "Hit",
    "BuiltIn", "Strict", "Deferred", "Degraded", "IsActive", "IsFinal", "IsGroup", "IsRead",
    "IsSend", "IsSuccess", "IsRetryable", "IsCanary", "IsControl", "IsDefault", "IsFavorite",
    "IsWinner", "IsOfficial", "IsPrimary", "IsAIReply", "IsBottleneck", "Capabilities",
    "Passed", "Success", "Retryable", "ActionMatchRate", "AnswerRelevancy", "CtxRelev",
    "Faithfulness", "Recall", "ClickRate", "OpenRate", "DeliveryRate", "FailureRate", "DropRate",
    "ChunkCount", "DocCount", "SearchCount", "TotalCustomers", "AvgDealAmount", "BudgetRAG",
    "BudgetUsed", "ExecutionCount", "ChromeTabID", "ReplyToMessageID", "TotalTimeout",
    "LoopCount", "Failed", "Skipped", "SkippedByStage", "SkippedDisabled", "ActionSkipped",
    "AuditLogged", "AuditWritten", "Assembled", "Polished", "TriggeredAI", "EmailDispatched",
    "Transited", "Transferred", "TransferredToHuman", "HandoffToHuman", "HumanLocked",
    "QueuedForAI", "Ordered", "Visible", "Truncated", "ShouldAlert", "ShouldForceMFA",
    "DependencyUnmet", "SkipLLM", "DryRun", "ProbeMissing", "Ordered", "Realtime",
    "L1WindowSize", "LoopCount", "Alpha", "Beta", "Length", "Visibility", "UseCount",
    "LoopCount", "OpportunityWinProbabilityMax", "MinReward", "Composite", "Capacity",
    "Skills", "QualityScore", "StepRate", "Rate", "OverallScore", "Circle", "Semantic",
}

# 明显是业务数据/条件字段的一律归 FIELD_LIT
FIELD_REGEX = re.compile(
    r"^(Enabled|Confidence|Score|Limit|Size|Status|Priority|Quantity|AttemptCount|SuccessCount|"
    r"UnreadCount|Page|PageSize|Depth|Remaining|DailyRemain|Accepted|Active|Allowed|Authorized|"
    r"Blocked|Configured|Duplicate|Found|Hit|BuiltIn|Strict|Deferred|Degraded|Is[A-Z]|"
    r"Has[A-Z]|Passed|Success|Retryable|Action[A-Z]|Answer[A-Z]|Ctx[A-Z]|Faithfulness|Recall|"
    r"ClickRate|OpenRate|DeliveryRate|FailureRate|DropRate|ChunkCount|DocCount|SearchCount|"
    r"TotalCustomers|AvgDealAmount|Budget[A-Z]|ExecutionCount|ChromeTabID|ReplyToMessageID|"
    r"TotalTimeout|LoopCount|Failed|Skipped|SkippedByStage|SkippedDisabled|Audit[A-Z]|"
    r"Assembled|Polished|TriggeredAI|EmailDispatched|Transited|Transferred|HandoffToHuman|"
    r"HumanLocked|QueuedForAI|Truncated|Should[A-Z]|DependencyUnmet|SkipLLM|DryRun|ProbeMissing|"
    r"Realtime|L1WindowSize|Alpha|Beta|Length|Visibility|UseCount|Opportunity[A-Z]|MinReward|"
    r"Composite|Capacity|Skills|QualityScore|StepRate|Rate|OverallScore|SmlistType|"
    r"(Account|Order|User)StatusType|CrisisLevel|EmailStatus[A-Z]|"
    r"WouldDeny|AutoApproved|Initialized$|CacheHit$|Intent$|ID$|Bill$|Quote$|Outreach$|"
    r"Opportunity$|Payment$|AnswerRelevancy|StripFewShotJSON|Disable[A-Z]|Enable[A-Z]|"
    r"LazyQuotes|MFAEnabled|WebhookEnabled|JSONMode|Logprobs|RERANK_ENABLED|"
    r"EMBEDDING_ALLOW_FALLBACK|AGENT_RUNTIME_BUS_ENABLED|PLATFORM_ENABLED|MTK_HUMANIZE|"
    r"QuietHoursEnabled|HumanizeEvaluatorEnabled|HTTPIngest[A-Z]|RERANK|REQUIRE_PRIVATE)"
)


def bucket(row: dict) -> tuple[str, str]:
    name, value, kind = row["name"], row["value"], row["kind"]
    if name in FIELD_NAMES or FIELD_REGEX.match(name):
        return "FIELD_LIT", "业务数据字段/条件值，非阈值"
    if PROTOCOL_HINT.match(name):
        return "PROTOCOL", "协议/错误码/枚举常量"
    if DEPLOY_HINT.search(name):
        return "DEPLOY", "部署配置（env/yaml），不进参数中心"
    if INFRA_HINT.search(name):
        return "INFRA", "检索/向量维度参数，改动需重建索引"
    if kind == "bool":
        return "TUNABLE", "功能开关，应入 feature_flags 或 config_params"
    return "TUNABLE", "阈值/上限/超时/批量，应入 config_params"


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--sweep", default=str(REPO_ROOT / "hardcode-sweep" / "hardcode-sweep.json"))
    ap.add_argument("--out", default=str(REPO_ROOT / "hardcode-sweep" / "hardcode-sweep-classified.json"))
    args = ap.parse_args()
    data = json.load(open(args.sweep, encoding="utf-8"))
    rows = []
    for r in data["config_consts"]:
        bucket_id, why = bucket(r)
        rows.append({**r, "bucket": bucket_id, "why": why})
    c = Counter(r["bucket"] for r in rows)
    by_dir = defaultdict(Counter)
    for r in rows:
        parts = r["file"].split("/")
        by_dir["/".join(parts[1:3]) if len(parts) > 3 else parts[-1]][r["bucket"]] += 1
    out = {"summary": dict(c), "rows": rows}
    Path(args.out).write_text(json.dumps(out, ensure_ascii=False, indent=2), encoding="utf-8")
    print(json.dumps(dict(c), ensure_ascii=False, indent=2))
    print("\n=== TUNABLE 按目录 top30 ===")
    for k, v in sorted(by_dir.items(), key=lambda x: -x[1]["TUNABLE"])[:30]:
        print(f"{v['TUNABLE']:4d} tun / {v['FIELD_LIT']:3d} fld / {v['PROTOCOL']:3d} pro  {k}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())