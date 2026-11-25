# Bug Verification: retired Bridge token page now redirects to identify-code management

- **Slug**: `bridge-token-page-identify-code-drift-20261125`
- **Tested**: 2026-11-25
- **Assessment**: `./assessment.md`
- **Fix**: `./fix.md`
- **Result**: main independently verified (scoped lane); full lint has one unrelated pre-existing error; manual UI not run

## Summary

Automated router/component regressions prove the old URLs no longer mount the retired token page or call its status/reset APIs. The destination identify-code page preserves the legitimate short in-flight window and now handles rotate/generate failure and recovery. Frontend build and scoped lint pass; full lint is blocked only by an unchanged `customerImportJob/List.vue` error.

## RED Evidence

Command:

```text
npx vitest run tests/unit/router_bridge_token_redirect.test.js tests/unit/identify_code_manage.test.js
```

Result: rc=1, 8 failed / 4 passed / 1 unhandled rejection.

Failed behaviors:

- `/bridge`, `/bridge/token`, `/token` did not converge on `/channels/identify-codes`.
- Old route still lazy-loaded `TokenManagement.vue`.
- Identify-code route lacked `requiresAdmin`.
- Rotate API failure produced no error toast/inline error.
- Recovery after rotate failure was impossible.
- Generate failure produced an unhandled rejection and no selection/error recovery.

Log: `check-bridge-token-ui-red.log`.

## GREEN Evidence

| Check | Result | Log |
|---|---|---|
| New router/component tests | 12/12 PASS | `check-bridge-token-ui-green-r2.log` |
| Related router/menu/onboarding regressions | 6 files / 47 tests PASS | `check-bridge-token-ui-related-tests-final.log` |
| Scoped ESLint on changed files | rc=0；0 errors / 0 warnings | `check-bridge-token-ui-scoped-lint-r2.log` |
| Frontend production build | rc=0 | `check-bridge-token-ui-build.log` |
| `git diff --check` | rc=0 | final stability record |

### Pinned behaviors

1. `/bridge`, `/bridge/token`, `/token` resolve to `/channels/identify-codes`.
2. `TokenManagement.vue` is not mounted; `getBridgeTokenStatus` and `resetBridgeToken` are not called.
3. Old URL render contains no “生成新凭证”, “灰度窗口”, or “PREV” copy.
4. `ChannelIdentifyCodeManage` and old redirect routes preserve `requiresAdmin`.
5. `/bridge/extension` remains a lazy component route and is not redirected.
6. Identify-code page still says “短暂在途窗口”; it does not render retired PREV grace copy.
7. Rotate success calls the identify-code API, shows the new code, and reloads the list.
8. Confirmation cancel performs no rotate request.
9. Rotate/generate API failure shows an error, never a success toast, clears rotating state, and supports a later successful retry.
10. Generate selection is cleared after an attempt and can be chosen again.

## Full Lint Baseline

`npm run lint:check` returned rc=1 with 1 error and 982 warnings. The only error is:

```text
user-web/src/views/customerImportJob/List.vue:376
preserve-caught-error: There is no `cause` attached to the symptom error being thrown
```

`git diff --quiet HEAD -- user-web/src/views/customerImportJob/List.vue` returned 0, and HEAD source at the same lines contains the same throw-without-cause pattern. It is pre-existing and outside this lane. Log: `check-bridge-token-ui-lint.log`.

## Build Evidence

`npm run build` completed with rc=0 and generated the production bundle, including the identify-code management chunk. No tracked source files were changed by the build. Log: `check-bridge-token-ui-build.log`.

## Manual / UI Evidence

Not run. This lane did not start a browser, click elements, or use batch automation.

## 逐元素 UI Acceptance Checklist for Main / Later Approved Run

1. As admin, open `/bridge/token`; confirm redirect to `/channels/identify-codes` and no `/api/bridge/token/status` request.
2. Repeat for `/bridge` and `/token`; confirm no retired token form, reset button, PREV, or token grace-window copy.
3. Open `/bridge/extension`; confirm the extension page still renders normally.
4. As non-admin, open the old URLs; confirm 403/NotFound and no legacy API call.
5. On identify-code management, choose a channel and click “生成识别码”; verify one rotate request and success code.
6. Force rotate 4xx/5xx/network failure; verify inline error + error toast, no success toast, button state resets, and no unhandled rejection.
7. Retry the same action successfully; verify the inline error clears and the list refreshes.
8. Click an existing row's “重置”; verify confirmation names the channel and only mentions the short identify-code in-flight window.
9. Cancel confirmation; verify zero rotate requests. Confirm again; verify one request and the new code/version.
10. Click “复制”; verify success and clipboard-failure toasts.
11. Inspect menu entry “渠道识别码”; confirm it opens the same canonical page.
12. Capture URL, semantic snapshot/text, request method/path/status, toast copy, and console errors for each explicit action. No auto-enumerated clicking.

## Residual Risks / Deferred Scope

- `TokenManagement.vue`, `bridgeToken.js`, stale onboarding/i18n copy, and backend compatibility endpoints remain as unreachable or separate cleanup work.
- Global identify-code status counting/error swallowing and controller token-rotation comments were explicitly not changed.
- Full lint's unrelated `customerImportJob/List.vue` error remains.
- No real outbound, deployment, UI, or production verification was performed.
- Main independent verification is complete for the scoped lane; manual UI evidence remains not run.

## Main Independent Verification

- Time: 2026-11-25 10:52:04–10:52:25 +08:00.
- HEAD: `2c5c53e3ded742ac0c6ebb1944529aaed18973fd`, checked before/after every command; no drift.
- Router/menu/onboarding regressions: 6 files / 47 tests PASS.
- Scoped ESLint: rc=0.
- `npm run build`: rc=0, built in 11.22s.
- Five scoped SHA256 hashes identical before/after; `git diff --check` rc=0.
- Active router/view grep: no `getBridgeTokenStatus`, `resetBridgeToken`, `prev_configured`, `生成新凭证`, or retired grayscale copy.
- Full-lint baseline file `customerImportJob/List.vue` is byte-identical to HEAD.
- Evidence: `main-bridge-token-ui-verification-20261125.txt` and `main-bridge-token-ui-verification-summary.txt` (`overall_rc=0`).
- Boundaries: manual per-element UI evidence, browser execution, deployment, and real external behavior were not run.
