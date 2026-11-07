// rag_config_global.go 知识库域 RagConfig 仓储的全局装配桥（AV-04c 批次6）。
//
// RagConfigRepository 定义在 aiagent/knowledge/repository（知识库子域），根域
// service/rag_product.go 装配时原先自己取 repository.GetDB() 再传给
// kbrepo.NewRagConfigRepository —— 那是 service 直取 DB 句柄的宽口径。本桥把
// 「取全局句柄 + 构造」收回仓储层（与 NewScriptLibraryRepositoryFromGlobal、
// NewStarterKitRepositoryDefault 同族）：句柄在仓储包内解析，连接、解析时刻与
// 原写法完全一致；RagConfigRepository 各方法自带 nil 句柄守卫，无库语义不变。
//
// 方向说明：root repository → knowledge/repository 是 L5 数据层内部的跨子域
// 构造转发（既有 csat/i18n_stats 引 internal/reporting 的先例），不构成分层
// 反向依赖；kbrepo 不回引本包，无 import 环。
package repository

import (
	kbrepo "hivemtk-user/internal/aiagent/knowledge/repository"
)

// NewRagConfigRepositoryFromGlobal 构造绑定全局句柄的知识库 RagConfig 仓储
// （全局无句柄时得到 db=nil 的仓储，由其方法级守卫兜底）。
func NewRagConfigRepositoryFromGlobal() *kbrepo.RagConfigRepository {
	return kbrepo.NewRagConfigRepository(GetDB())
}
