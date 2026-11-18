# R3 验证

Result: partial。

[评估](assessment.md) · [修复](fix.md)

三操作各九场景覆盖合法、unknown、过期、缺令牌、缺认领时间、缺上下文、系统豁免、取消及默认租户不能读18。另验证unknown不能被相同旧令牌完成。保留原认领、旧令牌拒绝、改排、租约回收及人工核对测试。

`sh_1e6d488ae001dkDUpl4Hkwqj5b`退出0：repository 2.450秒、service 3.204秒；后续共同回归`sh_1e6f64012001PTC5rIuB9Z80D4`也退出0。只受控测试，非完整新全仓结果或外部exactly-once证明，未发布。
