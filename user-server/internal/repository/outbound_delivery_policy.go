package repository

// outboundDeliveryPredicate 只接收仓储内部固定的表限定符，不接收请求输入。
// extra 是 text：空值兼容旧行；非法 JSON 导致查询失败而非放行审计。
// duplicate_of 是旧审计标识，delivery_kind=audit 是新审计标识，两者都不能投递。
func outboundDeliveryPredicate(table string) string {
	prefix := ""
	if table != "" {
		prefix = table + "."
	}
	extra := "COALESCE(NULLIF(" + prefix + "extra, ''), '{}')::jsonb"
	return "NOT jsonb_exists(" + extra + ", 'duplicate_of') AND " +
		"COALESCE(" + extra + "->>'delivery_kind', '') <> 'audit'"
}

// outboundRecoverableErrorPredicate 不允许将未知结果、人工对账、耗尽预算的来源痕迹抹掉。
func outboundRecoverableErrorPredicate(table string) string {
	prefix := ""
	if table != "" {
		prefix = table + "."
	}
	return prefix + "push_error NOT IN ('ack_unknown', 'outbound_push_exhausted') AND " +
		prefix + "push_error NOT LIKE 'reconciled_unknown:%'"
}
