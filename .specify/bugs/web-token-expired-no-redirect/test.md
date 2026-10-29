# Test: web-token-expired-no-redirect

**日期**：2026-10-29 ｜ **方法**：ego-browser 驱动 dev 前端（user-web :8211 / platform-web :8212），
CDP `Page.addScriptToEvaluateOnNewDocument` 在**每个新文档加载前**注入过期 JWT
（`exp = now-7200h`，签名任意——服务端必 401），精确模拟「用户 F5 时 localStorage 里已有过期令牌」。

## 修前基线（复现）

| 场景 | 修前结果 |
|---|---|
| user-web：过期 token + reload ×6 | ❌ 6/6 停在 `#/messageHub/list`，token 被清、无跳转 |
| platform-web：过期 token + reload | ❌ 停在 `#/dashboard`，数据全空只有过期 toast |

## 修后回归

| 用例 | 结果 |
|---|---|
| user-web：过期 token + 直接访问受保护页 ×6 | ✅ 6/6 落 `#/login`，token 已清 |
| platform-web：过期 token + 直接访问受保护页 ×4 | ✅ 4/4 落 `#/login`，token 已清 |
| user-web 全量 vitest | ✅ 71 文件 / 705 用例全绿（含 router_query_preserve 7 例——mock 已补 setToken 形状） |
| platform-web `vite build` | ✅ 编译通过 |
| user-web `vite build` | ✅ 编译通过 |

## verdict

**verified**（用户报告的两端症状均已复现→修复→回归通过；正常路径无回归——
守卫同步只在「localStorage 无 token 而 store 自称已登录」的僵尸态下动作，
有效会话零触碰）。
