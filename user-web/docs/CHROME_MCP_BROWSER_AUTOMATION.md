# Chrome MCP 浏览器自动化方案

> 目标：寄生在日常主 Chrome Profile 里，后台 tab 载入、无弹窗、多 Agent 共享会话

---

## 核心痛点（硬约束）

市面上绝大多数 MCP 浏览器工具都踩坑的 5 条硬约束：

1. **必须使用日常主 Chrome Profile，继承全部 Cookie / 登录态 / 扩展**，不能新建独立用户目录
2. **在现有 Chrome 主窗口内新建标签页，后台载入、不激活、不抢焦点**（拒绝新开独立自动化窗口）
3. **免手动授权、开机 / 重启 Chrome 后自动连接、断线自动重连**，不要再点 Allow
4. **多个 Agent（Claude Code / Codex / OpenCode / Hermes）共享同一个浏览器会话**，互不冲突
5. 不追求无头、不搞隔离浏览器，要接近官方 ChatGPT Chrome 扩展那种 "寄生在你正在用的浏览器里" 的体验

> **现状结论**：没有一个成品项目完美集齐全部特性并成熟稳定。两条可行路线——**Native Messaging 自制小网关（长期可靠首选）** vs **挑选现有项目做补丁改造（快速上手）**。所有基于 CDP WebSocket 的方案（Chrome DevTools MCP、大部分 playwright-based MCP）天生卡在**授权弹窗问题**，很难彻底根除，这是 Chrome 的安全限制，不是代码 bug。

---

## 为什么你试过的工具全都差一口气

### 1. CDP 远程调试（--remote-debugging-port）类（Chrome DevTools MCP / real-browser-mcp / Playwright Extension）

- **优点**：简单、MCP 生态成熟
- **致命短板**：
  - 主 Profile 打开 CDP，每次重启 Chrome 后第一次连接**必然弹出授权确认**，没有永久免确认办法
  - 很多实现新建 tab 后调用 `activate()`，强行切焦点；即使传 `active:false`，部分版本 CDP 创建标签页依然短暂抢焦点
  - 多 Agent 并发操作同一个 CDP 会话容易竞争、标签页管理混乱
- **本质限制**：CDP 是远程调试协议，属于外部调试器，Chrome 对外部调试器有明确的用户确认保护，无法彻底绕过。这就是一直被弹窗折磨的根源。

### 2. OpenCLI / BrowserSkill

走扩展注入 + 后台 service worker，但是架构是**启动一个专属自动化窗口**，不是附着在正在用的现有窗口里开后台 tab，不符合窗口约束。

### 3. chrome-use / Open Browser Use（Native Messaging + Chrome Extension）

架构是目前最贴近需求的路线：**扩展作为宿主，Native 程序和扩展之间走 Native Messaging，不是外部 CDP**。

- **优势**：
  - ✅ 复用主配置文件、全部 Cookie、扩展环境
  - ✅ 扩展在浏览器内部创建 tab，可以精确控制 `active: false`，真正后台加载，不抢焦点
  - ✅ 权限是**扩展安装时一次性授予**，不会每次重连弹出 Allow，重启浏览器自动可用
- **缺点**：项目新、MCP 封装简陋、多 Agent 会话管理薄弱、异常崩溃恢复差，直接上主力浏览器风险高
- **但架构方向是正确答案**，比 CDP 路线更适合目标

> **关键区分**：
> - CDP = 外部调试器 → 弹窗枷锁，无解
> - Native Messaging + 自家 Chrome 扩展 = 浏览器内部可信组件，一次授权永久生效，能精细控制后台标签页

---

## 方案 A：自建极简 Native-Messaging MCP 网关（推荐，长期稳定最终路线）

不要直接 fork chrome-use 完整大项目，剥离出最小骨架，自己维护一层薄网关。

### 整体架构

```
Agent(Claude Code/Codex/OpenCode) ←→ MCP 网关(本地Node/Python) ←→ Native Messaging ←→ 自制轻量Chrome扩展
```

### 1. Chrome 扩展（仅几百行）

- 权限：`tabs`、`scripting`，**不需要 host 全匹配高危权限**
- API：提供一条指令 `createBackgroundTab(url)`，调用 `chrome.tabs.create({active:false, windowId: 当前主窗口ID})`
- 维护标签页列表、页面获取 HTML、注入脚本、等待加载，不要做复杂 AI 规划
- 扩展安装一次，Native Messaging 清单放到 `~/Library/Application Support/Google/Chrome/NativeMessagingHosts/`，**永久授权，不再弹窗**

### 2. 本地 Native 主机程序

- 很小，只做 stdio 和扩展消息转发，不内置大模型逻辑
- 维持长连接，Chrome 重启后自动重连、断线重试、消息队列，解决多 Agent 并发争抢

### 3. MCP 薄层封装

- 对外暴露 MCP tools：打开后台标签、获取页面内容、点击、输入、截图
- 多 Agent 共用同一个浏览器会话，做简单锁 / 消息排队，避免并发操作互相打断

### 优点（完美命中全部需求）

- ✅ 主 Profile，完整 Cookie、登录态、扩展环境
- ✅ 在当前窗口创建后台 tab，`active:false`，完全不抢焦点
- ✅ 一次安装，重启 Chrome、重启电脑自动连接，无重复弹窗
- ✅ 多 Agent 共享会话，可控并发队列
- ✅ 自己掌握源码，不需要依赖更新不稳定的新项目

### 缺点

需要少量 JS + Python/Node 代码，不是开箱即用。但代码量很小，不需要浏览器自动化深坑。

> **原则**：不要让扩展承担 Agent 决策，扩展只做 "浏览器原语"，业务逻辑交给 MCP 层，这是稳定性关键。

---

## 方案 B：挑选现有项目做补丁改造（快速上手备选）

如果短期不想写代码，可在现有项目上打补丁：

| 项目 | 改造点 | 风险 |
|------|--------|------|
| chrome-use | 剥离扩展 + 替换 MCP 层 | 上游更新快，rebase 成本 |
| Open Browser Use | 改 extension 部分 | 同样依赖上游 |

**核心补丁思路**：直接复用其 Chrome 扩展 + Native Messaging 通信，替换掉 MCP 层包装为自家极简实现，跳过其重型依赖。

---

## 方案选型总结

| 维度 | CDP 路线 | 方案 A 自建网关 | 方案 B 改造 |
|------|----------|----------------|------------|
| 主 Profile | ✅ | ✅ | ✅ |
| 后台 tab 不抢焦点 | ⚠️ 有概率抢 | ✅ 精确控制 | ✅ |
| 重启 Chrome 后弹窗 | ❌ 必弹 | ✅ 无 | ✅ |
| 多 Agent 会话 | ⚠️ 竞争 | ✅ 队列 | ⚠️ 依赖上游 |
| 维护成本 | 低 | 中（自己维护几百行） | 高（rebase 上游） |
| 长期稳定性 | ❌ 弹窗无解 | ✅ 完全自主 | ⚠️ 依赖上游 |

**推荐**：重度使用 → 方案 A；短期快速试 → 方案 B 过渡。

---

## 实施步骤（方案 A）

1. **写 Chrome 扩展**（几百行 JS）
   - `manifest.json`：declarative manifest V3，权限最小化
   - `background.js`：Native Messaging 监听 + tabs API 封装
   - 核心 API：`createBackgroundTab(url)` / `getTabContent(tabId)` / `click(tabId, selector)` / `type(tabId, selector, text)` / `screenshot(tabId)`

2. **写 Native 主机程序**（Python / Node）
   - stdio 消息循环，JSON 行协议
   - 断线自动重连 + 消息队列（防 Chrome 重启时丢消息）
   - 多 Agent 互斥锁（同一时刻只有一个操作流入扩展）

3. **写 MCP Server**（Python 推荐 `mcp` SDK）
   - tools：`browser_open_tab` / `browser_get_content` / `browser_click` / `browser_type` / `browser_screenshot`
   - 超时保护：每个 tool 调用带超时，防止扩展卡死

4. **安装配置**
   - 扩展：开发者模式加载 unpacked extension
   - Native Messaging 清单：写入 `~/Library/Application Support/Google/Chrome/NativeMessagingHosts/com.hivemtk.browser.json`
   - MCP：在各 Agent 的 MCP config 里注册新 server

### Native Messaging 清单示例

```json
{
  "name": "com.hivemtk.browser",
  "description": "HiveMTK Browser Automation Host",
  "path": "/Users/xxx/.local/bin/hivemtk-browser-host",
  "type": "stdio",
  "allowed_origins": ["chrome-extension://xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx/"]
}
```
