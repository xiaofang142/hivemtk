# Assessment: user-profile-phone-clear-ignored

- 日期：2026-11-10（K轮线上 SaaS 拟人点击测试，路由 #6 `/profile`）
- 发现方式：UI 点验「清空手机号 → 保存修改 → toast 保存成功」，API 复核 `GET /api/auth/current-user` 仍返回旧值 `13800000000`（写操作 UI 成功 ≠ 落库）。

## 现象
`PUT /api/users/:id`（`SystemUserController.UpdateUser`）接收 `{"phone": ""}` 时返回 200「更新用户成功」，但库中 phone 保持原值；用户无法清空手机号。

## 根因
`internal/service/system_user.go` `SystemUserService.UpdateUser`：

```go
if req.Phone != "" {
    user.Phone = req.Phone
}
```

空串被当作「未提供」跳过，清空语义被静默吞掉；controller 层无校验、前端无感知，形成「假成功」。

## 佐证（同仓先例）
`UpdateByAdmin`（system_user.go:598-617）已对 Email/RealName/Phone 使用 `*string` + `!= nil` 判存在；自助路径未对齐。

## Files likely to change
- `user-server/internal/service/system_user.go`（UpdateUserRequest.Phone → *string；UpdateUser 分支 → != nil）
- `user-server/internal/service/token_revocation_callsites_test.go:137`（构造点改指针）

## 影响面
- 仅 Phone 字段语义变化：nil=不改（与原 `!= ""` 对无值场景等价），非 nil=显式设值（含空串=清空）。
- Email/RealName/Role/Status 语义不变（本轮不扩大改动）。
- 前端 Profile.vue 无需改（本就会发送 `phone` 字段，空串即清空意图）。

## 判据
非 invalid：空串是合法的「清空」意图，且同仓 UpdateByAdmin 已实现该语义。
