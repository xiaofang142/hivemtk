# Bug Fix: retired Bridge token page now redirects to identify-code management

- **Slug**: `bridge-token-page-identify-code-drift-20261125`
- **Fixed**: 2026-11-25
- **Assessment**: `./assessment.md`
- **Status**: applied / scoped GREEN / main independently verified (scoped lane)

## Summary

Old `/bridge`, `/bridge/token`, and `/token` deep links now redirect to the existing `/channels/identify-codes` management capability. The retired token status/reset/PREV form is no longer mounted, `/bridge/extension` remains intact, and the identify-code page keeps its legitimate short in-flight window while gaining visible rotate/generate error recovery.

## Changes

| File | Change |
|---|---|
| `user-web/src/router/modules/bridgeToken.js` | Redirect old token routes to `/channels/identify-codes`; remove `TokenManagement.vue` lazy import; preserve admin metadata and `/bridge/extension` |
| `user-web/src/router/modules/channels.js` | Add `requiresAdmin:true` to `ChannelIdentifyCodeManage` so the redirected destination preserves the old admin-only boundary |
| `user-web/src/views/channels/identify-code-manage.vue` | Add rotating state and inline error alert; catch rotate/generate failures; distinguish confirmation cancel from API failure; recover selection/loading state; retain short identify-code in-flight warning |
| `user-web/tests/unit/router_bridge_token_redirect.test.js` | New route/navigation regression: old URLs redirect, no legacy API calls/form copy, admin boundary, extension route intact |
| `user-web/tests/unit/identify_code_manage.test.js` | New component regression: legitimate in-flight copy, rotate success/cancel/failure/recovery, generate failure/recovery |

## Diff Highlights

```js
{
  path: '/bridge/token',
  name: 'BridgeTokenManagement',
  redirect: '/channels/identify-codes',
  meta: { title: '桥接凭证', requiresAdmin: true }
}
```

```js
async function rotate(channelId) {
  if (rotating.value) return
  rotating.value = true
  rotationError.value = ''
  try {
    // rotate + success toast + reload
  } catch (e) {
    rotationError.value = `识别码操作失败：${e?.message || '未知错误'}`
    ElMessage.error(rotationError.value)
  } finally {
    rotating.value = false
  }
}
```

## Tests Added or Updated

- `router_bridge_token_redirect.test.js` — 6 tests covering direct old URLs, redirect definitions, admin metadata, and extension route.
- `identify_code_manage.test.js` — 6 tests covering valid in-flight copy, rotate success/cancel/error/recovery, and generate error/recovery.

## Local Verification

- RED: `npx vitest run tests/unit/router_bridge_token_redirect.test.js tests/unit/identify_code_manage.test.js` → rc=1, 8 failed / 4 passed / 1 unhandled rejection; failures matched the assessed defects. Log `check-bridge-token-ui-red.log`.
- GREEN: same command → rc=0, 12/12 PASS. Log `check-bridge-token-ui-green-r2.log`.
- Related frontend regression: 6 files / 47 tests PASS. Log `check-bridge-token-ui-related-tests-final.log`.
- Scoped lint on all changed frontend files: rc=0, 0 errors / 0 warnings. Log `check-bridge-token-ui-scoped-lint-r2.log`.
- Full `npm run lint:check`: rc=1 due one pre-existing unrelated error at `src/views/customerImportJob/List.vue:376` (`preserve-caught-error`), plus the repository's existing warning baseline. The file is byte-identical to HEAD. Log `check-bridge-token-ui-lint.log`.
- `npm run build`: rc=0. Log `check-bridge-token-ui-build.log`.
- `git diff --check`: rc=0.
- Browser/manual UI: not run; no browser automation or batch clicking was performed.

## Deviations from Assessment

No production scope expansion. The legacy component/API files and backend compatibility endpoints remain in place but are unreachable from the old UI routes; deletion and backend status-scope/comment cleanup remain separate follow-ups. The identify-code page's short in-flight warning was intentionally preserved.

## Follow-ups

- Main independent verification of route redirects and component error recovery.
- Optional later cleanup: remove unreachable `TokenManagement.vue` / `bridgeToken.js`, stale onboarding copy, and update route inventory docs.
- Separate compatibility decision for `/api/bridge/token/status|reset` and its tenant-scope/error semantics.
- Full lint remains blocked by the unrelated pre-existing `customerImportJob/List.vue` error; not fixed in this lane.
