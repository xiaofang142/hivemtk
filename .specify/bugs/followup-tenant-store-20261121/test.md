# Bug Verification: FA-26/27 跟进提醒租户授权与统一存储

- **Slug**: followup-tenant-store-20261121
- **Tested**: 2026-11-21
- **Result**: pass（本地专项与回归全绿；跨副本生产口径见边界）

## 关闭判据对照

- FA-26「服务端按租户和操作者授权查改，跨租户隐藏存在性、普通用户不得更改他人待办」✔ 授权矩阵覆盖：跨租户同 ownerID（读/完成/取消全拒绝且回报"不存在"）、同租户 u2 被拒且状态零变化、管理员豁免成功、本人成功、重复完成守卫。被拒路径在旅程/统计推进前返回 ⇒ 零越权副作用。
- FA-27「所有生产入口共享按租户隔离的权威仓储…重启/双副本仍唯一可见」✔ 三条生产链装配改为同一持久仓储；测试覆盖实例间可见性 + 重启（同存储新实例）可见 + 状态跨实例一致。双副本=同库存储天然一致；未起真实第二进程复现。

## Checks

| Check | Command | Result | Notes |
|---|---|---|---|
| 授权矩阵+共享存储 | go test ./internal/service -run 'TestFollowUpTenant.*\|TestFollowUpSharedStore.*' -count=1 -v | pass | green.log 2 PASS |
| 专项 race | 同上 -race | pass | race.log |
| 关联链回归 | go test ./internal/service -run 'TestFollowUp\|TestFollowup\|TestCollection\|TestClosedLoop\|TestClosed_Loop' -count=1 -v | pass | regression.log 71 PASS / 0 FAIL |

## 边界

- 专用快测实例 127.0.0.1:8217（凭据由容器环境读入，未打印）。
- FollowUpPortAdapter（AI 工具）仍以 Background ctx 走系统权威口：工具运行时的租户绑定属 FA-AGENT-TOOL-01/02 已交付范围；系统链缺租户落 default:1 与全仓口径一致。
- 未提交/推送/部署；未运行真实页面。

## Evidence Files

fix.md / green.log / race.log / regression.log / source-sha256.json
