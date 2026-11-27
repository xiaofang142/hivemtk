# Research: Agent Context, Session & Cache

**Date:** 2026-11-27  
**Status:** Completed — consolidated into design v0.4  
**Primary report:** `docs/research/AGENT-CONTEXT-SESSION-CACHE-2026-11-27.md`

## 1. Confirmed provider facts

| Provider / protocol | Long context / output | Continuation | Prompt cache | Implementation consequence |
|---|---|---|---|---|
| OpenAI GPT-4o | 128k context / 16,384 output | Responses `previous_response_id` or durable conversation | implicit only for this generation | Current default is not a million-token model; capability must be model-specific. [GPT-4o](https://developers.openai.com/api/docs/models/gpt-4o) |
| OpenAI GPT-5.6+ | ~1.05M context / 128k output | Responses chain/conversation | implicit + explicit; 1,024-token minimum; 30m default, optional 12h | Stable prefix and explicit cache breakpoints; cached tokens still consume context. [Prompt caching](https://developers.openai.com/api/docs/guides/prompt-caching) |
| Anthropic current 1M models | 1M context / 128k output | `thread` beta, best-effort, 5m active / 1h max | automatic or explicit `cache_control`; 20-block lookback | Native thread and prompt cache are separate; 404 requires full replay. [Context windows](https://platform.claude.com/docs/en/build-with-claude/context-windows)、[Prompt caching](https://platform.claude.com/docs/en/build-with-claude/prompt-caching) |
| Gemini 4 | 1,048,576 shared input/thinking/output; default 65,536 output | Interactions `previous_interaction_id` | implicit only for Gemini 4 / Interactions | Explicit cache is not universal; long decode has different accounting. [Gemini 4 Carbon](https://ai.google.dev/gemini-api/docs/models/gemini-4-carbon)、[Caching](https://ai.google.dev/gemini-api/docs/caching) |
| DeepSeek Responses | 1M model context; model-specific output | **stateless**; ignores unsupported `previous_response_id`/`store` | implicit disk prefix cache | Must replay full history; do not claim native continuation. [Responses API](https://api-docs.deepseek.com/guides/responses_api/) |
| Qwen Responses | ~80% of model context usable as input; model-specific output | `previous_response_id` valid 7 days; optional conversation | implicit/explicit/session-cache header depending on model | Silent truncation is possible; local budget must be stricter than advertised context. [Responses](https://help.aliyun.com/en/model-studio/qwen-api-via-openai-responses) |

## 2. Framework patterns adopted

- OpenAI Agents SDK: choose one main state strategy per conversation; do not combine local replay with server-side continuation in the same run. Session ID is not authorization. [Sessions](https://openai.github.io/openai-agents-python/sessions)
- Claude Agent SDK: sessions preserve prompt/tool/results; cross-host resume requires shared session store or application-state projection. [Sessions](https://platform.claude.com/docs/en/agent-sdk/sessions)
- LangGraph: checkpointer for thread-scoped graph state; store for cross-thread long-term facts; both require retention/pruning. [Persistence](https://docs.langchain.com/oss/python/langgraph/persistence)
- AutoGen: team state is resumable application state, not a provider session primitive. [Managing state](https://microsoft.github.io/autogen/stable/user-guide/agentchat-user-guide/tutorial/state.html)

## 3. Decisions

1. **Application remains source of truth.** Provider continuation is an optimization; every rebuild uses the latest `AgentConversationContext`.
2. **Canonical bus session key is direct.** `BusLLMSessionID` is carried end-to-end and mapped by adapters; scenario/provider rows are children of that key.
3. **Execution scenario is separate from routing scenario.** `ExecutionScenarioID` isolates Detect/Judge/ReAct even when they share `ScenarioLowCost`.
4. **Three mechanisms are independent.** Session memory, prompt cache, and final answer cache have different keys, TTLs, permissions, and metrics.
5. **Long context is capability-gated.** Unknown model/protocol/region starts conservative; no vendor-wide assumptions.
6. **Group privacy starts at input.** Audience and membership intervals filter history before prompt construction; output redaction is only a second gate.
7. **Native writes require call-time fencing.** A lease covers provider call, tools, and commit; CAS alone is insufficient.
8. **Proxy discounts default off.** Until cross-repo billing supports cached-token discounts, proxy paths do not claim cache savings.

## 4. Non-goals confirmed

- No universal `session_id` field sent to every provider.
- No platform-side persistence of conversation content, customer profiles, leads, or session mappings.
- No final-answer cache for personalized multi-turn or tool-using flows.
- No unlimited group history or automatic maximum context window.

## 5. Verification strategy

- Unit: scope, canonical ordering, budget math, audience filtering, fact classification, session isolation, disposition, tool state machine.
- PostgreSQL integration: CAS, unique indexes, blind index, tombstones, out-of-order messages, encrypted ledger, log/trace redaction.
- Fake provider contract: exact request shape for stateless/native/cache modes and usage normalization.
- Real provider/sandbox: cold/hot TTFT, cached tokens, continuation expiry/rebuild; skip is not pass.
- Golden fixtures: single-chat long-term follow-up, group multi-member context, new-member privacy, cross-account same customer, long input/output without truncation.
