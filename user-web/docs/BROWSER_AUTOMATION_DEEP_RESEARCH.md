# 浏览器自动化 — 深度调研报告

> 调研日期：2026-09-08 ｜ 对技术方案中所有关键假设做实证验证和补充

---

## 0. 调研目录

| 章节 | 问题 | 核心结论 |
|------|------|----------|
| **1** | Go 能否直接实现 Chrome Native Messaging Host？ | ✅ 能。发现成熟 Go/Rust 库。**修正之前文档的 Python Host 假设** |
| **2** | Chrome MV3 service worker 30s 超时怎么处理？ | 3 种对策：offscreen documents / Active Port / Alarm 切分 |
| **3** | BrowserAgentBridge 完整架构 + 原语集 | 它走 WebSocket + FastAPI，不走 Native Messaging；完整原语 11 种 |
| **4** | agent-browser (vercel-labs) accessibility snapshot 能否借鉴？ | ✅ @e1 ref 方案可大幅提升 Brain 层准确率 + 省 token |
| **5** | Native Messaging 协议帧格式精确验证 | 小端 vs 大端有坑！Chrome 官方文档写 "native byte order" |
| **6** | Native Host 进程的真实生命周期 | Chrome 管理生命周期，Go 端需要 event_loop 模式 |
| **7** | 多扩展 / 多 Native Host 共存 | 不同 host name 互不干扰；多 Agent 需自行互斥 |
| **8** | 方案 A 修正：Go 直接做 Host，省掉 Python | 架构简化：Go Hand → 直接 Native Messaging → Chrome 扩展 |

---

## 1. Go 直接实现 Native Messaging Host ✅ 可行

### 1.1 发现

| 项目 | 语言 | 成熟度 | 说明 |
|------|------|--------|------|
| `github.com/al-shahreyaj/sample-go-app` | Go | Demo | 完整 mininal Go Native Host：stdin 读 4 字节长度 + JSON，stdout 写 response |
| `github.com/rickypc/native-messaging-host` | Go | ⭐⭐⭐⭐ 成熟 | 完整库：framing、manifest install、auto-update、跨平台 |
| `github.com/sashahilton00/native-messaging-host` | Go | ⭐⭐⭐⭐ 成熟 | 另一个 Go Native Messaging host 库 |
| `github.com/Arthur-Ficial/fenster` | Go + Rust | 深度调研项目 | 详细对比 Go vs Rust 做 NM host |
| `crates.io/native_messaging` | Rust v0.3.0 | ⭐⭐⭐⭐⭐ 最成熟 | Tokio async、manifest installer、跨浏览器支持（Chrome/Firefox/Edge/Brave/Vivaldi） |

### 1.2 最小 Go 实现（~60 行）

```go
package main

import (
    "encoding/binary"
    "encoding/json"
    "io"
    "os"
)

func main() {
    for {
        // 1. 读 4 字节长度头
        var length uint32
        if err := binary.Read(os.Stdin, binary.LittleEndian, &length); err != nil {
            if err == io.EOF {
                return // Chrome 关闭了 Native Host 通道
            }
            return
        }
        if length > 64*1024*1024 { // Chrome 限制 64MiB
            return
        }

        // 2. 读消息体
        buf := make([]byte, length)
        if _, err := io.ReadFull(os.Stdin, buf); err != nil {
            return
        }

        // 3. 解析 JSON
        var msg map[string]any
        json.Unmarshal(buf, &msg)

        // 4. 处理（转发到 Go Hand 的业务逻辑）
        response := handleCommand(msg)

        // 5. 写 response
        respJSON, _ := json.Marshal(response)
        binary.Write(os.Stdout, binary.LittleEndian, uint32(len(respJSON)))
        os.Stdout.Write(respJSON)
        os.Stdout.Flush()
    }
}
```

### 1.3 关键坑

| # | 坑 | 对策 |
|---|-----|------|
| 1 | **日志不能用 stdout** | Chrome 把 stdout 当协议通道，`println!` 会损坏帧。所有日志走 stderr 或文件 |
| 2 | **Chrome 限制 Native Host 消息大小** | 扩展→Host 64 MiB；Host→扩展 1 MiB。Rust crate 默认强制 1 MiB |
| 3 | **Chrome 自动管理 Native Host 生命周期** | 扩展 `connectNative()` 时 Chrome fork host 进程；扩展断开时 Chrome 杀进程。Go Host 不要自己 daemonize |
| 4 | **进程异常退出后 Chrome 自动重启** | Chrome 会把 Native Host 当成可重启的服务；Go Host 内部不要缓存非持久化状态 |

### 1.4 结论

**Go 可以直接实现 Native Messaging Host。省掉 Python 中间层是可行的、推荐的。** 架构简化为：

```
Go Hand (browser_automation_hand.go) ──(fork/exec Go NM Host)──▶ Chrome 扩展
                                                                    │
                                                                    ▼
                                                              用户主 Chrome Profile
```

不需要 Python，不需要 HTTP/WS 本地端口，整条链路是进程内 stdio pipe。

---

## 2. Chrome MV3 Service Worker 30s 超时对策

### 2.1 问题本质

Chrome MV3 service worker 有严格的生命周期限制：
- **30 秒 idle timeout**：无事件时自动终止，内存状态丢失
- **5 分钟 per-event limit**：单个事件 handler 最长 5 分钟
- 这和我们需要的 "执行 8 步原语"（可能几十秒）冲突

### 2.2 三种对策

| # | 对策 | 原理 | 适用场景 |
|---|------|------|----------|
| 1 | **Active Port Connection** | Native Messaging 的 `port` 对象本身就是一个长连接；Chrome 会保持 SW 活跃只要 port 还在 | **我们的场景！** Go Host ↔ Extension 的 port 连接天然保活 |
| 2 | **Offscreen Documents** | `chrome.offscreen.createDocument()` 创建不可见 HTML 文档；可使用 `setInterval`、Worker，不受 SW 超时限制 | 长任务（数据处理、持续计算） |
| 3 | **Alarm-based Chunking** | 把长任务切分，每块 < 30s，用 `chrome.alarms` 调度下一块 | 分钟级任务（但 alarms 最小 1 分钟间隔） |

### 2.3 关键发现：Active Port Connection 已经解决我们的问题

Chrome 官方文档确认：

> "As long as the port remains open, Chrome will keep the service worker alive."

我们的扩展与 Go Native Host 之间的 `chrome.runtime.connectNative()` 建立的 port 本身就是一个持久连接。**只要 Go Host 不退出、port 不关闭，service worker 不会被杀。**

所以 MV3 30s 超时对我们的方案影响很小——我们的核心通信通道（Native Messaging port）天然保活 SW。

### 2.4 offscreen document 备用方案

万一 Go Host 意外退出导致 port 断开，扩展可以启动 offscreen document 做恢复：

```js
// background.js
async function ensureRecovery() {
  // Go Host 断开时触发
  if (!port) {
    await chrome.offscreen.createDocument({
      url: 'offscreen.html',
      reasons: ['WORKERS'],
      justification: 'Maintain native messaging connection',
    });
  }
}
```

offscreen document 的限制：**只有 `chrome.runtime` API 可用**（不能直接用 `chrome.tabs` / `chrome.scripting`），所以 offscreen 只负责重连 Native Messaging，实际 tab 操作还是要发消息回 background SW。

---

## 3. BrowserAgentBridge 完整架构调研

### 3.1 确认：它走 WebSocket，不走 Native Messaging

BrowserAgentBridge（Alij93695 版）的架构：

```
外部 Agent (任何语言) ──HTTP POST──▶ FastAPI daemon (localhost:1313)
                                        │
                                        ▼ WebSocket
                                  Chrome MV3 扩展
                                        │
                                        ▼
                                  用户主 Chrome Profile
```

这是它和我们方案 A 的核心差异：

| 维度 | BrowserAgentBridge | 我们的方案 A |
|------|-------------------|-------------|
| 通信协议 | WebSocket (daemon ↔ 扩展) | Native Messaging (extension ↔ Go Host) |
| 守护进程 | Python FastAPI daemon | Go Host (Chrome 管理生命周期) |
| 权限 | 需要 "On all sites" | 最小化 tabs + scripting |
| 授权 | 扩展安装时一次授予 | 同（NM 清单 allowed_origins 白名单） |
| 后台 tab | ✅ new_tab 支持 active:false | ✅ |
| 原语数量 | 11 种 | 我们 13 种 |
| 外部 Agent 接入 | REST API POST | MCP 协议 |
| LLM 大脑 | ❌ 无 | ✅ 我们有 |

### 3.2 BrowserAgentBridge 完整原语集

| Action | 参数 | 说明 | 我们方案是否覆盖 |
|--------|------|------|------------------|
| `new_tab` | url, active=false | 开新 tab | ✅ `open_tab` |
| `close_tab` | tab_id | 关 tab | ✅ `close_tab` |
| `click` | selector | 点击 | ✅ |
| `fill` | selector, value | 填表单 | ✅ `type` |
| `type` | selector, value | 逐字输入（带键盘事件） | ✅ `type` + `submit_on_enter` |
| `scroll` | direction, amount | 滚动 | ✅ |
| `screenshot` | path, full_page | 截图 | ✅ |
| `markdown` | tab_id | 页面转 Markdown（给 LLM 吃） | ⚠️ 我们用 `get_html` + Brain 自己解析 |
| `wait` | condition | 等待 load/visible/gone | ✅ |
| `evaluate` | script | 注入 JS | ✅ |
| `get_html` | selector | 获取 HTML | ✅ |

### 3.3 值得借鉴

| BrowserAgentBridge 特性 | 借鉴到我们方案 |
|-------------------------|---------------|
| `markdown` action | Brain 层消费前把 HTML 转 Markdown（更省 token） |
| REST API 暴露 | 除了 MCP，也暴露 HTTP REST 方便非 MCP Agent 接入 |
| extension 的 "On all sites" 权限要求 | 我们的 tabs + scripting 权限在大多数站点够用，但某些 iframe 场景可能需要 host_permissions |

---

## 4. agent-browser (vercel-labs) accessibility snapshot 借鉴

### 4.1 @e1 / @e2 ref 方案

agent-browser 的核心创新：**把 DOM 变成带编号的引用 refs**，给 LLM 用。

```
$ agent-browser open https://example.com/form
$ agent-browser snapshot -i

@e1 [input type="email" placeholder="Email"]
@e2 [input type="password" placeholder="Password"]
@e3 [button] "Submit"
@e4 [a] "Forgot password?"
```

LLM 拿到的不是原始 HTML（几十 KB），而是这种 2-5 KB 的紧凑引用表。然后 LLM 决策 `click @e3`、`fill @e1 user@x.com`，不用猜 selector。

### 4.2 对比 CSS selector 方案

| 维度 | CSS selector（我们原方案） | accessibility @ref |
|------|--------------------------|---------------------|
| LLM 理解难度 | 高 — 需要生成正确的 CSS selector | 低 — 直接说 "click @e3" |
| Token 成本 | 高 — 要传 selector + 可能失败重试 | 低 — 2-5 KB ref 表 |
| 稳定性 | 中 — 页面改版可能让 selector 失效 | 高 — ref 由 snapshot 实时生成 |
| 我们方案改造量 | — | Brain 层 prompt 模板改 + Translator 加 snapshot 步骤 |

### 4.3 结论：**必须引入 accessibility snapshot**

Brain 层不再给原始 HTML，而是先做 accessibility snapshot 生成 @ref 表，再喂给 LLM。plan 里的 selector 变成 ref，Translator 执行时再把 ref 转回真实 DOM 元素。

**改造点**：
1. Go Hand 层新增 `snapshot` 原语（调 `chrome.scripting.executeScript` 注入 accessibility tree 提取脚本）
2. Brain prompt 改为吃 snapshot 输出
3. Plan schema 里的 `selector` 字段改为 `ref`（如 `@e3`）或 `selector`（兼容）
4. Translator 执行时 ref → 真实 DOM 元素定位

---

## 5. Native Messaging 协议帧格式精确验证

### 5.1 Chrome 官方文档

Chrome Native Messaging 官方文档：

> "The message size is 4 bytes, followed by the JSON message. The size of the message is in **native byte order**."

### 5.2 中文 CSDN 文章证实用小端

```go
// CSDN 2025 年文章（Java 实现）
int messageLength = ByteBuffer.wrap(lengthBytes)
    .order(ByteOrder.LITTLE_ENDIAN)
    .getInt();
```

### 5.3 Rust crate 证实用小端

```rust
// native_messaging crate v0.3.0
let mut len_buf = [0u8; 4];
stdin.read_exact(&mut len_buf)?;
let length = u32::from_le_bytes(len_buf);  // 小端
```

### 5.4 我们方案修正

**之前技术方案写的是 "大端"（我脑补错了），实际 Chrome 用小端 Little Endian。** 这是关键协议细节，错了会导致 Go Host 收到的消息长度完全不对。

修正为：`binary.LittleEndian`（不是 `binary.BigEndian`）。

### 5.5 协议帧总览

```
┌──────────────────────────────┐
│ 4 字节 uint32 小端长度头      │ ← 消息体的字节数
├──────────────────────────────┤
│ UTF-8 JSON 消息体              │
│ Chrome → Host: ≤ 64 MiB       │
│ Host → Chrome: ≤ 1 MiB        │
└──────────────────────────────┘
```

---

## 6. Native Host 真实生命周期

### 6.1 Chrome 管理一切

```
扩展 background.js
  │
  │ const port = chrome.runtime.connectNative('com.hivemtk.browser')
  │
  ▼
Chrome 内部启动 native host 子进程
  │
  ├─ stdin  ← Chrome 写的 frame
  ├─ stdout → Chrome 读 response
  │
  │ Go Host 正常运行
  │
  │ 以下任一情况 Chrome 杀 host 进程：
  │   1. 扩展关闭 port (port.disconnect())
  │   2. Chrome 关闭/重启
  │   3. host 向 stdout 写了非 frame 数据（损坏协议）
  │   4. host 崩溃
```

### 6.2 Go Host 应该是 event_loop 模式

```go
// 正确模式
func main() {
    for {
        msg := readFrame()
        if msg == nil {
            return // Chrome 关闭了通道，进程退出
        }
        resp := dispatch(msg)  // 转发到业务逻辑
        writeFrame(resp)
    }
}

// 错误模式（会导致扩展通信超时）
func main() {
    msg := readFrame()
    resp := dispatch(msg)
    writeFrame(resp)
    return // 一次就退出，Chrome 无法维持长连接
}
```

### 6.3 Chrome 重启后的自动恢复

| 组件 | Chrome 重启后行为 | 我们方案的处理 |
|------|-----------------|---------------|
| 扩展 | 随 Chrome 自动加载，不需手动 reload | ✅ MV3 扩展原生支持 |
| Native Host | Chrome 按需 fork 新进程 | ✅ Go Host event_loop 等待 |
| Go 后端 Hand 层 | 需要检测连接丢失并恢复 | `EnsureConnected()` 重试 + 指数退避 |

---

## 7. 多扩展 / 多 Native Host 共存

### 7.1 不同 host name 完全隔离

manifest.json 里 `name` 字段唯一：

```json
// BrowserAgentBridge 用的是
{"name": "com.browseragentbridge.host"}

// 我们用
{"name": "com.hivemtk.browser"}
```

Chrome 按 name 区分不同 Native Host，互不干扰。

### 7.2 多 Agent 并发访问同一个扩展

问题：多个 Agent 同时通过同一个 Go Hand → 同一个 Native Host → 同一个扩展发命令。

```
Agent A (Claude Code) ──┐
                         ├──▶ Go Hand (单实例) ──▶ Native Host ──▶ 扩展 ──▶ Chrome
Agent B (Codex) ────────┘
```

**扩展单线程执行** — 同一时刻只能处理一个脚本注入，两个命令并发发送会竞争 tab。

### 7.3 对策

Go Hand 层做命令队列：

```go
type BrowserHand struct {
    cmdQueue chan command      // 缓冲队列
    mutex    sync.Mutex        // 单实例互斥
    native   *nativeHostClient
}

func (h *BrowserHand) Click(tabID int, selector string) error {
    h.mutex.Lock()
    defer h.mutex.Unlock()
    // 严格串行执行
    return h.native.send("click", map[string]any{...})
}
```

---

## 8. 架构修正汇总（方案 A 更新）

### 8.1 之前方案（错）

```
Go Hand ──(HTTP/WS)──▶ Python Host ──(Native Messaging)──▶ Chrome 扩展
                      为什么需要 Python？之前以为 Go 不能做 NM Host
```

### 8.2 修正后方案（对）

```
┌────────────────────────────────────────────────────────────┐
│  Go Hand Layer (browser_automation_hand.go)                │
│                                                            │
│  ✅ 直接实现 Chrome Native Messaging 协议                   │
│  ✅ 4 字节 Little Endian 长度头 + UTF-8 JSON               │
│  ✅ event_loop 模式（for { read → dispatch → write }）     │
│  ✅ 单实例 mutex 解决多 Agent 并发                          │
│                                                            │
│  关键依赖：go.mod 里加 github.com/rickypc/native-messaging │
│  或自己实现 ~60 行 framing                                  │
└────────────────────────────────────────────────────────────┘
                              │ Native Messaging stdio
                              ▼
┌────────────────────────────────────────────────────────────┐
│  Chrome MV3 扩展 (~300 行 JS)                              │
│                                                            │
│  ✅ 权限最小化：tabs + scripting                            │
│  ✅ createBackgroundTab {active:false}                     │
│  ✅ Native Messaging port 天然保活 SW（不需要 offscreen）    │
│  ✅ accessibility snapshot 新增 @ref 生成                   │
└────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌────────────────────────────────────────────────────────────┐
│  用户主 Chrome Profile（完整 Cookie/登录态）                │
└────────────────────────────────────────────────────────────┘
```

### 8.3 去掉 Python Host，得到什么

| 好处 | 数量级 |
|------|--------|
| 架构层数 | 减一层（Go → Extension，不是 Go → Python → Extension） |
| 部署复杂度 | 减（不用装 Python + daemon，只用 Go binary） |
| 故障点 | 减（Python Host 崩溃概率 → 0） |
| 性能 | 更好（进程内 stdio pipe vs 可能的 HTTP/WS 端口） |
| manifest 安装 | 简化（只需要 Go binary 的 path） |
| 跨平台 | Go binary 单文件，比 Python + 依赖包方便 |

### 8.4 accessibility snapshot 引入后的变化

| 层 | 之前 | 现在 |
|----|------|------|
| Brain 输入 | 原始 HTML（几十 KB） | accessibility snapshot @ref 表（2-5 KB） |
| Brain 输出 | `{action:"click", params:{selector:"#submit"}}` | `{action:"click", params:{ref:"@e3"}}` |
| Translator | selector → DOM querySelector | ref → 定位 → 执行 |
| Go Hand | 13 种原语 | + `snapshot` 原语 |
| Token 成本 | 高 | 降 80%+ |

---

## 附录 A：关键技术假设验证矩阵

| # | 假设 | 验证方式 | 结果 | 影响 |
|---|------|----------|------|------|
| 1 | Go 可以做 Native Messaging Host | 搜 GitHub + Rust crate 文档 | ✅ 成熟库可用 | **架构简化** |
| 2 | Chrome 协议帧用大端 | Chrome 官方文档 + Rust crate | ❌ 用**小端** | **协议修正** |
| 3 | MV3 30s 超时会杀掉我们的扩展 | Chrome 官方 Port 保活文档 | ❌ Native Messaging port 天然保活 | 不需要 offscreen 备用 |
| 4 | BrowserAgentBridge 走 Native Messaging | 读它的 README + 看源码 | ❌ 走 WebSocket + FastAPI | 架构不同，但原语集可借鉴 |
| 5 | accessibility snapshot 能省 token | agent-browser SKILL.md | ✅ 2-5 KB vs 几十 KB | Brain 层必须引入 |
| 6 | Host 退出后 Chrome 自动重启 | Chrome 官方 Native Messaging 生命周期 | ✅ Chrome fork 新进程 | Go Host 不需 daemonize |
| 7 | 多扩展共享主 Profile | Chrome 官方文档 | ✅ 不同 extension id 隔离但共用 Profile | 多方案共存无冲突 |

## 附录 B：下一步需要实证验证的问题

| # | 问题 | 验证方法 | 阻塞级别 |
|---|------|----------|----------|
| 1 | Go 实现的 Native Host 在实际 Chrome 下能否正确收发？ | 写 60 行 demo Go Host + 最简扩展，跑 ping/pong | 🔴 高（MVP 阻塞） |
| 2 | `chrome.scripting.executeScript` 注入 accessibility snapshot 脚本的性能？ | 对典型企业后台（~500 交互元素）测试耗时 | 🟡 中 |
| 3 | 扩展 `active:false` 创建的 tab 是否真的不抢焦点？ | 手动测试多个网站 | 🟡 中（关键硬约束） |
| 4 | 扩展后台执行 click 时，某些 SPA 页面是否能正确触发事件？ | 测试 React/Vue 受控组件的 click 行为 | 🟡 中 |
| 5 | 多个 Agent 同时通过同一个 Go Hand 发命令时，Chrome 扩展是否真的串行处理？ | 并发测试 + 抓时间戳 | 🟢 低 |
