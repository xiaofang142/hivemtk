# 实施计划

Tech：原MV3 IIFE、jsdom/Vitest。无后端schema、新依赖或权限。constitution v1.0.4；单智能体、整体一次提交、仅自己范围。

## 文件范围

- 新增 channels/douyin-identity.js 与 content/douyin-identity.js：受限MAIN读器及隔离世界证据读取；平台DOM只在channels。
- 修改 channels/douyin.js、channels/xhs.js：稳定会话/精确昵称/明确方向；系统分类与消息ID规则保持。
- 修改 core/channel-adapter.js：历史/实时方向来源透传，不改发送和巡检速率。
- 修改 manifest.json、scripts/build.mjs、background/injector.js：静态/主动注入次序；不改他人在途background/index/http-ingest。
- 新增 test/conversation-evidence.test.js、test/douyin-identity-reader.test.js；更新 test/injector.test.js以验证两阶段注入，保留其他平台单阶段对照。
- 记录本spec任务/研究、总任务发现与进度。旧parser/walker测试保留；发现新根因另assess，不为绿随意删测。

## 执行

1. 先以真实结构的合成正文fixture通过现有getter/parser证明错误：昵称漂移、缺ID仍生成、真实self气泡客户化。
2. 再以MAIN读器真实函数验证回收/冲突与无特权；最小实现并回归。
3. 两阶段注入真实失败测试，然后装配；构建隔离产物，不覆盖安装目录。
4. 只读ego复用Space30验证MAIN/ISOLATED可见证据，不自动枚举点击或真发。
5. 定向/全量扩展测试、lint/编译，记录未测；整体门禁前不得Converged。

## 限制

DOM标记不是跨世界安全授权。刷新事件没有传参能力；页面可伪造信息，但拿不到扩展凭证或执行权。当前只校准抖音/小红书，不盲改其他渠道。旧昵称队列安全失败比自动猜客户更重要。
