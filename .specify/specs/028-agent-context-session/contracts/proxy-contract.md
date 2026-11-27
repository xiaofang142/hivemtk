# Contract: Direct Provider & Platform Proxy Routing

## 1. Routing rule

- `Source=self` (or legacy empty) + local API key + tenant permission → `RouteMode=direct` by default.
- `Source=platform` without local API key → `RouteMode=platform_proxy` is mandatory.
- `direct` is a `RouteMode`, not a `Source` enum value.
- A deployment may explicitly select proxy for a self-hosted key, but the choice is auditable and tenant-scoped.

## 2. Direct path

user-server:

1. selects provider/model/protocol/region capability;
2. builds the canonical provider request;
3. sends to the configured provider endpoint;
4. records normalized usage, continuation ref and request hash;
5. stores full prompt/output only in encrypted, access-controlled ledger when required.

No platform call is made.

## 3. Proxy path

### Request headers

- `X-HiveMtk-LLM-Session-ID`: opaque canonical key; HMAC-signed.
- `X-HiveMtk-Context-Version`: context schema/policy version; HMAC-signed.
- `X-HiveMtk-Prompt-Prefix-Version`: prefix fingerprint; HMAC-signed.
- `X-HiveMtk-Request-Idempotency-Key`: request identity; HMAC-signed.
- `X-HiveMtk-Proxy-Contract-Version`: versioned endpoint/body/header contract.

The platform does not parse session mappings and does not log raw header values or request body.

### Protocol adapter

The proxy contract versions:

- endpoint path;
- upstream HTTP method;
- body schema;
- accepted/returned headers;
- usage schema.

Current fixed `/chat/completions` cannot cover Responses, Anthropic Messages or Gemini native protocols. A protocol is proxy-enabled only after the matching adapter contract is implemented and tested.

## 4. Idempotency and unknown outcomes

- If the upstream provider natively guarantees idempotent replay for the request key, the proxy may return the original response.
- Otherwise a duplicate request returns a structured `outcome_unknown` result.
- `outcome_unknown` is reconciled by user-server using the encrypted scenario/tool ledger and external receipts; it must not trigger blind replay of a native continuation or high-write tool.
- The platform must not store response bodies to implement replay.

## 5. Usage and billing

The proxy returns normalized:

- prompt tokens;
- completion/reasoning tokens;
- cached read tokens;
- cache creation/write tokens;
- provider request ID;
- finish reason/incomplete status when available.

Cached-token discounts are **not passed through by default**. Until billing supports them, proxy usage is billed at the existing input rate and dashboards must not claim cache-cost savings.

## 6. Data boundary

The platform never persists:

- conversation messages;
- customer profiles;
- leads or clues;
- shared business notes;
- raw session mappings;
- provider continuation refs;
- response bodies.

Only existing operational billing metadata and normalized usage are retained.
