# Feature Specification: ego-lite 腿收口——事前阻塞写确认闸 + runner 接线杂项

**Feature Branch**: `014-egolite-blocking-confirm-gate`

**Created**: 2026-10-27

**Status**: Approved

**Input**: 用户指令「检查、调研、评估 ego-lite 接入方案并做完整的全链路论证……评估调研清楚后，
生成开发任务文档，随着按照文档开发，测试，交付」。

**依据文档**（本 spec 是其残余项的施工源）：

- `internal-docs/ego-lite-集成总体架构设计-2026-10-27.md` §七之四「登记的残余语义」：
  写操作确认闸当前生效的是**乐观执行 + 可杀死**，不是事前阻塞；
- `internal-docs/ego-browser-调用机制调研论证-2026-10-27.md` §六残余风险表（`--ego-server-name` 未使用）；
- `internal-docs/ego-lite-集成方案调研论证-2026-10-27.md` §7 决策点 4/6（写操作逐步确认是硬前置）。

## 一、现状与本轮结论（全链路论证收口）

ego-lite 作为第二执行腿的接入方案已经完成**论证→拍板→落地→真机复核**四步
（决策点 1-8 全部通过，M1/M2 + 资产目录自包含闭环 + 前端 5 页已入库，
`internal/egolite` 37 组单测全绿，真机修过 3 处接线断裂）。
用户三职责映射现状：

| 用户职责 | 现状 | 残余 |
|---|---|---|
| 调度 | `POST /api/egolite/executions`（并发闸 ≤2、draft/market 双通道、created→dispatching） | 无 P0 |
| 执行 | `ego-runner` outbound WS 收 spec → `ego-browser nodejs` stdin → 事件行回传 | 本轮修 4 处接线（见 §二） |
| 监督 | `ego_event_log` append-only + Monitor 页 + 看门狗（confirm TTL / 硬超时）+ 断连收口 | confirm TTL 到期未向 runner 推 abort 帧 |
| 官方升级 | 不 fork：CLI/skill/浏览器全归 ego 官方，runner 只探测版本 + 提示 `ego-browser upgrade` | `--ego-server-name` 透传缺失 |

**「to-browser」查证结论**：官方与社区均不存在名为 to-browser 的工具/协议；
用户所指即 **ego-browser**（ego lite 的 CLI + skill 面）。opencode 与它的协作机制
已由 E1-E14 实测钉死：skill 文件系统发现（三层符号链接）→ bash 执行
`ego-browser nodejs`（stdin 喂脚本）→ stderr 的 `console.log` JSON 行反馈 →
TaskSpace 承载跨轮状态。无插件、无 MCP、无私有 IPC。HiveMTK 的 ego-runner
复刻的是同一条路，差异仅在「谁来对错误负责」（无人值守 → 下发前拦截 + 事前确认闸）。

## 二、需求（本轮范围）

### R1 事前阻塞写确认闸（P0，架构文档 §七之四 明示的残余）

写动作必须在**发生之前**拿到用户裁决，而不是「先写完、不认可再杀」。

- 资产/流程脚本在写提交点调用 runner 注入的 `__awaitConfirm__(label)`：
  ① 打印 `{"type":"confirm",...}` 事件行（上报链不变）；② 轮询本机裁决文件直到
  allow / deny / TTL 到期；③ allow 返回 true 放行，deny/TTL 返回 false（脚本自行
  `process.exit(2)` 收口，事件流里留 `ok:false` 审计行）。
- runner 收到服务端 confirm 帧：allow → 写裁决文件；deny → 写裁决文件 + 取消执行进程
  （fail-close，既有语义）。裁决文件路径随执行注入（每执行独立临时目录，无复用/残留面）。
- 服务端 confirm TTL 看门狗判停后，**同步向 runner 推 abort 帧**（fail-close：到期即死，
  不许脚本继续轮询空转占住 space）。
- dry-run 对声明了 writes 的流程同样强制 `confirm_policy=require`（决策 4 的既定口径，
  修复 `flow_service.DryRun` 硬编码 none 的偏差）。

### R2 修复 4 处「接口通了但跑起来断」的接线（同类断裂第三批）

1. **abort/confirm 帧永远匹配不上**：服务端下行用行键 `EgoExecKey(eg-<row>)`，
   runner 在途执行键是 dispatch 键 `ego-<row>-<ms>`，精确相等必假 → 用户拒绝/看门狗
   abort 到达 runner 后被静默丢弃，进程照跑。修法：runner 侧前缀匹配
   （`ego-<row>` 命中 `ego-<row>-*`；`ego-91` 不得命中 `ego-9-*`）。
2. **confirm 帧的 decision 在解帧时丢失**：readLoop 把 `spec` 解成 taskSpec，
   `decision` 字段不是 taskSpec 成员直接被扔 → 即使 1 修好，allow/deny 也到不了裁决文件。
   修法：readLoop 先收 RawMessage，按帧类型分别解。
3. **`--ego-server-name` 透传缺失**（调用机制调研 §六登记待办）：多 ego 实例并存时
   runner 会连错实例。conf 增加 `ego_server_name=`，exec 参数前置
   `--ego-server-name=<name>`（官方 CLI 唯一跨实例选择手段，E3）。
4. **仓库里误落编译产物** `user-server/ego-runner`（9MB Mach-O，Oct 27 14:48）：
   删除（可再生构建产物，不进 git）。

### R3 真机协议 smoke（非单测可覆盖部分）

用本机在跑的 ego lite（0.6.1.13）实测裁决文件轮询协议的三条路径：
allow 放行 / deny 停止 / TTL 到期自停——验证注入的 JS 在 ego 内嵌 Node（v24）里
语法成立、`node:fs/promises` 动态导入可用、轮询语义正确。

## 三、非范围（后续轮，不混入本 lane）

- 跨实例官方包通道（binary_asset + SyncPull + ed25519 验签接入主链路，代码已备未接）；
- handoff→resume 全流程真机验证（F12，需真人配合 ego UI 交互，保留 P1 必测项）；
- 平台遥测（S8）、cron 触发（S10）、aiagent 工具（S11）、安装器（R2）、Windows（T11）、
  沙箱审核机（T12）、运行时收敛全量（T13）、SSE（T18）——按架构文档 M3/M4 排期。

## 四、验收标准

1. `go build ./...` 全绿；`internal/egolite`、`cmd/ego-runner` 单测全绿
   （测试库 env：`POSTGRES_TEST_PORT=8202` + 容器口令，探针裁决）。
2. 新增测试钉死：a) 组装脚本注入 confirm 契约（文件路径/TTL/helper 定义）；b)
   allow 写裁决文件且不取消进程；c) deny 写裁决文件且取消进程；d) 键前缀匹配语义；
   e) decision 解帧不丢；f) dry-run writes→require；g) ego_server_name 参数拼装。
3. 真机 smoke 三路径通过（allow/deny/TTL）。
4. 仓库无 `user-server/ego-runner` 编译产物残留。

## 五、脚本契约（写进发布台/流程编辑器的作者须知，随本轮入库于 preamble 自述）

```js
// 写提交点前调用；false = 用户拒绝或确认超时，必须立即收口不得继续写：
if (!(await __awaitConfirm__("提交订单"))) {
  console.log(JSON.stringify({ type: "action", ok: false, note: "confirm_denied_or_timeout" }));
  process.exit(2);
}
// 声明了 writes 的资产/流程未走 __awaitConfirm__ 时，行为退回「乐观执行 + 可杀死」
// （服务端 deny/TTL 仍会杀进程）——但不建议：审计只有事后行，没有事前闸。
```
