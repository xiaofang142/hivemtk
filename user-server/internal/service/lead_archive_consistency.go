package service

// leadArchiveAllowed 归档准入：候选与降级结论不得凭手工归档越过有效性闸。
// 资格确认须走 LM-03 的显式事件，不能由归档动作本身转正。
func leadArchiveAllowed(candidate, degraded bool) bool { //nolint:unused // LM-03 归档准入测试原型，尚未装配至生产归档入口。
	return !candidate && !degraded
}

// leadArchiveTimestamp 只有归档真正成功才返回 true（调用方据此写 ArchivedAt）；
// 失败、未完成、未准入一律不伪填归档时间。
func leadArchiveTimestamp(archivedSuccessfully bool) bool { //nolint:unused // LM-03 归档时间测试原型，尚未装配至生产归档入口。
	return archivedSuccessfully
}
