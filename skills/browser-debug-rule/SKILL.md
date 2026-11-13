---
name: browser-debug-rule
description: 全局强制规则。任何涉及浏览器调试、浏览器自动化、页面验证、截图取证、console/network/性能排查的任务，在调用任何浏览器工具（ego-browser / browser-harness / chrome-devtools MCP / playwright MCP / agent-browser）之前必须先加载本 skill 并严格遵守。主方案是 ego lite（ego-browser，2026-10-26 起生效），禁止用独立 profile、全新临时浏览器或重新登录来绕过。
---

# 浏览器规则全局（强制）— 主方案：ego lite

## 唯一目标

所有浏览器调试/自动化/验证任务，**必须复用用户已登录的真实浏览器状态**。
用户不重新登录，agent 不新建独立 profile。

ego lite 天然满足这一点：agent 在自己的 isolated Space 内工作（不抢用户当前标签页），
但直接复用 ego lite 的用户登录态，无需任何 CDP 配置或重新登录。

## 第一步（必做）

```bash
ego-browser skill
```

按其输出指南执行（指南末尾有 `[ego-browser:skill] end of guide` 标记验证完整性，
输出被截断时先落盘再分段读取）。之后把整段业务操作写成**一个 Node.js 脚本一次交付**：
`ego-browser nodejs <<'EOF' ... EOF`，内置 helper（snapshotText / click / fill / js / cdp / wait 等）可直接调用。

## 工具优先级（自上而下）

1. **普通公开页面抓取** → `curl` / webfetch，不启动浏览器
2. **一切需要登录态、点击、表单、JS 渲染、截图、登录后验证的任务 → ego-browser（主方案）**
3. **ego-browser 不可用或故障时的回退链路（见下"回退链路"）** → `browser-harness` / `chrome-devtools` MCP
4. **只在需要跨浏览器（Firefox/WebKit）、network mock/route、或完全干净一次性上下文且用户明确同意时用 `playwright` MCP**
5. `agent-browser` 仅用于用户明确要求的多 agent 并行/域名白名单场景

## 硬性禁止

- ❌ 禁止要求用户重新登录；看到登录页先判断是会话过期而非环境错误，并如实报告
- ❌ 禁止用 `--isolated`、`--user-data-dir=/tmp/...`、Playwright 默认全新 profile 来做需要登录态的调试
- ❌ 禁止在未获用户明确同意时执行：退出登录、清除 cookie/站点数据、清除 ego lite/Chrome profile 数据（注意：部分清除操作会波及整个浏览器 profile，清缓存前必读 ego-browser skill 的 `references/clearing-state.md`）
- ❌ 禁止擅自改动 ego lite 安装/skill 链接、CDP 端口、profile 路径、MCP 配置；如需变更先说明理由并征得同意

## ego-browser 健康检查（失败时的诊断顺序）

1. `command -v ego-browser` 是否在 PATH（`~/.local/bin`）
2. `ego-browser nodejs` echo 测试是否输出 `ego-browser ready`
3. `ego-browser skill` 是否能输出完整使用指南
4. 仍失败 → `ego-browser upgrade` 升级运行时（官方指引）；再失败才走下方回退链路并如实向用户报告

## 回退链路（仅 ego-browser 故障时使用；参数保留、非常规路径）

> 下表是**本机个人配置示例**（路径/端口/LaunchAgent 随部署而异），换机按实际部署修改，勿照抄。

| 项 | 值 |
|---|---|
| Profile 目录 | `/Users/xiaofang/chrome-main-profile` |
| CDP 端点 | `http://127.0.0.1:9223` |
| browser-harness | 环境变量 `BU_CDP_URL=http://127.0.0.1:9223`（已在 `~/.zshenv`） |
| chrome-devtools MCP | 已配置 `--browserUrl http://127.0.0.1:9223` |
| 开机守护 | LaunchAgent `com.xiaofang.chrome-main-debug` |

回退时的连接失败处理（按顺序）：`curl -s http://127.0.0.1:9223/json/version` → 不通则
`chrome-main`（alias）重启主浏览器 → 检查 `launchctl list | grep chrome-main-debug` 与日志
`~/.config/chrome-main-debug.log` → 都失败才报告，不得降级到其他浏览器/profile 继续执行。

## 验收标准

任何"调试生效"的结论必须附证据：
- ego-browser：脚本输出了快照文本 / 页面内容 / 截图路径等真实页面证据，而非仅命令无报错
- 涉及登录态时：读到实际认证凭据（如 `localStorage` 中的 token 存在）或登录后页面内容
- 回退链路时：harness `list_tabs()` 返回至少一个真实业务页面，或 chrome-devtools `list_pages()` 能列出用户正在用的标签页
