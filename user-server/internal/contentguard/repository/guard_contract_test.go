package repository_test

import (
	"hivemtk-user/internal/contentguard/repository"
	"hivemtk-user/internal/contentguard/service"
)

// 在外部测试包保留接口适配断言，生产仓储无需逆向引用服务层。
var (
	_ service.WordStore     = (*repository.GuardWordRepository)(nil)
	_ service.PolicyStore   = (*repository.GuardPolicyRepository)(nil)
	_ service.TaskStore     = (*repository.GuardTaskRepository)(nil)
	_ service.RecordStore   = (*repository.GuardRecordRepository)(nil)
	_ service.SanctionStore = (*repository.GuardSanctionRepository)(nil)
)
