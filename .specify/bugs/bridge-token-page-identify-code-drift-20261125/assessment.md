# Bug Assessment: retired Bridge token page still routes administrators to dead token workflow

- **Slug**: `bridge-token-page-identify-code-drift-20261125`
- **Created**: 2026-11-25
- **Source**: pasted text / source review request
- **Verdict**: valid
- **Severity**: medium
- **Lane**: Bug 车道（`docs/standards/SPEC-KIT-WORKFLOW.md` §2；assess-only）

## Report (summarized)

Review `user-web/src/views/bridge/TokenManagement.vue`, backend `internal/service/bridge_token.go`, related router/controller/API, identify_code management page and tests. Determine whether the UI still exposes retired token status/reset/grace-window behavior, whether it conflicts with identify_code-only, whether `onReset` lacks error handling, and which identify_code management capability should replace it.

## Symptom

The old `/bridge/token` page still renders a Bridge-token form and calls retired token APIs even though the backend has switched to identify_code-only. Administrators can see misleading “new credential / rotate / PREV grace window” UI; the rotate action always returns HTTP 410 and the page has no local error recovery or navigation to the real management page.

Expected: old deep links should land on `/channels/identify-codes`, and no retired token status/reset API should be called from the management UI.

## Reproduction

Source-proven; browser/runtime reproduction was not run because this lane is assess-only and browser automation was explicitly prohibited.

1. Open `/bridge/token`, `/bridge`, or `/token` as an administrator.
2. `onMounted(loadStatus)` calls `GET /api/bridge/token/status`.
3. Backend returns `retired:true`, `token:""`, `prev_configured:false`, and an identify-code count.
4. The page still renders the token input, “生成新凭证(轮换)” button and legacy PREV/grace-window copy.
5. Clicking the button calls `POST /api/bridge/token/reset`; backend returns HTTP 410 `ErrLegacyBridgeTokenRetired`.
6. `onReset` has `try/finally` but no `catch`; the rejection is not handled locally and the user is not routed to identify-code management.

## Suspected Code Paths

### Retired UI/API path

- `user-web/src/views/bridge/TokenManagement.vue:6-12` — legacy “桥接通道凭证” title and token-oriented configured/unconfigured status.
- `user-web/src/views/bridge/TokenManagement.vue:16-23` — PREV grace-window alert, although backend always returns `prev_configured:false`.
- `user-web/src/views/bridge/TokenManagement.vue:33-58` — token plaintext field, copy, rotate button, and “旧凭据进入灰度窗口” helper text.
- `user-web/src/views/bridge/TokenManagement.vue:68` — imports retired `getBridgeTokenStatus` / `resetBridgeToken`.
- `user-web/src/views/bridge/TokenManagement.vue:75-80` — maps `s.token` into the token field; load errors only `console.warn`.
- `user-web/src/views/bridge/TokenManagement.vue:83-93` — `onReset` lacks `catch`; success toast claims token rotation even though production reset cannot succeed.
- `user-web/src/api/bridgeToken.js:3-5` — legacy status/reset endpoints remain exposed to the SPA.
- `user-web/src/router/modules/bridgeToken.js:3-18` — `/bridge`, `/bridge/token`, `/token` still resolve to the legacy page; `/bridge/extension` shares the module and must not be redirected accidentally.
- `user-web/src/config/onboarding/pageTours.ts:447-465` — old route still has a token-specific tour.
- `user-web/src/i18n/locales/zh.json:2107-2112` — onboarding summary still teaches token generation and PREV grace window; other locale files carry the same stale key family.

### Backend retirement path

- `user-server/internal/service/bridge_token.go:10-18` — explicitly marks Bridge token retired and directs users to identify-code management.
- `user-server/internal/service/bridge_token.go:29-45` — status returns `retired:true`, empty token, `prev_configured:false`; identify-code count is mapped to `main_configured`.
- `user-server/internal/service/bridge_token.go:49-50` — reset always returns `ErrLegacyBridgeTokenRetired`.
- `user-server/internal/controller/bridge_token.go:15-16,29,40` — comments/Swagger still describe plaintext token display and PREV rotation.
- `user-server/internal/controller/bridge_token.go:45-55` — reset maps retirement to HTTP 410.
- `user-server/internal/router/router.go:456-458` — legacy routes remain admin-authenticated but are registered before the group-wide `TenantMiddleware`.
- `user-server/internal/repository/channel_identify_code.go:99-125` — `ValidCodes` uses `SystemScope`; legacy status therefore counts identify codes globally and swallows read errors in `BridgeTokenService.Status`.

### Correct identify_code capability

- `user-web/src/router/modules/channels.js:14-18` — `/channels/identify-codes`, route name `ChannelIdentifyCodeManage`.
- `user-web/src/views/channels/identify-code-manage.vue:6-12` — correct identify-code management page and legitimate short in-flight-window warning.
- `user-web/src/api/channelIdentifyCode.js:5-15` — `GET /api/identify-codes`, `GET /api/identify-codes/:id`, `POST /api/identify-codes/:id/rotate`.
- `user-server/internal/router/channel_routes.go:39-42` — identify-code list/get/rotate routes.
- `user-server/internal/service/channel_identify.go:117-158` — canonical generate/rotate implementation; old **identify code** may enter a short in-flight window, default 60s.
- `user-server/internal/model/menu.go:526-527` — menu key `channelIdentifyCode`, path `/channels/identify-codes`; old Bridge token menu entry was intentionally removed.
- `user-server/internal/model/menu_capability_routes.go:231-233` — API capability mapping for identify-code management.
- `docs/architecture/BRIDGE_TOKEN_PROTOCOL.md:52-59` — confirms the short grace window belongs to identify-code rotation, not retired Bridge tokens.

### Request/error behavior

- `user-web/src/utils/request.js:427-480` — non-401/403/404/429/5xx statuses use the generic server-message toast; HTTP 410 can show the backend retirement message, but the caller still receives a rejected promise.
- `user-web/src/utils/request.js:407-492` — interceptors toast and reject; they do not provide page-level recovery or navigation.

### Existing tests

- No `TokenManagement.vue`, `bridgeToken.js`, `BridgeTokenService`, or `ChannelIdentifyService` unit tests were found.
- `user-web/tests/unit/onboarding_pageTours.test.js:160` only records the old route-to-component mapping; it does not assert retirement behavior.
- Backend search found no focused tests for `ErrLegacyBridgeTokenRetired`, `/api/bridge/token/reset`, or identify-code create/rotate behavior.

## Root Cause Hypothesis

**High confidence.** T058/T063 retired the Bridge-token backend and removed its menu entry, but the legacy SPA route/component/API and onboarding content were not removed or redirected. The frontend still assumes the pre-retirement token contract. `onReset` compounds the drift by omitting local error handling, so the guaranteed 410 response becomes an unhandled rejection instead of an actionable transition to identify-code management.

A secondary backend drift remains: the compatibility status endpoint maps identify-code count to token `main_configured`, counts globally via `SystemScope`, swallows `ValidCodes` errors, and its controller comments still document PREV rotation. The UI redirect is sufficient to stop normal users from this path, but direct API consumers still see the legacy compatibility surface.

## User-visible impact

- Administrators following old bookmarks land on a dead credential workflow instead of channel identify-code management.
- The page can say “已配置(运行时)” when only identify codes exist, while the “new credential” field is necessarily empty.
- Users are told to rotate a Bridge token and wait for a PREV grace window that no longer exists.
- The rotate button always fails with 410; there is no page-level message explaining the next step and no navigation to the correct page.
- `onReset` produces an unhandled rejected promise; `loadStatus` hides failures behind `console.warn`.
- Onboarding copy continues to teach retired token behavior.
- No authentication bypass was found in the UI path. Direct legacy status calls can expose a global identify-code count to an admin because the route lacks group-wide tenant binding; this is an adjacent API-scope issue, not a reason to keep the UI.

## Correct management capability

The replacement is the existing **channel identify-code management** capability:

- Route: `/channels/identify-codes`
- Route name: `ChannelIdentifyCodeManage`
- Component: `user-web/src/views/channels/identify-code-manage.vue`
- APIs: `/api/identify-codes`, `/api/identify-codes/:channel_id`, `/api/identify-codes/:channel_id/rotate`
- Backend: `ChannelIdentifyService.CreateOrRotate`
- Menu key: `channelIdentifyCode`

The identify-code page's “旧码仅在短暂在途窗口内有效” copy is valid and must not be removed. It is not the retired Bridge-token PREV window.

## Proposed Remediation

**Preferred**: make the old routes redirect to the existing identify-code management page and stop rendering/calling the legacy token component.

1. In `user-web/src/router/modules/bridgeToken.js`, redirect `/bridge`, `/bridge/token`, and `/token` to `/channels/identify-codes`; remove the `TokenManagement.vue` component import from the route module. Keep `/bridge/extension` unchanged.
2. Preserve the old admin-only boundary. Because `/bridge/token` currently has `requiresAdmin:true` but `/channels/identify-codes` only has `requiresAuth:true`, add/preserve an explicit admin guard on the resolved identify-code route or redirect chain; do not accidentally broaden access.
3. Remove the stale `/bridge/token` onboarding tour or let the redirect make it unreachable, and clean the obsolete `bridgeToken` onboarding copy in a follow-up if locale churn is undesirable.
4. Harden the destination page's rotate/generate error path: success toast only after API success; distinguish confirmation cancel from API failure; reset submitting/selection state on both; no unhandled rejection. This prevents the redirect from merely moving the user to another weak error path.
5. Keep backend retirement behavior unchanged in the UI fix. Update stale controller Swagger/comments separately. If legacy status/reset endpoints remain for compatibility, add focused tests for `retired:true`, empty token, `prev_configured:false`, and reset 410.

**Alternative**: keep `/bridge/token` as a read-only retirement notice with a router link/button to `/channels/identify-codes`, removing all status/reset calls and token form controls. This preserves the old URL but leaves more surface area than a redirect.

**Files likely to change** (minimum behavioral fix):

- `user-web/src/router/modules/bridgeToken.js`
- `user-web/src/router/modules/channels.js`
- `user-web/src/views/channels/identify-code-manage.vue` (destination error handling)
- `user-web/tests/unit/` new router/component regression tests
- Optional cleanup: `user-web/src/views/bridge/TokenManagement.vue`, `user-web/src/api/bridgeToken.js`, `user-web/src/config/onboarding/pageTours.ts`, locale `bridgeToken` copy, `router-list.md` / route inventory docs

**Backend files only if compatibility endpoints are retained/tested**:

- `user-server/internal/controller/bridge_token.go` comments/tests
- `user-server/internal/service/bridge_token.go` status error/tenant-scope decision
- `user-server/internal/router/router.go` route placement/tenant scope decision

## Tests to add or update

### Automated

1. Router redirect test: `/bridge`, `/bridge/token`, `/token` resolve to `/channels/identify-codes`; `/bridge/extension` still resolves to `BridgeExtensionPackage`.
2. Router guard test: non-admin cannot reach the redirected management capability; admin can.
3. Legacy-call guard test: navigating old URLs does not call `/api/bridge/token/status` or `/api/bridge/token/reset` and does not mount `TokenManagement.vue`.
4. Destination component test: list/load success and failure; rotate success shows the new code exactly once; rotate failure shows an error, never a success toast, clears submitting state, and leaves the list coherent.
5. Confirmation test: cancel performs no rotate request; API failure is not swallowed as cancellation.
6. Clipboard test: identify-code copy success/failure paths remain intact.
7. Backend compatibility tests if endpoints remain: status is retired with no token/PREV; reset returns 410 with stable machine-readable reason; status DB errors are not reported as “not configured.”

### 逐元素 UI acceptance checklist (not executed in this assess-only lane)

1. As admin, explicitly navigate to `/bridge/token`; confirm URL becomes `/channels/identify-codes` and the page title is “渠道识别码管理”.
2. Confirm no request is made to `/api/bridge/token/status`; no “桥接通道凭证 / 新凭证 / 生成新凭证(轮换) / PREV 灰度窗口” text remains.
3. Repeat direct navigation for `/bridge` and `/token`; all land on identify-code management. `/bridge/extension` remains unchanged.
4. As non-admin, open each old deep link; confirm 403/NotFound and no legacy API call.
5. On identify-code management, inspect the channel selector; choose one channel and explicitly click “生成识别码”; confirm one rotate request, success code, and refreshed list.
6. Force rotate 4xx/5xx/network failure; confirm visible error, no success toast, button/selection state resets, no unhandled rejection.
7. On an existing row, explicitly click “重置”; verify the confirmation names the channel and says only the short identify-code in-flight window remains.
8. Cancel the confirmation; verify zero rotate requests. Confirm again; verify exactly one rotate request and the new code/version.
9. Explicitly click “复制” for a row; verify success and clipboard-failure toasts.
10. Open the “渠道识别码” menu entry; confirm it resolves to the same page and API capability.
11. Check onboarding: no step teaches Bridge-token rotation/PREV; identify-code in-flight-window guidance remains accurate.
12. Capture per-step evidence: URL, semantic snapshot/text, request method/path/status, toast copy, and console errors. No batch/auto-enumerated clicking.

## Risks & Considerations

- Redirecting to a route with weaker frontend metadata could broaden access; preserve the admin boundary.
- `/bridge/extension` shares the old router module; a broad module-level redirect would break the extension download page.
- Deep links/bookmarks will change URL intentionally; communicate the canonical `/channels/identify-codes` path.
- Legacy APIs may still have non-SPA consumers; do not remove them without an explicit compatibility decision.
- The identify-code page's short in-flight window is legitimate; do not “clean up” that warning as if it were retired Bridge-token grace behavior.
- No browser, DB, build, or test execution was performed in this assess-only lane.

## Open Questions

- [NEEDS CLARIFICATION: Should `/api/bridge/token/status|reset` remain as deprecated compatibility endpoints, or be removed after SPA cutover?]
- [NEEDS CLARIFICATION: If status remains, should it become tenant-scoped and return a typed DB error instead of a global count?]
- [NEEDS CLARIFICATION: Remove `TokenManagement.vue` / `bridgeToken.js` now, or leave them unreachable until a cleanup lane?]
