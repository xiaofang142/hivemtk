# Bug Fix: PUT /api/users/:id 清空手机号被「假成功」吞掉（Phone 改 *string 判存在）

- **Slug**: user-profile-phone-clear-ignored
- **Fixed**: 2026-11-11
- **Assessment**: ./assessment.md
- **Status**: applied

## Summary

assessment 开出的修法（`UpdateUserRequest.Phone` 改 `*string` + `!= nil` 分支，对齐
`UpdateByAdmin` 先例）已在提交 `c387adb7`（2026-11-10 23:06:46）原样落地并随
`assessment.md` 一起入库。本轮（2026-11-11 A-20）动手前复核发现该证据漂移，按纪律
停止重复改源码，转为：核实该提交与 assessment 逐字一致、补齐缺失的「空串清空生效」
回归测试（唯一缺口）、完成本地验证。本轮对 `system_user.go` 工作区零改动
（`git diff` 为空），仅新增一条测试。

## Evidence Re-check（动手前复核）

- assessment 所述根因 `if req.Phone != ""` 在当前工作区已不存在：现行代码为
  `system_user.go:215-216` `if req.Phone != nil { user.Phone = *req.Phone }`。
- 漂移来源坐实：`git show c387adb7 -- user-server/internal/service/system_user.go`
  显示该提交包含 `-if req.Phone != ""` → `+if req.Phone != nil`、
  `Phone string` → `Phone *string`（:56）及修复注释（:52-53），与 assessment 修法
  逐字一致；构造点 `token_revocation_callsites_test.go:137` 同批改为
  `Phone: &[]string{"13800000000"}[0]`。
- 同仓先例未动：`UpdateByAdmin`（system_user.go:618-620）仍为
  `if req.Phone != nil { target.Phone = *req.Phone }`，自助与管理两条路径语义现已对齐。
- assessment 的根因判断与预提交代码吻合（该提交的删除行即原根因），属「修法已被
  抢先实施」而非「根因不符」，无需重跑 assess。
- 控制器无需改动：`auth.go:792-796` 直接 `ShouldBindJSON` 到 `*string` 字段，
  `{"phone":""}` 绑为指向空串的指针、字段缺省/`null` 绑为 nil，语义正确。

## Changes

| File | Change | Notes |
|------|--------|-------|
| `user-server/internal/service/system_user.go` | modified by commit c387adb7（本轮零改动） | `UpdateUserRequest.Phone` `string`→`*string`（:56）+ `UpdateUser` 分支 `!= ""`→`!= nil`（:215-216）+ 修复注释（:52-53）。Email/RealName/Role/Status 四字段语义未动 |
| `user-server/internal/service/token_revocation_callsites_test.go` | modified by commit c387adb7（本轮零改动） | :137 构造点改指针写法 `Phone: &[]string{"13800000000"}[0]` |
| `user-server/internal/service/system_user_test.go` | added test（本轮，+52 行，:869-917） | 新增 `TestSystemUserService_UpdateUser_PhoneClear`：空串=显式清空（响应体+库中重载双断言）；nil=不修改（旧 `!= ""` 对无值场景的等价语义） |

## Diff Highlights

本轮新增回归测试的核心断言（c387adb7 的源码 diff 见上节 Evidence Re-check）：

```go
// 空串 = 显式清空：必须真的把库里的手机号清掉，而不是静默跳过。
req := &UpdateUserRequest{Phone: &[]string{""}[0]}
updatedUser, err := service.UpdateUser(context.Background(), user.ID, req)
// ... response 断言 updatedUser.Phone == ""
var reloaded model.SystemUser
database.First(&reloaded, user.ID)
if reloaded.Phone != "" {
    t.Errorf("Expected phone cleared in DB, got %q", reloaded.Phone)
}
// nil = 未提供：不修改（与旧 != "" 对无值场景的语义等价）。
```

## Tests Added or Updated

- `internal/service/system_user_test.go::TestSystemUserService_UpdateUser_PhoneClear` —
  钉死两条语义：`Phone: &""` 必须清空（响应体与 DB 重载双断言）；`Phone: nil`
  不修改既有值。测试有牙（见 Local Verification 反向测）。
- 此前全仓无任何「清空手机号」断言（本轮动手前 `grep Phone system_user_test.go`
  零命中），本条即 assessment 影响面分析里「仅 Phone 字段语义变化」的防回归锚。

## Local Verification

- 环境口径：按 CLAUDE.md 3.5 导出测试库变量；实测用 8217 专用快测实例
  （`.env` 声明 `USER_POSTGRES_TEST_HOST_PORT=8217`，实例在监听）+
  `POSTGRES_TEST_PASSWORD` 取自 `.env` 的 `POSTGRES_PASSWORD`。
- `gofmt -l system_user.go system_user_test.go token_revocation_callsites_test.go` → 空输出。
- `go build ./...` → 无输出（exit 0）。
- `go test ./internal/service/ -run 'TestSystemUserService_UpdateUser|TestSystemUserService_toUserResponse' -v -count=1`
  → 10 枚全 PASS（含新 `PhoneClear`、构造点所在 `UpdateUser_RevokesOnPrivilegeChange`），
  `ok hivemtk-user/internal/service 8.232s`。
- **反向测（真红因，非引用旧结论）**：把 `:215` 分支临时改回旧语义
  `if *req.Phone != ""`（真旧代码是 string 比较、无解引用；此为等价模拟），
  单跑 `PhoneClear` → `--- FAIL`，红因恰为 bug 本体：
  `system_user_test.go:894: Expected phone cleared in response, got "13800000000"`、
  `:901: Expected phone cleared in DB, got "13800000000"`。随后从备份还原，
  `md5` 与提交态逐字一致（`ed61a27b2bfc9d1e0becaaf09f76770d`），复跑 → PASS。
  - 伪影说明：反向跑第二段（nil 分支断言）在模拟写法下对空 `UpdateUserRequest{}`
    解引用 nil panic（栈帧 `system_user_test.go:909`）——真旧代码不存在此路径，
    该伪影不影响清空路径红因的有效性。
- `go test ./internal/service/ -count=1`（全包回归）→
  `ok hivemtk-user/internal/service 536.098s`，零 FAIL。
- `go test ./internal/controller/ -run 'TestSystemUserController_UpdateUser' -v -count=1`
  → `--- PASS: TestSystemUserController_UpdateUser_Success (14.25s)`
  （覆盖 `ShouldBindJSON` → `*string` 的 HTTP 绑定路径）。
- 未跑 `make lint` / `make vet`：本轮源码改动仅为已入库的 c387adb7 与一条测试新增，
  未触碰装配面；且 test 阶段纪律禁改源码，未为门禁绿灯重复全量 lint。

## Deviations from Assessment

- 证据漂移（非方案偏离）：assessment 记录的「现状 `if req.Phone != ""`」在动手前已被
  提交 `c387adb7` 修掉。本轮未重复实施该修复（工作区已是目标态，重复实施无可做差异），
  改为核实该提交与 assessment 修法逐字一致后补缺口（回归测试 + 本报告 + test.md）。
  assessment「Files likely to change」列出的两个文件与最终落库内容完全吻合，
  无额外文件、无语义外溢。
- assessment 未显式列「Tests to add」小节（只列了构造点适配），本轮按任务指令
  「如能补一条 Phone 清空（空串）生效的回归测试则补上」补齐；该测试在本轮前全仓缺失。

## Follow-ups

- 下次有 user-server 在跑实例的联调轮，用一次性测试账号补一次 UI/API 端到端复核
  （profile 清空手机号 → 保存 → 刷新为空），详见 test.md 的 Residual Risks 与
  Recommendation。
