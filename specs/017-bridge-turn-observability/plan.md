# 蜂桥同轮观测 Implementation Plan

> 实施方式：主智能体使用 executing-plans 串行，tasks.md为唯一执行清单。整体完成一次提交，不中途提交。

**Goal**：真实时间、同轮同尝试及缺测覆盖率取代错误的日志拼接指标。
**Architecture**：Model可空计时字段→tracing纯关联/序列化与sink→业务操作边界→Repository实测聚合→monitor兼容API→Vue纯格式/归并。保持既有学习JSON契约及所有投递安全闸。
**Tech Stack**：Go/GORM/PostgreSQL，Vue3/ElementPlus/Vitest。专用测试PG127.0.0.1:8217；不安装依赖。

## Constitution check
v1.0.4：单智能体串行、五层数据访问、租户fail-closed、证据先于完成、仅自身在途文件、整体一次双推。无新增裸go、外部服务、风控旁路或独立浏览器profile。

## 文件与顺序
1. model/message_trace.go 与 pkg/tracing/{tracing.go,observation.go,observation_test.go}：字段/时间/身份/凭证过滤。
2. internal/service/{inbox_ingress_persist.go,webhook_ai.go,webhook_ai_result.go,webhook_outbound.go,inbox_ingress_outbound.go,bridge_observation_test.go} 与 internal/bridge/handler_http.go：真实操作边界，turn持久到hub.Extra，不改业务终态。
3. internal/repository/trace_observation.go/test 与 internal/monitor/{monitor.go,monitor_test.go,observation_test.go}：作用域聚合和coverage，旧数据无端到端猜测。
4. user-web/src/utils/traceTiming.js、views/system/TraceMonitor.vue、tests/unit/traceTiming.test.js：未测与尝试归并；需要文案时使用既有i18n键。
5. 二次复核补充：internal/aiagent/agent/tooluse/{executor.go,executor_observation_test.go}补实际工具边界；internal/monitor/handler.go及observation_test.go传递可选turn/channel/account查询范围，Trace总览也按完整范围分轮。均属于US2/US3，不改变业务执行/鉴权。
6. 渲染回归补充：user-web/tests/unit/traceMonitor.test.js 用真实Vue/ElementPlus组件验证未测、实测零、跳过状态和一次明确查询，接口仅mock读取边界。结果出口观测与跨投递尝试间隙在既有service/monitor观测文件中验证，不能改变安全决策。
7. 新增结果出口与跳过文案在 user-web/src/i18n/locales/{ar,de,en,es,fr,ja,pt,ru,zh}.json 同步；渲染测试使用真实语言文件，不用测试专有词条掩盖缺键。

## 验证
每任务先RED再最小GREEN。Go专用PG，相关tracing/service/bridge/monitor/trace_learning回归；Vue单元测试；边界检查与build/vet。最后全量门禁和ego真实页面验证。未覆盖client阶段，不声称完整实测p95。
