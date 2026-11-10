# Bug Verification: PUT /api/users/:id 清空手机号「假成功」修复验证

- **Slug**: user-profile-phone-clear-ignored
- **Tested**: 2026-11-11
- **Assessment**: ./assessment.md
- **Fix**: ./fix.md
- **Result**: partial

## Summary

assessment 的原始复现是 UI 点验 + 在线 API 复核（K 轮线上 #6 profile）；本轮无在跑
user-server 实例（8204 /health 探测无响应），未做在线端到端复现，按命令体守卫
不虚标 `verified`，判 `partial`。服务层等价验证全绿且双向有牙：修后语义下
「空串清空」真清库（响应体+DB 双断言）；反向还原旧语义时回归测试真红、红因恰为
线上症状本体（清空被吞、旧值 `13800000000` 留存）。全包回归 536s 零 FAIL。

## Checks Performed

| Check | Command / Action | Result | Notes |
|-------|------------------|--------|-------|
| Reproduction (post-fix, service-level) | `go test ./internal/service/ -run TestSystemUserService_UpdateUser_PhoneClear -v -count=1` | pass | `Phone: &""` → 响应体与 DB 重载均为 `""`；`Phone: nil` → 既有值不变 |
| Reproduction (pre-fix, reverse check) | 临时把 system_user.go:215 改回旧语义 `*req.Phone != ""` 单跑同用例 | pass（预期红） | 真红因=线上症状本体（:894/:901 旧值留存）；还原后 md5 `ed61a27b` 与提交态逐字一致，复跑转绿 |
| 原始复现（UI 点验 + 在线 API） | 探测 `http://127.0.0.1:8204/health` | skipped | 无在跑实例；且在线复现会改共享环境数据（清真实用户手机号），属破坏性/昂贵检查，按命令体记 skipped 不擅自执行 |
| New / updated tests | `go test ./internal/service/ -run 'TestSystemUserService_UpdateUser|TestSystemUserService_toUserResponse' -v -count=1` | pass | 10 枚全 PASS（含构造点用例 RevokesOnPrivilegeChange），`ok ... 8.232s` |
| Regression suite | `go test ./internal/service/ -count=1` | pass | `ok hivemtk-user/internal/service 536.098s`，零 FAIL |
| Controller binding path | `go test ./internal/controller/ -run 'TestSystemUserController_UpdateUser' -v -count=1` | pass | `PASS 14.25s`；`ShouldBindJSON` 对 `*string` 的 HTTP 绑定路径正常 |
| Build | `go build ./...` | pass | 无输出，exit 0 |
| Format | `gofmt -l`（三个相关文件） | pass | 空输出 |
| Lint / vet | `make lint` / `make vet` | skipped | 本轮源码改动仅为已入库的 c387adb7 + 一条测试新增，未触装配面；test 阶段禁改源码，未重复全量门禁 |

## Output Excerpts

定向用例（修后）：
```
=== RUN   TestSystemUserService_UpdateUser_PhoneClear
--- PASS: TestSystemUserService_UpdateUser_PhoneClear (0.13s)
...
--- PASS: TestSystemUserService_UpdateUser_RevokesOnPrivilegeChange (0.00s)
PASS
ok  	hivemtk-user/internal/service	8.232s
```

反向测红因（旧行为，测试有牙的直接证据）：
```
system_user_test.go:894: Expected phone cleared in response, got "13800000000"
system_user_test.go:901: Expected phone cleared in DB, got "13800000000"
--- FAIL: TestSystemUserService_UpdateUser_PhoneClear (1.12s)
```

全包回归与构建：
```
ok  	hivemtk-user/internal/service	536.098s
ok  	hivemtk-user/internal/controller	18.686s   （-run TestSystemUserController_UpdateUser）
go build ./...  → 无输出（exit 0）
```

## Residual Risks

- 在线端到端（UI profile 页清空 → 保存 → 刷新为空）未跑：无在跑实例，且复现需改
  共享环境数据。服务层用例与线上症状同根因（同一 `UpdateUser` :215 分支），风险低
  但未闭环到 UI/HTTP 层。
- controller 既有 `UpdateUser_Success` 用例只发 real_name 载荷，未显式覆盖
  `{"phone":""}` 的 HTTP 级清空路径（`*string` 绑定由 gin 语义与服务层断言覆盖）。
- `Email/RealName/Role/Status` 四字段语义按 assessment 要求未动，
  `TestSystemUserService_UpdateUser_EmptyFields`（空值=不改）反向钉死仍绿，
  未发现回归。

## Recommendation

服务层闭环完成、回归测试双向有牙；建议主智能体在下次有 user-server 实例的联调轮，
用一次性测试账号补一次 UI/API 端到端复核后关闭本 bug。若接受「自动化等效验证」
口径（服务层证据 + 反向红因），可按 `partial → 关闭（留 UI 复核为遗留项）` 处理。
