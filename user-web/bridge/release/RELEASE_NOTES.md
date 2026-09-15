# HiveBridge 蜂桥 v1.0.0 发布包

- 产物：`release/hivebridge-1.0.0.zip`（解压后含 manifest.json，可直接「加载已解压的扩展程序」）

## 包含能力
- 三平台私信桥接：抖音 / 小红书 / TikTok（网页版）
- 实时上行：客户私信 -> user-server（触发 AI）
- 历史回填：页面加载/会话切换时存量消息仅落库，不触发 AI（防回环）
- 下行回写：user-server AI 回复 -> 回写网页私信输入框并发送
- 拟人限速风控：最小间隔 + 令牌桶 + 会话冷却 + 相同文案去重
- 多用户/多会话：按 (channel, account, conversation) 隔离路由与历史

## 安装（本机 Chrome）
1. 打开 chrome://extensions
2. 开启「开发者模式」
3. 「加载已解压的扩展程序」-> 选择解压后的 dist 目录
4. 点击工具栏图标 -> 填写 user-server 地址（如 `http://localhost:8204`）-> 保存
5. 打开任一平台私信页 -> 点「自检当前私信页」按 bridge.md §10 真机校准清单核对

## 验证清单
- [ ] 客户发消息后 user-server 收到 inbound_message（AI 触发）
- [ ] AI 回复回写到网页并出现在对话框（outbound_reply）
- [ ] 刷新/切换会话后历史进入 user-server（history 帧，direction=inbound/outbound）
- [ ] 连续高频消息被限速拦截（popup / 后台日志可见 reason）
- [ ] 三平台（抖音 / 小红书 / TikTok / 闲鱼 / 快手）选择器真机校准通过
