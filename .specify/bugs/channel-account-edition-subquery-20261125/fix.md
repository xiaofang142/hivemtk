# Bug Fix: 渠道账号版本门子查询继承外层条件

- **Slug**: channel-account-edition-subquery-20261125
- **Assessment**: [assessment.md](assessment.md)
- **Status**: applied and focused checks green

## Planned change

构造 GORM 版本门子查询时，仅从外层查询创建 `Session(NewDB: true)` 并立即建立 `Channel` model；不再用原始 context 开启第二个普通 Session。这样保留外层查询 context（包括系统作用域），同时避免复制账号查询 clauses。edition gate、租户 callback 与跨租户识别码解析行为保持不变。

## Regression

新增账号主键与渠道主键不同的国外渠道夹具，覆盖 `GetByID`、`GetByIdentifyCode` 及跨租户拒绝。

## Local verification

- RED：`channel-account-system-scope-red.log`：开启 tenant mode 后，以无租户 context 执行识别码解析，原实现返回 PostgreSQL `syntax error at or near ")"`；退出码 1。
- GREEN：`channel-account-edition-subquery-green-final.log`：`TestEditionChannelAccountRepositoryMatrix` 和新增回归通过；退出码 0。
- GREEN：`focused-service-after-subquery-fix.log`：首选账号分发、CRM账号/行业/owner/admin scope、canonical lead-history aliases、SageX跨租户 bootstrap 五项回归全部通过；退出码 0。
- 最终收敛复验：工作区 `closeout-repo-focused-final.log`、`closeout-service-focused-final.log` 均退出码 0；完整 user Go 构建、vet、测试、audit、lint 与 CRM Python unittest 均 exit 0，汇总见 `user-go-summary.json`。
