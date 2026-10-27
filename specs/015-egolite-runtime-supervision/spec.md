# Feature Specification: ego-lite 运行时监督与收口杂项（specs/014 改进清单 C2/C4/C7/C10/C16/C17 可代码化集）

**Feature Branch**: `015-egolite-runtime-supervision`

**Created**: 2026-10-28

**Status**: Approved

**Input**: 用户指令「完成文档规划的代码开发，一次性全部完成」——即
`internal-docs/ego-lite-接入评估与改进清单-2026-10-28.md` §三 C 组中**可代码化**的全部项。

## 一、范围裁决（哪些进本轮，哪些不进并说明）

| 改进清单项 | 裁决 | 理由 |
|---|---|---|
| C2 运行时收敛（R4/T13） | **进（可实现子集）** | 三层落地：① 执行前二次静扫（扫描门 runner 侧镜像）② 注入域守卫 helpers（协作层）③ 事件 URL 域审计（强制层，fail-close）。完整 JS 沙箱需 ego 官方配合（内嵌 Node 的 require/import 无法从 runner 拦截），残余诚实登记 |
| C4 契约进前端 | **进** | Flows.vue 编辑器 + Assets.vue 运行面板提示 |
| C7 安装器 R2 | **进（macOS）** | install.sh/uninstall.sh/README；Windows（C11）另轮 |
| C10 runner 升级指令 | **进** | admin 广播 upgrade 帧 → runner 执行官方 `ego-browser upgrade` → 结果事件回传（能力升级归 ego 官方，平台只触发与监督） |
| C16 事件协议 lint | **进** | EGO-R032 warn：全文无结构化事件输出（软契约最低要求） |
| C17 ego_server_name 用户面 | **进** | 安装引导页 conf 模板补可选行 |
| C1 handoff→resume 真机验证 | **部分进** | Space 跨进程同名续跑已真机验证（spaceId/页面状态持久）；handOff 人工环节需真人配合 ego UI，保持 P1 登记 |
| C3 G-E7 法律评估 | 不进（外部动作） | 非代码 |
| C5/C6 签名链/平台包通道 | 不进 | 需 platform-server 联动决策，代码已备 |
| C8/C9/C11/C13/C15/C18 | 不进 | 平台契约/基础设施/Windows/前端表单/SSE/压测，按清单排期 |

## 二、需求

### R1 运行时域监督三层（C2 可实现子集）

1. **执行前二次静扫**（assemble 时，fail-close）：entry 原文命中 `runnerEntryBlockers`
   （EGO-R001/002/003/004/010/011/013 的 runner 侧镜像）即拒绝执行。只扫 entry——
   preamble 是 runner 自产受信代码（confirm 契约用 `import("node:fs/promises")`）。
2. **域守卫 helpers**（协作层注入）：`__DOMAINS__` 常量 + `__domainAllowed__(url)` +
   `__gotoChecked__(page,url)` + `__fetchChecked__(page,url,opts)`（越域抛错）。
   语义与 Go 侧同源：整站 `*` 放行；精确域/子域命中；`www.` 归一；零域全拒。
3. **事件 URL 域审计**（强制层）：事件 payload 带 `url` 且越域 ⇒ runner 发
   `error` 事件 + cancel 终止执行（fail-close）。零域单跳过（该层未启用，避免误杀
   老资产；服务端保存时 domains 必填互补）。

### R2 服务端配套

- `dispatch` 向 task spec 下发 `domains`（draft←flow.DomainsJSON；market←资产目录，
  下发时刻快照，版本钉扎同语义）。
- `POST /api/egolite/admin/runner/upgrade`（RequireAdminMiddleware）：向全部在线 runner
  广播 `{"type":"upgrade"}`，返回送达数。
- 扫描门 EGO-R032（warn）：全文无 `console.log(JSON.stringify(...))` → 事件流将为空。

### R3 安装器（macOS）

`scripts/ego-runner/install.sh`（编译→conf 不覆盖→LaunchAgent RunAtLoad+KeepAlive→--check）、
`uninstall.sh`（conf/日志保留）、README（conf 全键说明含 `ego_server_name`）。

### R4 前端契约提示（C4/C17）

- Flows.vue 编辑器：脚本契约 alert（常量/`__awaitConfirm__` 用法/**不要 process.exit**/
  域守卫 helpers/事件输出要求）。
- Assets.vue 运行面板：写确认 + 域白名单口径。
- EgoRunnerInstallGuide.vue：conf 模板补 `ego_server_name` 可选行 + install.sh 指引 +
  官方升级说明。

## 三、验收标准

1. `go build ./...` + 四包 `go test` 全绿（含新增 8 组：runner 域/审计/预检/upgrade argv
   6 组 + 服务端 parseDomains/EGO-R032 2 组）。
2. 三个 Vue 文件 eslint 零 error 零 warning。
3. install.sh/uninstall.sh `bash -n` 通过。
4. 真机：同名 space 跨进程续跑（spaceId 一致 + 页面状态持久 + finish 收口）。
5. 契约冲突清零：`__awaitConfirm__` 契约不再依赖 `process.exit`（EGO-R013 禁止）。

## 四、非范围

见 §一裁决表"不进"行；完整 JS 沙箱（require/import 运行时拦截）需 ego 官方
能力面配合，登记为长期项；Windows 安装包（C11）另轮。
