# Plan

## Architecture and constitution gate

Use existing Go/Gin/GORM/PostgreSQL with Router → Controller → Service → Repository → Model. Commerce DTO and injectable Read/Preflight/Submit/Observe service ports. No new service, infrastructure, agent or production provider. Tests use explicit injected adapters. Existing migration v3.106.0 immutable; commerce v3.107.0 agreed with coordinator (G3 v3.108.0). Shared initial_schema and module_tables edits are additive.

## Data model

CommerceOperation links external_order ID and optional after_sale/order_change IDs, stores canonical parameter hash, intent/revision, state/native state, route, preflight/risk/policy and connection versions, owner/approval and due/lease/fencing/dispatch metadata. CommerceApproval stores immutable digest/actor/deadline/role revision. CommerceAttempt records each submit/observe dispatch and classified outcome. CommerceResource is a tenant/platform/shop/order guard with active operation, currency/reserved amount/quantity and revision. CommerceOutbox is transactional typed event/task envelope. CommercePolicy/CommerceMember establish server-managed shop roles/limit/stop-write/recovery epoch and single write owner. No HTTP request may manufacture role, credential or success evidence.

## Files and contracts

New user-server/internal/{model,repository,service,controller,router,dto}/commerce*.go. Existing model/integration.go gains optional shop/source/buyer/version metadata without changing old mirror key semantics. Repository queries always explicit tenant, including actor/policy/linked order/case lookup. All transitions under short transactions and row locks; Service supplies validated decisions, Repository owns atomic persistence/CAS. SQL/GORM only in repository and migration. API /api/v1/commerce/profiles, orders, operations and operation subresources preflight/approve/submit/observe/cancel/claim/evidence; shop policy/members managed by authenticated current administrators and checked again from DB. Response envelope code=0 means local API acceptance only. Worker entry service RunDue(ctx, limit) may also be triggered by observe endpoint; no hidden network in router.

## Reliability and platform design

Freeze current snapshot/obligations and approved route. Before dispatch acquire resource+connection/policy/member locks, verify current guards and reserve balance/quantity. Dispatch commit occurs before network; unknown holds resource. Leases fence obsolete worker writes; recovery outbox routes to Observe. Authoritative Read/Observe verifies address digest/customer review, refund transaction+currency+minor amount and funds state, or action-specific return/exchange states. Official workbench has named steps, operator claim and separately reviewed manual attestation, never platform_verified from client input. Managed Submit records observation only, never provider Submit. ERP transport uses frozen write owner and observes platform authority separately. Profiles expose declared workflow with pending_live_validation; no provider means API unavailable.

## Validation and sequencing

First compile and integration fixture vertical slice, then platform contract matrix, then negative/fault coverage. Dedicated test DB 127.0.0.1:8217/user_db_test; read container env without printing secrets, run authentication preflight first. Required full go build/vet and targeted -race repository/service/controller/router/migration checks plus architecture gates. Keep verification and convergence findings inside tasks.md. User requests exactly spec/plan/tasks; checklist/research/contracts/quickstart content consolidated here and in source DTOs to avoid extra documents. No hooks configured in extensions.yml.
