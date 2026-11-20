# Bug Assessment: 发布前质量门阻塞

- **Slug**: release-quality-gate-regressions
- **Created**: 2026-11-20
- **Source**: FA-15 交付前全仓门禁实跑
- **Verdict**: valid
- **Severity**: medium

## Report and Reproduction

在 master 的已提交工作区运行 `make lint`、`make audit` 与 `bash scripts/check-architecture.sh` 分别退出 2、2、1。FA-15 的同意回归和 race 检查通过，但不能把局部通过算作交付门通过。

## Root Cause

- 配置已有 `PLATFORM_HEARTBEAT_ENABLED` 读取点，示例遗漏；audit 在覆盖检查处停止。
- lint 实报八个 gofmt 文件、一处 HTTP body Close 未处理、三个首字母大写错误字符串、两个未使用声明。
- `channel_route_rule_service.go` 文件名有禁止的冗余后缀。
- `ReplayRepositories`、`DelayedRepository` 实际仅装配同句柄仓储，不执行查询；作为 receiver 方法触发所有仓储方法必须首参 context 的约束。应改为包级构造函数，查询仍由已有带 context 的方法执行，不能新增 Background 或放宽门禁。
- 企业微信客服 `defaultAPIBase` 常量未使用：需进一步以构造器回归确认默认 URL 是否已由共享 core 提供，避免为消除 unused 更改实际默认值。

## Remediation Contract

只修已提交的门禁阻塞，不放宽规则、不覆盖其他会话未提交文件、不改 FA-15 授权策略。移除无调用的遗留解析包装前核对实际在用处理链。Close 按同文件已有模式显式处理。八处 gofmt 只做格式修正。纯装配改包级构造，保留 nil 依赖与请求上下文语义。配置示例补已有开关及默认值，不新增生产配置。

文件范围：`user-server/.env.example`、`user-server/docs/ENV_REFERENCE.md`（如生成后有漂移）、`user-server/internal/model/{ai_agent,ai_sales_champion,im_platforms}.go`、`internal/repository/{channel_interaction,delayed_outbound_repo,bridge_offline_replay_repo}.go`、`internal/service/{client_device,douyin_lead_miner,outbound_impl_bridge,outbound_impl_im_platforms,workflow_http_action,webhook,webhook_channel_im_platforms,delayed_replay_permission,bridge_offline_replay,channel_route_rule_service,channel_route_rule}.go`、`internal/channelbot/wecomcs/wecomcs.go`；新增测试限装配 nil 行为与客户端默认 URL 回归。

## Verification

先核对默认 URL 回归，再执行原三个失败命令；运行受影响包、build/vet 和完整 Go 测试。若 audit 揭示其他阻塞，记录新根因和范围后才能修复。最终结果如实记录 fix.md/test.md，门未通过不销 FA-15。

## Evidence Supplement Before Configuration Fix

`check-env-example-coverage.py:20` 实际扫描根目录 `.env-example`，其中该键已有完整说明但定义被注释；先前仅补 `user-server/.env.example` 无法覆盖此门。范围补入根 `.env-example`，只启用原有默认 true 示例行。`TestNewClient_DefaultAndOverrideURL` 实跑退出 1：微信客服默认 BaseURL 为空；共享 core 未提供渠道默认值，构造器须先装入渠道默认选项，再允许调用方覆盖。

本轮首个多文件 patch 因删除遗留解析函数的匹配上下文有误被整体拒绝；核对 git diff 确认零源码部分落盘后，分两份精确 patch 应用。第二轮 lint 工具自身在 approval 包分析阶段 panic（非 lint 业务告警），需用独立缓存复测，不屏蔽分析器。

## Evidence Supplement Before Documentation Fix

第三次 `make audit` 已越过配置覆盖检查，停在 10 处离线链接：历史 1.3.7 两份制品附件当前未保留、唯一清单位于外层独立仓、渠道矩阵实际生成到 `user-server/docs/channel-capabilities.md`、六份 converge 引用了不存在的 `acceptance.md`。后六项可链接现存 `final-validation-20261117.md`，但须保留其中 partial 结论；历史附件和跨仓路径改为明确路径说明，不生成证据或另造任务清单。

文档范围补入 `.specify/bugs/bridge-safe-quality-20261116-sol/final-validation-20261117.md`、`docs/INDEX.md`、`docs/channel-onboarding.md`、`specs/017-bridge-turn-observability/converge.md`、`specs/018-bridge-lead-deferred/converge.md`、`specs/019-bridge-conversation-evidence/converge.md`、`specs/020-retrieval-observation/converge.md`、`specs/021-answer-cache-scope/converge.md`、`specs/022-bridge-outbox-wake/converge.md`。不改历史测试数字、发布或送达结论。

## Evidence Supplement Before Controller Fixture Fix

完整 Go 测试在工作流控制器有五项真实失败：成功路径仍使用缺少 `config.action_type` 的 action、空节点或字符串节点、旧 `from/to` 边。FA-02 新服务端校验按约拒绝，`ExecuteFlow` 未先检查创建结果又把空 data 断言为 map，导致 panic。范围仅补入 `user-server/internal/controller/workflow_orchestrator_test.go`：成功路径改用合法 trigger/log 图及 `source/target` 边，解析前断言 HTTP 状态，并保留或补充非法图拒绝用例；不得为旧夹具放宽生产校验。

同一全量运行后半段还出现共享 Go 缓存文件缺失的构建错误；与上述业务失败分别记录，后续以独立 `GOCACHE` 重验，不能用环境问题抹去控制器红灯。仓储装配回归新增 `user-server/internal/repository/replay_constructors_test.go`，只验证 nil 与同句柄传递，不引入数据库连接或后台 context。

独立缓存重现工作流控制器相同五项失败及解析 panic，排除缓存因素。第四次 audit 已验证 10 处断链清零，继续揭示已有 `CLIENT_DEVICE_TENANT_CODES` 仅在子目录配置样例/生成参考出现，而可发现性门只读根 `.env-example` 等运维面；范围仍为根配置样例，补充空白名单拒收语义，不改门或生产配置。

## Evidence Supplement Before Signature Boundary Registration

第五次 audit 在 `SignatureWindow` 停止。该值固定 Slack 入站签名的时间容差，影响防重放的信任边界，不是应允许运维热调的业务旋钮；调大将扩大可接受重放范围。按现有协议/安全护栏的登记方式，在 `scripts/hardcode-tunable.baseline` 添加逐项理由和该常量名，保留验签实现及固定五分钟窗口，并运行现有 `TestVerifySignatureWindow` 回归。范围仅补入该基线文件，不放宽扫描规则或增加通配豁免。

## Evidence Supplement Before Unused Translation Cleanup

第六次 audit 已通过配置与常量门，i18n 棘轮报告一个孤儿词条。用该门相同的五条消费路径计算，唯一键为 `长度不能超过 32 字符`；在前端源码、测试与后端查询该完整字面量仅命中九份语言包，无调用点。范围补入 `user-web/src/i18n/locales/{ar,de,en,es,fr,ja,pt,ru,zh}.json`，各删除这一个死词条；不新增虚假引用，不改棘轮基线，保留正在使用的其他长度校验文案。已读前端标准，补跑管理端 lint、测试与构建。

## Evidence Supplement Before Hidden Checkout Scan Fix

同一代码在原检出 audit 通过，而原生隔离检出在定时任务门报告所有登记项 STALE。`scripts/check-tenant-ctx-writes.py` 的 `iter_go` 对绝对路径 `p.parts` 过滤隐藏目录，检出上层的 `.local` 使所有 Go 文件被忽略；既导致真登记误报，也可能漏掉无登记任务或不带 ctx 的 seam。范围补入该脚本及既有 `scripts/check-tenant-ctx-writes.test.sh`：仅按仓库内相对路径过滤，增加隐藏上层检出仍扫描违规、仓库内隐藏/vendor/node_modules 仍忽略的回归，不删登记、不放宽归属判据。此变更不触及正在独立运行全量测试的 Go 源码。
