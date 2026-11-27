# Quickstart: Agent Context, Session & Cache

## Prerequisites

- Go toolchain used by `user-server/go.mod`.
- PostgreSQL test database on `127.0.0.1:8217`.
- `POSTGRES_TEST_PASSWORD` set. Missing password or skipped DB tests are not a pass.
- Provider credentials only for real-provider performance checks; fake provider tests run without network.

## Core commands

Run from `hivemtk-user/`:

```bash
gofmt -w user-server/internal/aiagent/context user-server/internal/model user-server/internal/repository user-server/internal/service user-server/internal/aiagent/llm
go build ./user-server/...
go vet ./user-server/...
go test ./user-server/internal/aiagent/context/...
go test ./user-server/internal/aiagent/llm/...
go test ./user-server/internal/service/...
```

For a focused DB test:

```bash
POSTGRES_TEST_PASSWORD=... go test ./user-server/internal/repository -run 'AgentContext|LLMSession|ConversationMessageDisposition|ToolInvocation' -count=1
```

## Golden scenarios

### Single chat

1. Turn 1: customer states industry, budget and objection.
2. Turn 2: customer corrects the product entity.
3. Turn 3: customer says “就按上次说的”.
4. Assert intent, sales reply and humanize evaluation use the corrected entity and cross-Session MessageHub history.
5. Assert another conversation/tenant/account cannot see the facts.

### Group chat

1. A mentions a product.
2. B adds budget.
3. C asks delivery time.
4. Assert current sender, member roles and real MessageIDs are preserved.
5. Add a new member after private messages; assert pre-join private messages never enter model input.
6. Assert unrelated chatter does not become the current intent and member PII is not emitted.

### Session/cache

1. Use a fake provider to assert stateless/native request shapes and `ExecutionScenarioID` isolation.
2. Use a real provider/sandbox to assert continuation, cached tokens and cold/hot TTFT.
3. Assert `Source=platform` without local key uses proxy; `Source=self` with key uses direct by default.
4. Assert duplicate unknown outcomes reconcile through ledger without storing proxy response bodies.

## Observability checks

- Actual provider request prefix hash is stable across identical fixtures.
- Session reuse, cache hit and answer cache hit are separate metrics.
- Logs/traces contain only IDs/hashes for customer conversation content.
- Group privacy guard and audience filter both produce evidence.
- Long input/output fixtures do not silently truncate.

## Rollback

- Disable native session per provider/model/protocol/region; system falls back to stateless replay.
- Disable long-context scenario flags; system returns to summary/small-window mode.
- Disable proxy route for self-hosted keys; `Source=platform` remains mandatory proxy.
- Keep final answer cache eligibility unchanged during rollback.
