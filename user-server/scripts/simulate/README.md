# 模拟真实用户提问压测器

针对 `hivemtk user-server` 已集成渠道，模拟真实访客提问，验证 AI 链路、限速与稳定性。

## 已集成渠道与本次覆盖

生产环境 user-server 集成了两类「用户侧」入口：

| 通道 | 端点 | 本次是否模拟 | 说明 |
|------|------|------------|------|
| **网页客服 web_embed** | `POST /api/chat/public/sessions` + `/:sid/messages` | ✅ 主入口 | 走完整 RAG+AI 链路，最贴近真实访客，无需桥接前端 |
| 桥接三通道（xiaohongshu/douyin…） | `POST /api/bridge/ingest` | ⏸ 预留 | 需有效 bridge_account（当前库无 active 账户），且走回环去重，模拟成本高 |
| Telegram / 企微 / WS | 各渠道 webhook | ⏸ 未覆盖 | 需对应账户与回调穿透 |

> 「从不同端口测试」= 覆盖**不同入口端点/渠道方案**。本工具以 web_embed 为主，
> 支持 `--app-key` 多次指定以轮询**多个渠道入口**，`--base-url` 指向你要打的
> user-server（默认且推荐 `http://localhost:8204`，即 `make dev` 起的本地实例）。

## 快速开始

```bash
cd hivemtk/user-server/scripts/simulate

# 最小验证（5 条，单并发，最稳妥）
python3 simulate.py --app-key ak_xxxx --count 5

# 标准 200 问（默认已限速：并发1、间隔15~25s、超时120s）
python3 simulate.py --app-key ak_xxxx --count 200

# 多渠道轮询（不同入口方案）
python3 simulate.py --app-key ak_a --app-key ak_b --count 200

# 守护进程模式（无限循环，每轮 20 题，轮间休息 90s，日志追加 simulate.log）
python3 simulate.py --app-key ak_xxxx --daemon --round-size 20 --round-gap 90

# 单进程守护+监控（内置跑轮次 + 周期评估，写 monitor_report.log）
python3 monitor.py --supervisor --app-key ak_xxxx --round-size 12 --round-gap 60

# 从环境变量读 app_key
export SIM_APP_KEY=ak_xxxx
python3 simulate.py --count 100
```

可用 active 渠道 app_key 查询：

```bash
PW=$(grep '^POSTGRES_PASSWORD=' ../../../.env | cut -d= -f2-)
PGPASSWORD=$PW psql -h localhost -p 8232 -U admin -d user_db -tAc \
  "SELECT app_key FROM chat_channels WHERE status='active';"
```

> 注意：用 `X-Chat-App-Key` header 传 app_key 才能正确解析渠道；
> 仅 body 传 `channel_id` 会触发 `AppKeyResolve` 解析偏差导致 403。

## 参数

| 参数 | 默认 | 说明 |
|------|------|------|
| `--base-url` | `http://localhost:8204` | 服务基址 |
| `--app-key` | 无（必填，可多次） | 渠道 AppKey，轮询模拟多渠道 |
| `--count` | 200 | 提问总条数 |
| `--concurrency` | 1 | 最大并发（栈脆弱，建议 1~2） |
| `--min-gap` / `--max-gap` | 15 / 25 | 每条随机间隔秒，**合理控制访问速度** |
| `--timeout` | 120 | 单条 HTTP 超时（AI 真实回复常 40~60s） |
| `--daemon` | 关 | 守护进程：无限循环，每轮 `--round-size` 条，轮间 `--round-gap` 秒 |
| `--round-size` / `--round-gap` | 20 / 90 | 守护模式每轮题数 / 轮间休息秒 |
| `--max-rounds` | 0 | 守护最大轮数，0=无限 |
| `--seed` | 无 | 随机种子，可复现 |
| `--shuffle` | 关 | 打乱题库顺序 |

## 真实感设计

- **真实昵称**：从 `names.json` 的「姓+名+后缀」随机组合（如 `于秀英宝宝`、`蒋敏老师`），
  写入 `visitor_name`，落库为 session `user_name`。
- **合理访问速度**：默认单并发 + 15~25s 随机间隔。实测 AI 真实回复 40~60s，
  且 RAG/LLM 栈易过载——盲目并发会触发「AI 服务暂时不可用」兜底甚至
  `RemoteDisconnected` 断连，故默认偏保守。
- **提问库可随时扩充**：直接编辑 `questions.json` 往 `questions` 数组追加
  `{"q":"...","cat":"分类"}`，无需改代码。不足目标条数时自动循环取样。

## 题库与昵称库

- `questions.json`：**536 条**真实业务提问，覆盖 product/price/buy/logistics/
  aftersale/return/feature/cooperation/complaint/account/chat 共 11 类。
  追加示例：
  ```json
  {"q":"你们支持7天无理由吗？","cat":"return"}
  ```
- `names.json`：50 姓 + 50 名 + 后缀池，可随时扩充。

## 结果解读

- **成功率**：HTTP 链路成功比例。
- **其中降级**：AI 栈过载返回「AI 服务暂时不可用」兜底——链路通但非真实应答，
  属合法运行时失败（RAG 栈容量/超时），非代码缺陷。遇大量降级请降速或等栈恢复。
- **平均耗时 / AI均长**：仅统计成功且非降级的真实应答。

## 扩展：桥接通道（不同端口方案）

当库中存在 active `bridge_accounts` 时，可在 `simulate.py` 中参照
`internal/bridge/handler_http.go` 的 `POST /api/bridge/ingest` 协议
（参数 `channel/account_id/conversation_id`，白名单 `xiaohongshu`/`douyin`，
body 含 `sender{id,name,type}` + `message{content}`）追加 bridge 入口实现，
实现「从桥接端口模拟真实用户上报」的多方案覆盖。当前因无 active 账户，默认走 web_embed。

## 守护进程 + 监控评估

### 守护执行（合理频率持续测试）
- `simulate.py --daemon`：无限循环跑，每轮 `--round-size` 题（默认 20），
  轮间休息 `--round-gap` 秒（默认 90），日志追加 `simulate.log`。
- `monitor.py --supervisor`：单进程**内置守护+监控**——跑轮次→评估→休息→继续，
  自带心跳，每 `--eval-every` 轮输出评估报告到 `monitor_report.log`。
  （推荐：本机交互环境无法常驻后台进程时，用 supervisor 单进程前台跑最稳。）

### 间隔监控与评估
- `monitor.py --once`：单次采样，输出评估报告：
  - 服务探活（`/api/health` HTTP 码 + 延迟）
  - 日志累计（累计题数 / 成功率 / AI 降级率 / 近 3 轮趋势）
  - **DB 真实落库**（查 `customer_sessions` + `session_messages` 计数、近 10 分钟增量）
  - 评估结论（服务可达、AI 降级率、链路/写库异常告警）
- `monitor.py --watchdog` / `--supervisor`：周期采样，自动保活 daemon。
- 已配置 **automation 定时任务**（每小时）：自动执行"跑一批保守测试 + monitor 评估 + 汇报"，
  实现无人值守的守护式周期监控。

### AI 回答内容质量监控（内容级，推荐配合守护进程）
`simulate.py` 每发一条请求都会把**真实 AI 回答内容**落库到 `interactions.jsonl`
（含问题、分类、访客、耗时、回答全文、降级/空标记）。`ai_quality.py` 读取该文件，
对回答做内容级质量评估并持续监控：

- `python3 ai_quality.py --once --last 50`：评估最近 50 条，输出质量报告（含真实回答样例）
- `python3 ai_quality.py --once --since 60`：评估最近 60 分钟内的回答
- `python3 ai_quality.py --daemon --interval 300 --last 30`：每 300s 评估最近 30 条，写 `ai_quality.log`

质量评估维度（纯标准库启发式，无需外部依赖）：
- **空回答 / 降级兜底**（"AI 服务暂时不可用"等 27 字兜底）
- **截断**（长回答未以句末标点结束，疑似被 max_tokens 截断）
- **重复/啰嗦**（连续相同字符 >8，或长且占比高的重复子串——模型卡壳循环复述；
  普通要点列表/模板 token 不误判）
- **切题度**（按问题分类匹配关键词，识别答非所问）
- **综合质量分 0~100** + 优/中/差分档；报告采样展示真实「好回答」与「最差回答」供人工复核

> 与守护进程组合使用示例（两个终端或 supervisor 单进程内已内置质量评估）：
> ```bash
> # 终端 A：合理频率持续发请求、落库真实内容
> python3 simulate.py --app-key ak_xxxx --daemon --round-size 12 --round-gap 60
> # 终端 B：周期读取内容、监控回答质量
> python3 ai_quality.py --daemon --interval 300 --last 30
> ```
> `monitor.py --supervisor` 已在每轮评估后自动调用 `ai_quality.evaluate_recent`，
> 单进程即可同时完成「持续测试 + 内容质量监控」。

### 评估结论判据
| 现象 | 结论 |
|------|------|
| 服务 200 + 成功率高 + 降级率低 + DB 增长 | 运行健康 |
| 降级率 100%（全 27 字兜底） | RAG/LLM 栈未真实应答，需排查 8207/8208/8209 |
| 出现 `RemoteDisconnected` 断连 | 连续压力触发服务端断连，需拉大间隔或降并发 |
| 日志成功但 DB 无增量 | 链路/写库异常 |
