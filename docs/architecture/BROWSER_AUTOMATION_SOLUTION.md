# hivemtk 浏览器自动化模块 · 解决方案调研文档

> 承接《BROWSER_AUTOMATION_TECH_DECISION.md v1.0》（技术选型结论：维持 Chrome Native Messaging + Go NM Host + 扩展 + user-server 统一端口路线）。
> 本文档记录 v1 稳定化阶段的**问题 → 调研 → 验证 → 落地**全过程，是 TECH_DECISION 的实施配套文档。
> 结论先行：**I1 / I2 已验证完备、无需改代码；I3 缺失已补齐（新增健康检查脚本），v1 三项 P0/P1 目标全部达成。**

## 1. 问题（要解决什么）

v1 稳定化路线定义了 6 个改进项，其中 P0/P1 三项必须先闭环：

| 编号 | 内容 | 优先级 | 落地前状态 |
|---|---|---|---|
| I1 | 扩展原语覆盖（Hand 16 个动作 ↔ 扩展 primitives 对齐） | P0 | 未知：bridge/ 扩展 41 个 JS 文件只做 DM 摄取，疑似缺口 0%→100% 不确定 |
| I2 | token 轮换（host_token 生成/轮换/双候选校验） | P1 | 未知：host_token.go 是否已实现 |
| I3 | 健康检查脚本（一键诊断扩展→Host→服务端链路） | P1 | 缺失：scripts/ 28 个脚本无 browser/host 相关 |

## 2. 调研过程（查了什么）

1. **Hand 命令集**：`user-server/.../browser_automation/service/hand.go`，16 个动作 —
   open_tab / click / type / snapshot / markdown / screenshot / wait / wait_for_selector /
   scroll / click_near / assert / query / post_comment / extract / close_tab / tab_exists。
2. **bridge/ 扩展**：`user-web/browser_automation/` 下 41 个 JS 文件，全部为 DM 摄取通道，
   无 `connectNative`、无 automation handler —— 初看疑似缺口，后证伪（见 §3）。
3. **browser_automation/ 扩展**：9 个文件，`primitives.js` dispatch 全覆盖 16/16，
   另有 CDP 可信输入（input.js）、无障碍引用（accessibility）、activateFirst 等增强。
4. **token 链**：`host_token.go`（Generate / Rotate 老值 `_prev` 宽限 / Validate 常量时间双候选 /
   EnsureExists）+ `host.go`（ResetToken + WS 双防护 token+回环 IP + register 帧 version/pid）。
5. **路由/端口**：`user-server/internal/router/browser_automation_routes.go` —
   GET `/browser-automation/host/status`（JWT 鉴权组）、POST `/host/token/reset`（admin）、
   WS engine GET `/api/browser/host-ws`；user-server 端口 8204（DefaultListenPort），platform 8205。
6. **scripts/**：28 项（audit / check-architecture / deploy-user / e2e 等），确认无 browser-host 健康脚本。

## 3. 解决方案（结论 + 证据）

### 3.1 I1：验证完备，无需改代码

`primitives.js` dispatch 与 Hand 16 动作一一对应（open_tab / click / type / click_near /
post_comment 经 CDP 可信输入 / wait_for_selector / assert / query / scroll / extract /
snapshot 经无障碍引用 / markdown / screenshot / wait / close_tab / tab_exists）。
**缺口清单：空。** bridge/ 扩展与自动化链路无关（DM 摄取引擎），不纳入本模块。

### 3.2 I2：验证完备，无需改代码

轮换语义完整：Rotate 后老值进 `_prev` 宽限期，Validate 双候选常量时间比对，
WS 握手 token + 回环 IP 双防护，register 帧携带 version/pid 可审计。
**缺口清单：空。**

### 3.3 I3：缺失，已补齐 —— `hivemtk/scripts/check_browser_host.sh`

171 行，mode 100755，风格对齐 `check-architecture.sh`。5 级检查：

1. 扩展 manifest：src 与 dist 版本一致；
2. nm-host `hostVersion` 锚点 vs manifest（Chrome SW ScriptCache 陷阱锚点）；
3. Host 二进制 `/usr/local/bin/hivemtk_browser_nm_host` + `~/.hivemtk/nm_host.conf` token 非占位；
4. Chrome NativeMessagingHosts manifest 已注册（macOS/Linux）且路径可执行；
5. 服务端 `host/status` 在线检查（`--offline` 跳过；无 JWT 只警告不失败）。

验证：本机 `--offline` 与无 JWT 两种模式均为 exit 0 全通过；`bash -n` 干净；
扩展 / dist / manifest 三处版本 1.2.0 一致。

## 4. 使用方法

```bash
# 全链路检查（含服务端，需 JWT）
JWT_TOKEN=<token> bash hivemtk/scripts/check_browser_host.sh

# 离线检查（只查本机扩展 + Host，不调服务端）
bash hivemtk/scripts/check_browser_host.sh --offline
```

exit 0 = 全通过；非 0 = 按 `[1]..[5]` 分级报错定位（扩展 → Host → 注册表 → 服务端）。

## 5. 风险与后续（I4–I6，未入 v1）

- I4 重连续跑 P2、I5 审计日志 P2、I6 多 Host 预留 P3：v1 不做，待 v2 路线排期。
- 已知约束：远端 fetch 无权限，提交仅本地（054e1c13 起），推送前需先恢复远端权限并做 fast-forward 检查。
- 版本锚点：扩展 / dist / manifest / nm-host 四处 1.2.0，任一处升级必须同步其余三处（脚本检查 [1][2] 即为此设）。
