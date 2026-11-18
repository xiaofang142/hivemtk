# 蜂桥同轮、同尝试的真实链路观测

**日期**：2026-11-16　**状态**：Specified　**授权**：用户采纳全部建议，自主推进；风控与回答正确先于速度。

## 用户故事与验收

### US1：不把缺测和日志落库当实际操作（P1）
- Span 记录操作开始、结束与单调时钟耗时；异步批次不会改变这些时间。
- 未测记录与真实小于1ms的0分开；旧记录保持可读，但不进入新的实测延迟统计。

### US2：同一客户消息的首次/一次重生成和投递尝试可关联（P1）
- 相同租户、渠道、账号、会话和入站身份得到同一 turn；任一范围变化都不同。
- 首次与重生成 answer_attempt 不同；同出站重领 delivery_attempt 不同，不被旧 msg_id 去重隐藏。
- 生成、入队、认领、回执以及 handoff/NO_REPLY/error 分开；入队不是客户送达，unknown 不是成功。

### US3：可靠监控与兼容性（P1）
- 同会话两轮不可交叉配对，跨租户、账号、会话不得配对。
- 仅同 turn 有效时间边界、delivered 回执产生服务端接入到ACK指标；不声称平台已读。
- UI 对未测展示横线，层级按 turn/answer_attempt 归并，不把两次生成的工具混挂。

## 功能要求
- FR-001：新增可空 StartedAt/CompletedAt、DurationMeasured、TimingVersion、TurnID、AnswerAttempt、DeliveryAttempt，旧字段兼容，不猜测历史数据。
- FR-002：context carrier 子副本隔离，修改 span 不改变父上下文；关联身份缺任一权威范围则 turn 为空，不按昵称猜测。
- FR-003：计时只观察，不改变发送锁、人工持有、投诉否决、重试次数、ACK状态转换或渠道节奏。
- FR-004：初次和再生成、真实入队与ACK须在操作前开始计时；零ID重复审计不得记录成功入队。
- FR-005：新聚合使用 repository 读数据，tenant fail-closed；缺测不入均值/p95，返回覆盖数量。
- FR-006：递归去除 token、password、cookie、authorization、api/merchant key 等凭证；载荷有界且保持合法JSON。现有受租户授权的学习正文兼容保留，新计时元数据不存正文。不得隐式访问外部分析服务。
- FR-007：未采集的浏览器打字/点击/锁等待各段保持unknown；本项目不以缩短拟人动作换取指标。

## 边界
缺账号/会话/入站ID、空context、并发子span、0ms真实操作、非法/巨大JSON、重复ACK、unknown/failed回执、旧trace与新turn并存、重领、批次延迟、负时间/不同轮工具均有回归。

## 成功标准
- SC-001：两轮同会话与跨作用域种子只产生各自有效配对；缺测样本不改变实测均值。
- SC-002：异步sink前后的操作时间完全一致，非零耗时来自实际代码边界，真实0仍可辨识。
- SC-003：既有安全/租户/学习回归通过，凭证不进入trace JSON；无生产发送/配置修改作为单测副作用。
- SC-004：只有 tasks 全完成并验证后才 Converged；生产与真实页面未覆盖明确标记partial。
