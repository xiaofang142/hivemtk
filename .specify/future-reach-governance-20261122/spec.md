# Future Reach Governance

Created: 2026-11-22. Status: implementing. Source: outer docs/未来任务清单.md M22–32, RV04/06/08/09/11 and BR01/02/04/06/10. User authorizes autonomous local implementation and simulated provider signals; production release and live evidence remain separate.

## User stories and acceptance

1. An operator sends through the existing ReachSendPipeline. A timeout after submission produces outcome_unknown, one provider call, no fallback and no automatic retry. A provider-confirmed rejection may be retried under budget and Retry-After. Each retry and approved fallback rechecks current consent, DNC, permissions, policy, and channel-specific eligibility.
2. A merchant saves purpose/brand/outlet evidence without enlarging old grants. Unproven imported leads remain ineligible for marketing. Explicit withdrawal and identity merge preserve stricter decisions. Necessary support is separate from marketing.
3. An operator reviews durable campaign audience assignments, immutable approved content, bounded DAG execution, absolute wait deadlines, pause/exit and current consent. Control groups never dispatch marketing. Unknown submissions retain pressure and cost reservations.
4. Business notifications cite authoritative events and use the same pipeline; closed windows create internal reminders. Provider task creation, member confirmation, acceptance, delivery and read remain distinct.
5. Finance rebuilds paid-minus-refunded totals by currency from immutable authoritative facts, including idempotent corrections. Draft orders, clicks, unknown refunds and task creation never count as revenue/delivery.
6. Recovery preserves historical keys, quarantines new side effects and incomplete consent/tombstones, and permits read-only reconciliation. Amazon reply ownership remains frozen unless observed native configuration and in-flight boundaries permit handoff. Widget identity requires server assertions; asset installation requires trusted signatures and bounded safe extraction.

## Requirements

FR-001 Existing send interfaces are reused; no second sender or default simulated success.
FR-002 Unknown network outcomes never automatically retry or switch outlet. Only explicit pre-dispatch/provider-rejection evidence permits retry.
FR-003 Typed errors and actual attempt counts survive reporting and reservation settlement.
FR-004 Current channel-specific permission, consent, DNC and policy are rechecked for every actual provider attempt.
FR-005 DryRun branches before any production sender or reservation side effect.
FR-006 Persistent governance extends existing consent, marketing-flow, quota and attribution objects; tenantless and cross-tenant paths fail closed.
FR-007 Approval/content/identity/pause/recovery versions and idempotency keys bind persisted work.
FR-008 Simulation and live validation are reported separately; missing provider authorization remains pending_live_validation.

## Success criteria

SC-001 Timeout/unknown/fallback adversarial tests observe exactly one primary call and zero backup calls.
SC-002 Withdrawal between attempts results in zero further provider calls.
SC-003 Durable restart and duplicate event tests do not repeat completed effects or double-count money.
SC-004 Each claimed EC has current implementation and normal/negative/fault evidence; incomplete EC rows remain open.
