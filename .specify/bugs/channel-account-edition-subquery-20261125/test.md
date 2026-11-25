# Bug Test: 渠道账号版本门子查询继承外层条件

- **Slug**: channel-account-edition-subquery-20261125
- **Status**: RED, focused GREEN, and full Go gates passed

## Planned checks

- 新增仓储回归用例：账号主键与渠道主键不同仍可被国外渠道版本门返回。
- 同一用例验证识别码解析与跨租户隔离。
- 修复后运行仓储定向测试和 service 相关定向回归；完成提交前完整 Go 门禁。

## RED evidence

`channel-account-edition-subquery-red.log`：
`TestEditionChannelAccountGateDoesNotInheritAccountPredicates` 在原实现上返回 `got=<nil> err=<nil>`，退出码 1。

## Additional RED and GREEN evidence

- `channel-account-system-scope-red.log`: tenant mode 开启且识别码调用未绑定租户时，新增回归捕获 PostgreSQL 空子查询语法错误，退出码 1。
- `channel-account-edition-subquery-green-final.log`: repository matrix 与新增的独立账号/渠道主键、system-scope identify-code、cross-tenant rejection 回归通过，退出码 0。
- `focused-service-after-subquery-fix.log`: channel dispatch preferred account、CRM entity/account scope、lead-history canonical aliases、SageX cross-tenant bootstrap 均通过，退出码 0。
- 最终复验 `closeout-repo-focused-final.log` 与 `closeout-service-focused-final.log` 均退出码 0。
- 完整 user Go 门禁六项均退出码 0：`user-go-summary.json`；包含全量 `make test-go`，日志为 `user-go-tests.log`。
