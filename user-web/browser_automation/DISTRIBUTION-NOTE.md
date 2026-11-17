# DISTRIBUTION NOTE — HiveMTK Browser Automation 扩展权限披露

> 分发/上架前必读。本文件说明 `manifest.json` 中高敏感权限的**实际用途**；按各应用商店
> 政策（Chrome Web Store「Permissions justification」、Edge Add-ons、Firefox AMO 同类要求），
> 这些权限在**商店说明/隐私政策中必须逐条明示**，不得仅在代码里出现。

## 权限面与用途

### `host_permissions: ["<all_urls>"]`

本扩展是**本机自动化 Host**（寄生式后台 tab + Native Messaging 架构）：

- 自动化目标站由**用户在任务中指定**（在用户自己的浏览器里替用户操作任意网站），
  扩展在构建/分发时无法预知目标域，因此 host_permissions 收窄到固定清单不可行。
- `<all_urls>` 使 `chrome.scripting` 能向目标 tab 注入执行脚本、配合 `tabs` 权限
  查询/管理后台寄生 tab，从而实现对目标标签页的**全权控制**（读取页面状态、
  执行页面脚本、驱动交互）。
- 影响面：凡安装本扩展的浏览器，扩展可访问任意站点的 tab 内容。**这是设计使然**，
  也是本扩展与 HiveBridge（见下）在权限面上最大的差异点。

### `permissions: ["debugger"]`

- 用途：`chrome.debugger`（Chrome DevTools Protocol）注入**可信输入事件**
  （`Input.dispatchKeyEvent` / `Input.dispatchMouseEvent`，见 `src/core/cdp/input.js`）。
- 为什么必须用它：页面级 JS（`dispatchEvent`）产生的输入事件 `isTrusted === false`，
  目标站的前置风控（如抖音/闲鱼登录框、验证码控件）会拒绝合成事件；只有 CDP
  debugger 通道能产生浏览器级的可信输入。附带表现：attach 期间目标 tab 顶部
  会出现 Chrome 官方的「XX 正在调试此浏览器」提示条，这是浏览器自身的透明化
  设计，扩展无法也不应隐藏。

### `permissions: ["nativeMessaging", "tabs", "scripting", "storage"]`

- `nativeMessaging`：与本机自动化 Host 进程（Native Messaging host）通信，任务
  编排与执行结果在本地闭环，不经过第三方服务器。
- `tabs` / `scripting` / `storage`：寄生后台 tab 的创建与管理、页面注入、任务配置存取。

## 分发要求

1. **商店说明中必须明示**（逐条原文引用亦可）：
   - 「本扩展需要 `<all_urls>` 主机权限以在您指定的目标网站上执行自动化任务」；
   - 「本扩展使用 `debugger` 权限注入可信键盘/鼠标事件，启用期间浏览器会显示调试提示条」；
   - 「自动化目标站由您（用户）自行指定，扩展厂商不预设、不收集目标站数据」。
2. 隐私政策中说明数据流向：任务数据走本机 Native Messaging 与用户自建的
   HiveMTK user-server，不经扩展厂商中转。
3. 不得以 side-load 之外的渠道（如伪装成普通工具类扩展）分发；`manifest.json`
   中的 `key` 用于固定扩展 ID，重打包分发前应替换为自己的并同步更新 Native
   Messaging host 的 allowed_origins。

## 关联：HiveBridge 扩展（user-web/bridge）的 `host_permissions` 说明

HiveBridge（私信桥接扩展）的 host_permissions 含 `http(s)://*/*`、`ws(s)://*/*` 通配，
用途是**serverUrl 用户自配**：

- 扩展连接的 user-server 地址（`serverUrl`）由用户在扩展弹窗中自行配置：
  开源/自部署构建默认 `http://localhost:8204`（`DEFAULT_USER_SERVER`），用户可改指
  任意自有部署地址；官方 SaaS 发行包的云端地址在**构建期**经 `BRIDGE_ONLINE_SERVER`
  注入（`bridge/src/core/constants.js`），源码与开源产物中不预填厂商域名。
- 由于 serverUrl 是用户自配的任意 host:port，host_permissions 无法在构建期收窄到
  固定清单——通配是为了让「用户把服务指到哪，扩展就能连哪」成立，并非为访问
  任意网站内容；对网页内容的访问仅限用户已开启桥接的渠道站点 tab（`activeTab`
  + `scripting`，按渠道选择器注入）。
- 上架/分发时同样须在商店说明中明示：「扩展会连接您配置的自建服务器地址，
  并访问您开启桥接的社媒渠道页面以收发私信」。
