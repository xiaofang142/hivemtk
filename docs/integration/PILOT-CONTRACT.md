# EC-00 启动基线与 PilotContract v1

## 规格与裁决

来源为外层任务仓 M00/EC-00。本地只交付安装/租户身份核对、版本化试点配置、
00-A/B/C 契约验证；不连接平台、不执行订单动作、不索取凭证、不支付费用。
部署启动基线独立于 EC-01/02：未配置也是可读取状态，生产许可始终阻塞。

现有落点：`system/install.Load` 是安装身份源；`pkg/tenant` 是请求租户源；
`system_config_kv` 是已有租户结构化配置模型；`capregistry.All` 是消息能力声明源。
复用上述入口，不建店铺主数据、订单执行器或第二份能力注册表。当前没有可权威核对
平台 shop 与 merchant owner 的店铺实体；契约中的 `shop_id`、`owner_ref`、
`authorization_ref` 是必填的审计引用，不与 `bridge_accounts` 或外部平台做真实性绑定。

规则冲突与最小裁决：

- 交互标准要求 `/api/v1`，CLAUDE.md/宪法要求 `/api/{domain}/{resource}`：
  遵守后者，使用 `/api/pilot/contract`。
- 安装身份公开口已脱敏：新接口只挂租户管理员组，普通用户与匿名不可访问。
- 消息能力不等于订单写能力：返回原注册声明供核对，订单写能力标记
  `pending_live_validation`，任何输入不得提升为真实就绪。
- 仓库自动提交发布与本轮禁止冲突：依本轮指示不提交、不发布、不改外层任务文档。
- graphify 已在本仓宪法明确停用；不恢复失效工具，不生成无关图谱。

## 计划

1. DTO 定义 schema、revision、安装/租户/店铺归属、授权引用、读写范围、资金上限、
   授权人、观察窗、回退人、动作/订单/后置条件/负责人及 FA/AR/CH 关闭证据。
2. Repository 复用 KV 模型和读取口；条件写入防止并发修订互相覆盖。
3. Service 核对请求租户/安装身份，纯本地校验，严格接受 fake/simulated。
4. Controller/Router 挂管理员 GET/PUT/POST validate；配置入口展示未配置与模拟边界。
5. fake 用例覆盖 00-A/B/C、身份缺失/错配、错误 schema/provider、隔离与并发冲突。

## 契约

- `GET /api/pilot/contract`：读取本租户配置，未配置返回 `configured=false`；
  数据库/安装读取失败不可伪装为“未配置”。不会创建、修复或重铸安装身份。
  未配置响应只含真实 `install.Load` 基线、当前租户和空草稿字段，不预置 `fake` provider；
  fake/simulated 只有显式录入并保存后才成为已配置契约。
- `POST /api/pilot/contract/validate`：本地校验提交草稿，不保存、不触发平台动作。
- `PUT /api/pilot/contract`：保存草稿，输入 revision 必须与当前修订一致（首版为 0）；
  响应 revision 加一，记录令牌 actor、UTC 时间。无效 schema/身份/provider 拒绝保存；
  不完整草稿可保存，阻塞逐项返回。并发冲突返回 409，客户端须重读。

`version=pilot-contract.v1`，资金为整数最小货币单位；显式 0 合法，省略不合法。
`install_id` 与请求 tenant 做实际一致性检查；店铺/owner/授权引用只检查非空，不证明
相互归属或商户授权真伪；外部审批等待与工程分钟分开记录。
读取已保存契约仍重新核对当前安装与租户，不能靠旧缓存跨部署使用。

00-A：fake 授权店、FA/AR/CH 关闭提交及测试引用完整、单笔订单和后置条件/负责人锁定，
本地可为 `ready_for_simulated_validation`；生产仍为 `blocked`。
00-B：只有文档而无授权店，可验证并保存 fake 草稿，明确店铺/授权阻塞。
00-C：没有行是否存在的自动放行规则，FA 缺少关闭提交或测试引用始终阻塞。

## 风险与不证明事项

契约不含凭证、订单地址/客户隐私；审计引用只存不解析 URL，也不执行指令。
本地 fake 证据不可替代真实商户授权、平台审批、店铺归属验证、真实订单后置条件、
回退演练或浏览器逐元素证据。配置骨架没有开放任何业务执行权限。

## EC-00 交付与验证记录

- 落点：`system/install.Load`（只读、无身份不重铸）、请求租户上下文及现有租户 KV；
  `capregistry.All()` 仅作为现存能力声明参考，未把消息能力解释成订单改地址能力。
- 接口：租户管理员 `GET/PUT /api/pilot/contract`、`POST /api/pilot/contract/validate`；
  安装身份只在管理员受限响应出现，不增加公开端点。
- 验收边界：本机配置及 fake 基线独立于 EC-01/02；没有店铺连接器、订单动作、凭证、
  真商户授权或平台连通性证明。已取得 ego-browser 模拟组件逐元素证据，但没有全站真实 API 页面证据。
- 逐项规格自查：安装 ID/版本/初始化状态从既有 install lock 只读；缺失身份保持
  `baseline_available=false` 并阻塞；租户 ID 与请求上下文严格匹配。shop/owner/授权引用是
  契约字段且非空校验，但没有权威店铺实体/外部查询来证明 shop-owner-tenant 真实绑定；
  read/write scope、资金上限、授权人、观察窗、回退人、单订单/后置条件/负责人均为明确字段；
  00-A 的完整本地契约只得到 simulated-ready，00-B 无店铺仍 blocked，00-C 按依赖 ID 要求
  FA/AR/CH 各且仅一项并同时有关闭提交及测试证据；工程分钟与审批等待分别保存；生产始终 blocked。
- 命令与结果（本轮实跑）：
  - `go build ./...`、`go vet ./...`：通过。
  - `go test ./internal/service ./internal/controller ./internal/router ./internal/repository -run '^TestPilotContract' -count=1`：使用专用本地测试库新鲜复跑通过，包含 `TenantCAS`；该测试库只验证租户边界与并发冲突，不连接平台。
  - `TestPilotContract_FundsLimitAmountPresenceFromJSON`：回归测试先观察到 omitted/null 错误地 ready，再以 presence-aware `*int64` 修正后通过；覆盖省略、null、显式零与负值，校验不完整草稿可保存但仍阻塞。
  - Service 采用导出 `PilotContractService` 接口及私有 `pilotContractService` 实现；Controller 构造注入接口。
  - `python3 scripts/check-layer-boundary.py`、`bash scripts/check-architecture.sh`、`bash user-server/scripts/check-gorm-only.sh`：通过；层边界扫描 1608 个非测试 Go 文件，跨层违规 0。架构脚本有一条既有 webhook Service interface warning，不属于本任务。
  - `cd user-web && npm run test -- --reporter=dot`：111 个文件、1067/1067 测试通过；`npm run lint:check` 与 `npm run build` 同一轮均退出 0。
  - 全仓 `go test ./internal/...` 未由主智能体独立复跑；实现者此前报告全仓测试受既有数据库配置/架构棘轮差异阻塞，不将其描述为通过。
  - 本轮 `make lint` 被既有 `user-server/internal/model/order_change_request.go:46` gofmt 问题拦截；该文件不属于本任务且未修改。`make vet` 退出码 0。
  - `make audit` 在临时索引包含本新文档后通过文档链接门，最终被既有孤儿 i18n 键拦截（1 条，基线 0）；本任务新增词条均有组件/导览调用，不刷新基线掩盖差异。跨仓缺 checkout 等提示为跳过性警告。Graphify 刷新因当前环境缺 `graphify` Python 模块失败，仓库宪法注明该图谱工具已停用。
  - `git diff --check`：通过。常规 `git diff --stat` 仅计已跟踪改动；最终含 13 个已跟踪修改和 13 个新文件，勿把未跟踪文件误读为零实现。
- ego-browser 本地组件证据（主智能体复验；临时 Vite `127.0.0.1:8218`，读取真实 `PilotContractPanel.vue`，API 全部为显式 demo fake；不连本任务服务/数据库/线上）：初始快照为 `demo-install-readonly`、`未配置`、`pending_live_validation / blocked`；用唯一 `textarea[aria-label="PilotContract JSON 草稿"]` 填入 fake-only JSON，并逐个点击唯一“本地校验”与“保存草稿”按钮。校验后显示“仅完成本地契约校验，未保存；生产仍阻塞。”；保存后快照为“已保存草稿”、`revision: 1`、`pending_live_validation / blocked`，状态“草稿已保存；生产仍阻塞。”。证据文件为外层计划目录 `ec00-ui-final-snapshot.txt` 与 `ec00-ui-before-click.png`。此证据只证明实际组件在 fake API 下的状态与操作，不证明整页生产 API、真实 install lock/数据库或真实商户验收。
- 工程工作量仅作为启动期实现估算入契约；平台审批等待另列，当前为 `pending_external_approval`，均不是实际工时/真实审批记录。
- 不提交、不推送、不部署；外层任务清单保持未修改。
