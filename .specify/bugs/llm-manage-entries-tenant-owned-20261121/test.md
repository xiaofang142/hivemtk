# Bug Verification: FA-LLM-01（写/测口子范围）

- **Slug**: llm-manage-entries-tenant-owned-20261121
- **Tested**: 2026-11-21
- **Result**: partial（本子范围 pass；架构级余项保持开放）

## 关闭判据对照（子范围）

- 「越权请求零注册表变化、零写库、零上游调用」✔（A 用 B 名字改/删/测全拒绝且 provider 仍在；拒绝发生在落库与上游调用之前）
- 「A/B 同名及不同名配置分别验证」→ 判定走名单白名单，与名字无关，等价覆盖
- 「不能仅增加 UI 过滤」✔（服务端写口判定 + 403 映射）

## 真正余项（架构级，保持开放）

- 「非默认租户新建配置写到默认租户」：内存注册表是全局单键 map（dispatcher 内注释已登记「多租户需按租户分 map」），新建/调用链路的按租户分 map 属架构改造，本子范围不动。
- 「路由/缓存/调用均带同一作用域」同上，随分 map 改造推进。

## Checks

| Check | Command | Result | Notes |
|---|---|---|---|
| 归属判定专项 | go test ./internal/service -run 'TestLLMRouting_ManageEntries' -count=1 -v | pass | green.log 2 PASS |
| 专项 race | 同上 -race | pass | race.log |
| LLM 路由回归 | -run 'TestLLMRouting\|TestLLMSource\|TestLLMProvider' | pass | regression.log 14 PASS / 0 FAIL |

## Evidence Files

fix.md / green.log / race.log / regression.log / source-sha256.json
