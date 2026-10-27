# Feature Specification: ego-lite 调度闭环（cron 触发 + AI 工具）与 runner 自更新（specs/014 改进清单 C10 余量 / C12）

**Feature Branch**: `016-egolite-scheduling-and-selfupdate`

**Created**: 2026-10-28

**Status**: Approved

**Input**: 用户指令「按照评估与改进清单一次性完成代码开发和测试」——C 组剩余可代码化项。

## 一、范围

| 清单项 | 本轮交付 |
|---|---|
| C12-S10 cron 触发 | `egolite_flow_crons` 表 + CRUD/启停 + TaskManager 注册（browser_cron_triggers 模式）+ 重启恢复 + 路由 + 前端编辑器字段。**纪律：只允许挂纯读流程（writes 空）**——决策点 6（ego 腿全自动写受限），无人值守写流程会在 waiting_confirm 卡到 TTL 判停 |
| C12-S11 aiagent 工具 | `egolite_run` 工具（设计文档 S11「SetBrowserTaskRunner 同型」）：flow_id/asset_id 双通道发起执行，异步派发，注入式接线（tooluse 不反向依赖 egolite 域） |
| C10 余量 runner 自更新 | conf 增 `repo_dir`（可选）：upgrade 帧处理在 `ego-browser upgrade` 之后追加自更新（git pull --ff-only + go build 到当前二进制路径，重启生效），结果事件回传 |
| C1 余量 | 真机 handoff→takeOverTaskSpace 尝试（自动化可达的最大程度；人工确认环节如仍需真人则如实登记） |

不进本轮（与清单一致）：C3 法律评估（外部）、C5/C6（platform 联动、决策门控 M3）、
C7 余量 Windows 包（无法在本机验证）、C8/C9（隐私评审/G-E7 前置）、C13/C14/C15/C18（P3 排期）。

## 二、需求细节

### R1 流程 cron 触发（S10）

- 表 `egolite_flow_crons`：flow_id（uniqueIndex）+ user_id + cron_expr（5 段）+ time_zone
  （G20：IANA 名，空=服务器本地）+ enabled（**不声明 default:true**——BrowserCronTrigger
  同款 GORM 零值陷阱，缺省语义在 service 层）+ next/last_run_at。
- 调度：注册到 `internal/pkg/cron` TaskManager（5 段补秒段转 6 段；time_zone 前缀
  `CRON_TZ=`），触发回调以归属用户身份发起 draft 执行；`enabled` 触发时在 system scope
  复读复查（Disable 与 fire 竞态窗口）；last=触发时刻、next=其后下一次。
- 触发前置：runner 在线（EnsureOnline，离线跳过不落行——避免 created 堆积）、flow 归属 +
  scan_passed + writes 复查；并发闸满（ErrConcurrencyGate）视为本次跳过。
- 重启恢复：RestoreAll 注册全部 enabled 触发器。
- 前置校验：cron 表达式 6 段解析器预检；writes 非空的流程拒绝挂 cron（fail-close）。

### R2 aiagent `egolite_run` 工具（S11）

- 参数：`flow_id`（draft 通道）/ `asset_id`（market 通道）二选一必填；可选 `params`（对象）、
  `space_name`、`timeout_sec`。
- 行为：注入 runner（SetEgoLiteRunner，router 装配层接线 EnsureOnline + Start），
  **异步派发**（执行可达数分钟，不阻塞工具调用；recover 兜底），返回 execution id/state，
  进度用既有执行查询接口。
- 风险级 RiskHighWrite（同 browser_open_task：触发的是真实浏览器动作）。

### R3 runner 自更新（C10 余量）

- conf `repo_dir` 可选；upgrade 帧 → 先官方 `ego-browser upgrade`，若 repo_dir 配置：
  `git -C <repo_dir> pull --ff-only` + `go build -o <当前二进制> ./cmd/ego-runner`。
- 结果（成功/失败/跳过原因）统一 `runner` 事件回传；**重启生效**（运行中进程不被替换）。
- 未配置 repo_dir ⇒ 只做 ego-browser 升级（现状行为，零回归）。

## 三、验收标准

1. `go build ./...` + 四包 `go test` 全绿（新增：flow cron 服务测试、tooluse 工具测试、
   runner 自更新单测）。
2. 新表登记三处齐：model/egolite.go、migrate.go allModels、module_tables.go egoLiteOwnedTables
   （前缀匹配测试自动覆盖）。
3. writes 非空流程挂 cron 被拒；enabled:false 显式创建不被 GORM 零值陷阱翻转为 true。
4. 前端 Flows.vue 可视化挂 cron（eslint 零告警）。
5. 真机：handoff→takeOverTaskSpace 结果如实记录（成功或"需真人"均为有效结论）。
