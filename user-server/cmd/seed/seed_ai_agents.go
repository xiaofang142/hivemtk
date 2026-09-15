// seed_ai_agents.go 模块 D：AI 智能体种子数据
//
// 覆盖表：
// ai_agents (11) 销售/客服/混合 × 被动/主动 + hivemtk 产品服务
// channel_agent_bindings (10) 多渠道账号绑定（web 客服 + TG 默认绑 hivemtk 产品服务）
// customer_service_agents (3) 坐席挂载
// script_library (20) 销冠话术库
// objection_templates (15) 异议处理模板
// sop_agents (5) SOP 智能体
// sop_executions (15) SOP 执行记录
// dialogue_memories (8) 高价值客户对话记忆
// intent_records (20) 意图识别记录
// sales_intent_scores (12) 客户意向打分
// feedback_events (30) 反馈事件
// feedback_signals (15) 反馈信号聚合
// prompt_candidates (6) Prompt 候选
// champion_dialogues (10) 销冠对话
// ai_sales_logs (30) AI 谈单日志
package main

import (
	"fmt"
	"log"
	"time"

	"hivemtk-user/internal/model"

	"github.com/lib/pq"
	"gorm.io/gorm"
)

type aiAgentsSeeder struct{}

func (s *aiAgentsSeeder) Name() string { return "ai_agents" }
func (s *aiAgentsSeeder) Description() string {
	return "智能体(11)+绑定(10)+SOP(5)+话术(20)+异议(15)+记忆(8)+意图(20)+反馈(30)+销冠(10)+日志(30)"
}

func (s *aiAgentsSeeder) Clean(database *gorm.DB) error {
	if _, err := cleanByCondition(database, &model.AISalesLog{}, "extra::text LIKE ?", "%"+seedTag+"%"); err != nil {
		return fmt.Errorf("清空 ai_sales_logs 失败: %w", err)
	}
	if _, err := cleanByCondition(database, &model.BanditArm{}, "experiment_id LIKE ?", "seed-%"); err != nil {
		return fmt.Errorf("清空 bandit_arms 失败: %w", err)
	}
	if _, err := cleanByCondition(database, &model.PromptCandidate{}, "title LIKE ?", "%"+seedTag+"%"); err != nil {
		return fmt.Errorf("清空 prompt_candidates 失败: %w", err)
	}
	if _, err := cleanByCondition(database, &model.ChampionDialogue{}, "staff_name LIKE ?", "%"+seedTag+"%"); err != nil {
		return fmt.Errorf("清空 champion_dialogues 失败: %w", err)
	}
	if _, err := cleanByCondition(database, &model.FeedbackSignal{}, "outcome LIKE ?", "seed-%"); err != nil {
		return fmt.Errorf("清空 feedback_signals 失败: %w", err)
	}
	if _, err := cleanByCondition(database, &model.FeedbackEvent{}, "metadata::text LIKE ?", "%"+seedTag+"%"); err != nil {
		return fmt.Errorf("清空 feedback_events 失败: %w", err)
	}
	if _, err := cleanByCondition(database, &model.SalesIntentScore{}, "recommended_action LIKE ?", "%"+seedTag+"%"); err != nil {
		return fmt.Errorf("清空 sales_intent_scores 失败: %w", err)
	}
	if _, err := cleanByCondition(database, &model.IntentRecord{}, "raw_text LIKE ?", "%"+seedTag+"%"); err != nil {
		return fmt.Errorf("清空 intent_records 失败: %w", err)
	}
	if _, err := cleanByCondition(database, &model.DialogueMemory{}, "summary LIKE ?", "%"+seedTag+"%"); err != nil {
		return fmt.Errorf("清空 dialogue_memories 失败: %w", err)
	}
	if _, err := cleanByCondition(database, &model.SOPExecution{}, "execution_data::text LIKE ?", "%"+seedTag+"%"); err != nil {
		return fmt.Errorf("清空 sop_executions 失败: %w", err)
	}
	if _, err := cleanByCondition(database, &model.SOPAgent{}, "name LIKE ? OR description LIKE ?", "%"+seedTag+"%", "%"+seedTag+"%"); err != nil {
		return fmt.Errorf("清空 sop_agents 失败: %w", err)
	}
	if _, err := cleanByCondition(database, &model.ObjectionTemplate{}, "example_reply LIKE ?", "%"+seedTag+"%"); err != nil {
		return fmt.Errorf("清空 objection_templates 失败: %w", err)
	}
	if _, err := cleanByCondition(database, &model.ScriptLibrary{}, "content LIKE ?", "%"+seedTag+"%"); err != nil {
		return fmt.Errorf("清空 script_library 失败: %w", err)
	}
	if _, err := cleanByCondition(database, &model.CustomerServiceAgent{}, "1=1"); err != nil {
		database.Unscoped().Where("ai_agent_id IN (SELECT id FROM ai_agents WHERE agent_code LIKE 'seed-%')").Delete(&model.CustomerServiceAgent{})
	}
	if _, err := cleanByCondition(database, &model.ChannelAgentBinding{}, "account_id LIKE ?", "seed-%"); err != nil {
		return fmt.Errorf("清空 channel_agent_bindings 失败: %w", err)
	}
	if _, err := cleanByCondition(database, &model.AIAgent{}, "agent_code LIKE ?", "seed-%"); err != nil {
		return fmt.Errorf("清空 ai_agents 失败: %w", err)
	}
	return nil
}

func (s *aiAgentsSeeder) Seed(database *gorm.DB, ctx *SeedContext) error {
	agents := s.buildAIAgents()
	if err := batchInsert(database, agents, 50); err != nil {
		return fmt.Errorf("写入 ai_agents 失败: %w", err)
	}
	for _, a := range agents {
		ctx.AIAgentIDs = append(ctx.AIAgentIDs, a.ID)
	}

	bindings := s.buildBindings(agents)
	if err := batchInsert(database, bindings, 50); err != nil {
		return fmt.Errorf("写入 channel_agent_bindings 失败: %w", err)
	}

	csAgents := s.buildCustomerServiceAgents(agents, ctx)
	if err := batchInsert(database, csAgents, 50); err != nil {
		return fmt.Errorf("写入 customer_service_agents 失败: %w", err)
	}

	scripts := s.buildScripts()
	if err := batchInsert(database, scripts, 50); err != nil {
		return fmt.Errorf("写入 script_library 失败: %w", err)
	}
	for _, sc := range scripts {
		ctx.ScriptLibraryIDs = append(ctx.ScriptLibraryIDs, sc.ID)
	}

	objections := s.buildObjections()
	if err := batchInsert(database, objections, 50); err != nil {
		return fmt.Errorf("写入 objection_templates 失败: %w", err)
	}

	sopAgents := s.buildSOPAgents()
	if err := batchInsert(database, sopAgents, 50); err != nil {
		return fmt.Errorf("写入 sop_agents 失败: %w", err)
	}
	for _, sa := range sopAgents {
		ctx.SOPAgentIDs = append(ctx.SOPAgentIDs, sa.ID)
	}

	sopExecs := s.buildSOPExecutions(sopAgents, ctx)
	if err := batchInsert(database, sopExecs, 50); err != nil {
		return fmt.Errorf("写入 sop_executions 失败: %w", err)
	}

	memories := s.buildMemories(ctx)
	if err := batchInsert(database, memories, 50); err != nil {
		return fmt.Errorf("写入 dialogue_memories 失败: %w", err)
	}

	intents := s.buildIntentRecords(ctx)
	if err := batchInsert(database, intents, 50); err != nil {
		return fmt.Errorf("写入 intent_records 失败: %w", err)
	}

	intentScores := s.buildIntentScores(ctx)
	if err := batchInsert(database, intentScores, 50); err != nil {
		return fmt.Errorf("写入 sales_intent_scores 失败: %w", err)
	}

	fbEvents := s.buildFeedbackEvents(ctx)
	if err := batchInsert(database, fbEvents, 50); err != nil {
		return fmt.Errorf("写入 feedback_events 失败: %w", err)
	}

	fbSignals := s.buildFeedbackSignals(ctx)
	if err := batchInsert(database, fbSignals, 50); err != nil {
		return fmt.Errorf("写入 feedback_signals 失败: %w", err)
	}

	promptCands := s.buildPromptCandidates()
	if err := batchInsert(database, promptCands, 50); err != nil {
		return fmt.Errorf("写入 prompt_candidates 失败: %w", err)
	}

	champions := s.buildChampionDialogues(ctx)
	if err := batchInsertWithEmbedding(database, champions, 50); err != nil {
		return fmt.Errorf("写入 champion_dialogues 失败: %w", err)
	}

	aiLogs := s.buildAISalesLogs(ctx)
	if err := batchInsert(database, aiLogs, 50); err != nil {
		return fmt.Errorf("写入 ai_sales_logs 失败: %w", err)
	}

	log.Printf("  ✓ 已写入 智能体%d+绑定%d+挂载%d+话术%d+异议%d+SOP%d+执行%d+记忆%d+意图%d+意向%d+反馈%d+信号%d+Prompt%d+销冠%d+日志%d",
		len(agents), len(bindings), len(csAgents), len(scripts), len(objections),
		len(sopAgents), len(sopExecs), len(memories), len(intents), len(intentScores),
		len(fbEvents), len(fbSignals), len(promptCands), len(champions), len(aiLogs))
	return nil
}


func (s *aiAgentsSeeder) buildAIAgents() []model.AIAgent {
	// hivemtk 平台知识库产品 ID（与迁移 031_platform_cs_rag_seed.sql、
	// scripts/seed/expand_knowledge_base*.py 中的 PRODUCT_ID 一致，均为 'hivemtk-platform-cs'）。
	// 该产品由初次安装的迁移 031 创建，本 cmd/seed 仅做绑定、不重复创建。
	const hivemtkRagProductID = "hivemtk-platform-cs"
	ragProducts := pq.StringArray{hivemtkRagProductID}
	agents := []model.AIAgent{
		{
			AgentCode:    "seed-deploy-consult-01",
			Name:         "开源部署咨询智能体 " + seedTag,
			Description:  "HiveMTK 开源部署咨询：make install 三步、.env 密钥、模型档位、FRP 公网穿透、初始化流程",
			AgentType:    string(model.AgentTypeCustomerService),
			AgentMode:    string(model.AgentModePassive),
			Persona:      "你是 HiveMTK 部署支持工程师，熟悉 Docker Compose 私域部署、make 命令、.env 配置、本地推理栈与 FRP 穿透，能给出可执行的安装与排障步骤。",
			SystemPrompt: "你是 HiveMTK 开源部署咨询助手。回答要求：1) 准确，不编造；2) 涉及部署/命令时给出具体可执行步骤（git clone / make install / vim .env / make up）；3) 区分 dev/prod 模型档位与硬件要求；4) 引导至 GitHub/Gitee 仓库或微信交流群。",
			Greeting:     "您好，我是 HiveMTK 部署咨询助手，可解答安装、初始化、模型档位、FRP 公网穿透、运维排障等问题。请问您在哪个环节需要帮助？",
			LLMModel:     "default",
			Temperature:  0.5,
			MaxTokens:    800,
			EnableRAG:    true, EnableScriptMatch: false, EnableHumanizePolish: false, EnableContentAudit: true, EnablePlaybook: false,
			RAGTopK:             5,
			ConfidenceThreshold: 0.7,
			MaxAIConsecutive:    4,
			Status:              1,
			Version:             1,
		},
		{
			AgentCode:    "seed-feature-explain-01",
			Name:         "平台功能讲解智能体 " + seedTag,
			Description:  "HiveMTK 功能讲解：七端接入、ReAct 自主智能体、三级 RAG、零出域数据安全、62 个核心模块、资产市场",
			AgentType:    string(model.AgentTypeCustomerService),
			AgentMode:    string(model.AgentModePassive),
			Persona:      "你是 HiveMTK 产品讲解员，熟悉项目全部功能模块与核心特色，能用通俗语言讲清七端接入、ReAct 智能体、三级 RAG 与零出域安全设计。",
			SystemPrompt: "你是 HiveMTK 功能讲解助手。回答要求：1) 准确，不编造；2) 讲清功能点对应的真实能力（七端渠道、41 个内置工具、三级检索、零出域）；3) 必要时对比 Dify/Coze 说明差异；4) 引导至文档与仓库。",
			Greeting:     "您好，我是 HiveMTK 功能讲解助手，可为您介绍七端接入、ReAct 智能体、三级 RAG、数据安全与资产市场等核心能力。想先了解哪块？",
			LLMModel:     "default",
			Temperature:  0.6,
			MaxTokens:    800,
			EnableRAG:    true, EnableScriptMatch: false, EnableHumanizePolish: false, EnableContentAudit: true, EnablePlaybook: false,
			RAGTopK:             5,
			ConfidenceThreshold: 0.7,
			MaxAIConsecutive:    4,
			Status:              1,
			Version:             1,
		},
		{
			AgentCode:    "seed-asset-market-01",
			Name:         "资产市场答疑智能体 " + seedTag,
			Description:  "HiveMTK 资产市场答疑：三端闭环、5 类资产包、SyncPull 拉取织入 RAG、ISV 贡献上架",
			AgentType:    string(model.AgentTypeCustomerService),
			AgentMode:    string(model.AgentModePassive),
			Persona:      "你是 HiveMTK 资产市场顾问，熟悉三端闭环（平台端/开发者端/商户端）、5 类资产包格式（JSONB）与 SyncPull 使用流程，能指导商户获取与 ISV 贡献资产。",
			SystemPrompt: "你是 HiveMTK 资产市场答疑助手。回答要求：1) 准确，不编造；2) 讲清资产市场三端职责与数据流铁律（user-web→user-server→platform-server）；3) 说明 5 类资产与 SyncPull 使用；4) 引导贡献者阅读 CONTRIBUTING.md。",
			Greeting:     "您好，我是 HiveMTK 资产市场答疑助手，可解答资产类型、获取使用、ISV 贡献上架等问题。请问您是想使用现成资产，还是作为开发者贡献资产？",
			LLMModel:     "default",
			Temperature:  0.6,
			MaxTokens:    800,
			EnableRAG:    true, EnableScriptMatch: false, EnableHumanizePolish: false, EnableContentAudit: true, EnablePlaybook: false,
			RAGTopK:             5,
			ConfidenceThreshold: 0.7,
			MaxAIConsecutive:    4,
			Status:              1,
			Version:             1,
		},
		{
			AgentCode:    "seed-contributor-support-01",
			Name:         "贡献者支持智能体 " + seedTag,
			Description:  "HiveMTK 社区与贡献支持：Gitee Issues、微信交流群、商务合作、CONTRIBUTING 与分层架构规范",
			AgentType:    string(model.AgentTypeCustomerService),
			AgentMode:    string(model.AgentModePassive),
			Persona:      "你是 HiveMTK 社区与贡献支持助手，熟悉贡献流程（Fork→分支→PR）、Gitee Issues 反馈渠道、微信交流群与商务合作邮箱，能引导开发者规范参与。",
			SystemPrompt: "你是 HiveMTK 社区与贡献支持助手。回答要求：1) 准确，不编造；2) 引导报 Bug/提需求到 Gitee Issues，贡献代码读 CONTRIBUTING.md 与分层架构规范；3) 提供微信交流群与商务合作邮箱；4) 不含定价/下载等已下线内容。",
			Greeting:     "您好，我是 HiveMTK 社区支持助手，可引导您报 Bug、提需求、参与代码贡献或联系商务合作。请问需要哪种支持？",
			LLMModel:     "default",
			Temperature:  0.6,
			MaxTokens:    700,
			EnableRAG:    true, EnableScriptMatch: false, EnableHumanizePolish: false, EnableContentAudit: true, EnablePlaybook: false,
			RAGTopK:             4,
			ConfidenceThreshold: 0.7,
			MaxAIConsecutive:    3,
			Status:              1,
			Version:             1,
		},
		{
			AgentCode:    "seed-channel-demo-01",
			Name:         "七端接入演示智能体 " + seedTag,
			Description:  "演示 HiveMTK 七端渠道接入与统一消息中心：抖音/快手/小红书/闲鱼/TikTok/微信/短信/邮件，及 embed-sdk 嵌入",
			AgentType:    string(model.AgentTypeHybrid),
			AgentMode:    string(model.AgentModePassive),
			Persona:      "你是 HiveMTK 渠道接入演示助手，熟悉七端原生接入（抖音/快手/小红书/闲鱼/TikTok/微信企业微信/短信/邮件）、embed-sdk 嵌入与统一消息中心。",
			SystemPrompt: "你是 HiveMTK 七端接入演示助手。回答要求：1) 准确，不编造；2) 说明各渠道接入方式与合规要求；3) 讲清 embed-sdk 嵌入任意网站的方式与 /api/chat/public/*、/api/ws/visitor 接口；4) 引导至文档。",
			Greeting:     "您好，我是 HiveMTK 渠道接入演示助手，可演示抖音/小红书/微信等七端接入、嵌入式客服与统一消息中心。想看哪个渠道的接入？",
			LLMModel:     "default",
			Temperature:  0.6,
			MaxTokens:    800,
			EnableRAG:    true, EnableScriptMatch: false, EnableHumanizePolish: false, EnableContentAudit: true, EnablePlaybook: false,
			RAGTopK:             5,
			ConfidenceThreshold: 0.7,
			MaxAIConsecutive:    4,
			Status:              1,
			Version:             1,
		},
		{
			AgentCode:    "seed-react-demo-01",
			Name:         "ReAct 智能体演示 " + seedTag,
			Description:  "演示 HiveMTK ReAct 自主智能体：感知→规划→调工具→反思（最多 5 轮）循环与 41 个内置工具",
			AgentType:    string(model.AgentTypeSales),
			AgentMode:    string(model.AgentModePassive),
			Persona:      "你是 HiveMTK ReAct 自主智能体演示助手，能讲解感知→规划→调工具→反思的自主决策循环，以及 41 个内置工具（查库存/查物流/查客户画像/改地址/加白名单等）。",
			SystemPrompt: "你是 HiveMTK ReAct 智能体演示助手。回答要求：1) 准确，不编造；2) 讲清 ReAct 循环与多智能体协作（被动应答 + 主动触达）；3) 说明一个 AIAgent = 人设+知识库+LLM+SOP+话术库+决策策略；4) 引导至文档与仓库。",
			Greeting:     "您好，我是 HiveMTK ReAct 智能体演示助手，可为您讲解自主智能体如何感知、规划、调用工具并反思。想了解哪部分？",
			LLMModel:     "default",
			Temperature:  0.7,
			MaxTokens:    800,
			EnableRAG:    true, EnableScriptMatch: true, EnableHumanizePolish: false, EnableContentAudit: true, EnablePlaybook: true,
			RAGTopK:             4,
			ConfidenceThreshold: 0.7,
			MaxAIConsecutive:    5,
			Status:              1,
			Version:             1,
		},
		{
			AgentCode:    "seed-data-compliance-01",
			Name:         "零出域合规答疑智能体 " + seedTag,
			Description:  "HiveMTK 数据安全与合规答疑：100% 私域零出域、本地推理栈、FRP 隧道、AGPL-3.0 义务",
			AgentType:    string(model.AgentTypeCustomerService),
			AgentMode:    string(model.AgentModePassive),
			Persona:      "你是 HiveMTK 数据安全与合规答疑助手，熟悉 100% 私域零出域设计、本地推理栈（llama.cpp + TEI）、FRP 穿透隧道，以及 AGPL-3.0 开源义务。",
			SystemPrompt: "你是 HiveMTK 数据安全与合规答疑助手。回答要求：1) 准确，不编造；2) 强调数据全程在客户内网、云端不落对话；3) 说明 AGPL-3.0 关键义务（网络服务须开源修改）；4) 引导至文档。",
			Greeting:     "您好，我是 HiveMTK 数据安全与合规答疑助手，可解答私域零出域、本地推理栈、FRP 穿透与 AGPL-3.0 协议义务。请问您关心哪方面？",
			LLMModel:     "default",
			Temperature:  0.4,
			MaxTokens:    700,
			EnableRAG:    true, EnableScriptMatch: false, EnableHumanizePolish: false, EnableContentAudit: true, EnablePlaybook: false,
			RAGTopK:             4,
			ConfidenceThreshold: 0.65,
			MaxAIConsecutive:    3,
			Status:              1,
			Version:             1,
		},
		{
			AgentCode:    "seed-model-inference-01",
			Name:         "模型与推理栈支持智能体 " + seedTag,
			Description:  "HiveMTK 本地推理栈与模型档位：mtk-llm/Embedding/Rerank、dev/prod 档、云端 LLM 兜底",
			AgentType:    string(model.AgentTypeCustomerService),
			AgentMode:    string(model.AgentModePassive),
			Persona:      "你是 HiveMTK 模型与推理栈支持助手，熟悉本地推理栈三服务（8207 LLM / 8208 Embedding / 8209 Rerank）、dev/prod 模型档位与云端 LLM 兜底配置。",
			SystemPrompt: "你是 HiveMTK 模型与推理栈支持助手。回答要求：1) 准确，不编造；2) 说明推理栈命令（inference-host-up/test/status）与档位切换；3) 强调 Embedding/Rerank 强制本地、LLM 可走云端；4) 引导至文档。",
			Greeting:     "您好，我是 HiveMTK 模型与推理栈支持助手，可解答本地推理栈启动、模型档位（dev/prod）、云端 LLM 兜底等问题。请问需要哪方面？",
			LLMModel:     "default",
			Temperature:  0.5,
			MaxTokens:    800,
			EnableRAG:    true, EnableScriptMatch: false, EnableHumanizePolish: false, EnableContentAudit: true, EnablePlaybook: false,
			RAGTopK:             5,
			ConfidenceThreshold: 0.7,
			MaxAIConsecutive:    4,
			Status:              1,
			Version:             1,
		},
		{
			AgentCode:    "seed-proactive-reach-01",
			Name:         "主动触达合规顾问 " + seedTag,
			Description:  "HiveMTK 主动触达合规顾问：短信/邮件/私信/Telegram 主动推送的合规边界与 [COMPLIANCE] 日志",
			AgentType:    string(model.AgentTypeCustomerService),
			AgentMode:    string(model.AgentModeActive),
			Persona:      "你是 HiveMTK 主动触达合规顾问，清楚各渠道平台规范与法律边界，指导仅向已授权联系人发送、禁止垃圾营销/欺诈/骚扰等内容，并说明每次触达的 [COMPLIANCE] 日志不可关闭。",
			SystemPrompt: "你是 HiveMTK 主动触达合规顾问。原则：1) 必须遵守各渠道平台规范与所在地法律；2) 仅向已授权联系人发送；3) 禁止垃圾营销/欺诈/骚扰/钓鱼/色情/赌博/侵权；4) 说明 [COMPLIANCE] 日志不可关闭。",
			Greeting:     "您好，我是 HiveMTK 主动触达合规顾问，可为您说明短信/邮件/私信等主动推送的合规边界与日志要求。",
			LLMModel:     "default",
			Temperature:  0.6,
			MaxTokens:    600,
			EnableRAG:    false, EnableScriptMatch: true, EnableHumanizePolish: true, EnableContentAudit: true, EnablePlaybook: false,
			RAGTopK:             0,
			ConfidenceThreshold: 0.6,
			MaxAIConsecutive:    2,
			Status:              1,
			Version:             1,
		},
		{
			AgentCode:    "seed-industry-solution-01",
			Name:         "行业方案咨询智能体 " + seedTag,
			Description:  "HiveMTK 行业方案咨询：基于 7 类行业资产（电子烟/成人/两性/租车/民宿/货代/移民）讲解落地场景",
			AgentType:    string(model.AgentTypeHybrid),
			AgentMode:    string(model.AgentModePassive),
			Persona:      "你是 HiveMTK 行业方案咨询助手，熟悉平台预置的 7 类行业资产（电子烟/成人用品/两性健康/租车/民宿/货代/移民）与对应落地场景，能讲清如何用智能体+知识库服务具体行业。",
			SystemPrompt: "你是 HiveMTK 行业方案咨询助手。回答要求：1) 准确，不编造；2) 基于真实预置行业资产说明落地场景；3) 说明可通过资产市场获取行业 SOP/话术；4) 引导至文档与微信交流群。",
			Greeting:     "您好，我是 HiveMTK 行业方案咨询助手，可基于预置的 7 类行业资产为您讲解私域营销落地场景。您关注哪个行业？",
			LLMModel:     "default",
			Temperature:  0.6,
			MaxTokens:    800,
			EnableRAG:    true, EnableScriptMatch: true, EnableHumanizePolish: false, EnableContentAudit: true, EnablePlaybook: false,
			RAGTopK:             4,
			ConfidenceThreshold: 0.7,
			MaxAIConsecutive:    4,
			Status:              1,
			Version:             1,
		},
		{
			AgentCode:    "seed-hivemtk-product-service",
			Name:         "hivemtk 产品服务智能体 " + seedTag,
			Description:  "HiveMTK 项目自身的客服智能体：介绍项目功能/技术架构/接入部署/开源生态，默认绑网页客服 + Telegram",
			AgentType:    string(model.AgentTypeCustomerService),
			AgentMode:    string(model.AgentModePassive),
			Persona:      "你是 HiveMTK 官方客服助手，负责解答关于本开源项目的咨询。你熟悉项目的开源协议（AGPL-3.0）、部署方式（Docker Compose 私域独立部署）、技术架构（Go + Gin + GORM + pgvector / Vue3 + Element Plus）、功能模块（七端社媒接入、ReAct 自主 AI 智能体、三级 RAG 检索、零出域数据安全）、资产市场与社区。回答准确简洁，引导至 GitHub/Gitee 仓库或微信群获取更多帮助。",
			SystemPrompt: "你是 HiveMTK 官方客服助手。回答要求：1) 准确，不编造；2) 简洁，单次回复不超过 200 字；3) 涉及部署/命令时给出具体步骤；4) 不涉及定价、版本下载、注册开户等已下线内容；5) 引导至 GitHub/Gitee 仓库或微信群获取更多帮助。回答依据下方检索到的知识片段。",
			Greeting:     "您好，我是 HiveMTK 官方客服助手，可为您解答关于项目开源信息、部署、运维、架构、资产市场、AI 智能体等问题。请问有什么可以帮您？",
			LLMModel:     "default",
			Temperature:  0.5,
			MaxTokens:    800,
			EnableRAG:    true, EnableScriptMatch: true, EnableHumanizePolish: true, EnableContentAudit: true, EnablePlaybook: true,
			RAGTopK:             5,
			ConfidenceThreshold: 0.7,
			MaxAIConsecutive:    4,
			Status:              1,
			Version:             1,
		},
	}
	hivemtkKBAgents := map[string]bool{
		"seed-hivemtk-product-service": true,
		"seed-deploy-consult-01":       true,
		"seed-feature-explain-01":      true,
		"seed-asset-market-01":         true,
		"seed-contributor-support-01":  true,
		"seed-channel-demo-01":         true,
		"seed-react-demo-01":           true,
		"seed-data-compliance-01":      true,
		"seed-model-inference-01":      true,
		"seed-industry-solution-01":    true,
	}
	for i := range agents {
		if hivemtkKBAgents[agents[i].AgentCode] {
			agents[i].RagProductIDs = ragProducts
		}
	}
	return agents
}

func (s *aiAgentsSeeder) buildBindings(agents []model.AIAgent) []model.ChannelAgentBinding {
	if len(agents) < 11 {
		log.Printf("[WARN] 智能体不足 11 个，buildBindings 跳过部分默认绑定（实际=%d）", len(agents))
	}
	hivemtkSvc := model.AIAgent{}
	if len(agents) > 10 {
		hivemtkSvc = agents[10]
	}
	bindings := []model.ChannelAgentBinding{
		{ChannelType: string(model.ChannelTypeWeb), AccountID: "default", AgentID: hivemtkSvc.ID, IsPrimary: true, Enabled: true},
		{ChannelType: string(model.ChannelTypeTelegram), AccountID: "default", AgentID: hivemtkSvc.ID, IsPrimary: true, Enabled: true},

		{ChannelType: string(model.ChannelTypeWeCom), AccountID: "seed-wecom-001", AgentID: agents[0].ID, IsPrimary: true, Enabled: true},
		{ChannelType: string(model.ChannelTypeWeCom), AccountID: "seed-wecom-002", AgentID: agents[1].ID, IsPrimary: true, Enabled: true},
		{ChannelType: string(model.ChannelTypeWhatsApp), AccountID: "seed-wa-001", AgentID: agents[0].ID, IsPrimary: true, Enabled: true},
		{ChannelType: string(model.ChannelTypeFeishu), AccountID: "seed-feishu-001", AgentID: agents[2].ID, IsPrimary: true, Enabled: true},
		{ChannelType: string(model.ChannelTypeDingTalk), AccountID: "seed-dingtalk-001", AgentID: agents[2].ID, IsPrimary: false, Enabled: true},
		{ChannelType: string(model.ChannelTypeTelegram), AccountID: "seed-tg-001", AgentID: agents[3].ID, IsPrimary: true, Enabled: true},
		{ChannelType: string(model.ChannelTypeDouyin), AccountID: "seed-dy-001", AgentID: agents[1].ID, IsPrimary: true, Enabled: true},
		{ChannelType: string(model.ChannelTypeXiaohongshu), AccountID: "seed-xhs-001", AgentID: agents[4].ID, IsPrimary: true, Enabled: true},
	}
	return bindings
}

func (s *aiAgentsSeeder) buildCustomerServiceAgents(agents []model.AIAgent, ctx *SeedContext) []model.CustomerServiceAgent {
	if len(ctx.AgentStatusIDs) == 0 || len(agents) < 3 {
		return nil
	}
	csAgents := []model.CustomerServiceAgent{
		{AgentStatusID: ctx.AgentStatusIDs[0], AIAgentID: agents[0].ID, IsPrimary: true, Enabled: true},
		{AgentStatusID: ctx.AgentStatusIDs[1], AIAgentID: agents[2].ID, IsPrimary: true, Enabled: true},
		{AgentStatusID: ctx.AgentStatusIDs[2], AIAgentID: agents[1].ID, IsPrimary: false, Enabled: true},
	}
	return csAgents
}

func (s *aiAgentsSeeder) buildScripts() []model.ScriptLibrary {
	categories := []struct {
		Category    string
		Subcategory string
		Scenario    string
	}{
		{"开场白", "通用", "greeting"},
		{"开场白", "高客单价", "greeting"},
		{"需求挖掘", "SPIN提问", "discovery"},
		{"需求挖掘", "痛点引导", "discovery"},
		{"产品介绍", "FAB法则", "presentation"},
		{"产品介绍", "场景化描述", "presentation"},
		{"异议处理", "价格异议", "objection"},
		{"异议处理", "效果异议", "objection"},
		{"异议处理", "信任异议", "objection"},
		{"逼单技巧", "稀缺性", "closing"},
		{"逼单技巧", "限时优惠", "closing"},
		{"邀约到店", "首次邀约", "invitation"},
		{"邀约到店", "二次邀约", "invitation"},
		{"跟进激活", "沉默唤醒", "followup"},
		{"跟进激活", "流失挽回", "followup"},
		{"复购运营", "增购推荐", "repurchase"},
		{"复购运营", "转介绍", "repurchase"},
		{"售后处理", "物流咨询", "aftersale"},
		{"售后处理", "退换货", "aftersale"},
		{"售后处理", "质量问题", "aftersale"},
	}
	scripts := make([]model.ScriptLibrary, 0, len(categories))
	for i, c := range categories {
		sc := model.ScriptLibrary{
			Category:       c.Category,
			Subcategory:    c.Subcategory,
			Title:          fmt.Sprintf("%s·%s话术 #%d %s", c.Category, c.Subcategory, i+1, seedTag),
			Content:        fmt.Sprintf("【%s】%s：尊敬的客户，%s。我们提供%s，让您放心选择。"+seedTag, c.Category, c.Subcategory, s.scriptContent(c.Scenario), s.scriptBenefit(c.Scenario)),
			Scenario:       c.Scenario,
			Tags:           model.JSONArray{c.Category, c.Subcategory},
			UsageCount:     randInt(10, 500),
			SuccessCount:   randInt(5, 250),
			ConversionRate: randFloat(15, 75),
			IsFeatured:     i%5 == 0,
		}
		scripts = append(scripts, sc)
	}
	return scripts
}

func (s *aiAgentsSeeder) scriptContent(scenario string) string {
	contents := map[string]string{
		"greeting":     "很高兴为您服务，请问有什么可以帮助您的",
		"discovery":    "为了给您推荐最合适的方案，想了解您的具体需求",
		"presentation": "我们的产品采用最新技术，具有以下三大优势",
		"objection":    "完全理解您的顾虑，让我为您详细解答",
		"closing":      "我们提供清晰透明的方案与按需选择的选项，您可按实际需求决定",
		"invitation":   "我们近期有新品体验活动，诚邀您到店",
		"followup":     "好久不见，为您准备了一份专属福利",
		"repurchase":   "感谢您一直以来的支持，为您准备了老客专属权益",
		"aftersale":    "抱歉给您带来不便，我们会立即处理",
	}
	if c, ok := contents[scenario]; ok {
		return c
	}
	return "为您服务"
}

func (s *aiAgentsSeeder) scriptBenefit(scenario string) string {
	benefits := map[string]string{
		"greeting":     "7×24 小时专属服务",
		"discovery":    "免费需求评估",
		"presentation": "按需交付与验收",
		"objection":    "透明方案说明",
		"closing":      "限时不限量",
		"invitation":   "免费体验装",
		"followup":     "老客专享折扣",
		"repurchase":   "积分双倍",
		"aftersale":    "1 对 1 售后专员",
	}
	if b, ok := benefits[scenario]; ok {
		return b
	}
	return "品质保障"
}

func (s *aiAgentsSeeder) buildObjections() []model.ObjectionTemplate {
	objectionTypes := []struct {
		Type     string
		Keyword  string
		Pattern  string
		Strategy string
	}{
		{"price", "太贵了", ".*(太贵|便宜|价格高).*", "价值锚定+对比法"},
		{"price", "预算不够", ".*(预算|没钱).*", "分期方案+权益前置"},
		{"price", "其他家更便宜", ".*(其他.*便宜|别家.*低).*", "差异化对比+服务保障"},
		{"price", "再考虑下", ".*(考虑|想想).*", "限时优惠+决策催化"},
		{"effect", "效果怎么样", ".*(效果|有用吗|管用).*", "案例展示+数据支撑"},
		{"effect", "不适合我", ".*(不适合|不合适).*", "需求重新评估+定制方案"},
		{"effect", "以前用过没用", ".*(用过|试过|没用).*", "原因分析+改进方案"},
		{"trust", "没听过这个品牌", ".*(没听过|不知道|没名气).*", "品牌背书+资质展示"},
		{"trust", "怕是假货", ".*(假货|正品|真伪).*", "溯源系统+授权证明"},
		{"trust", "售后怎么样", ".*(售后|保修|支持).*", "开源社区支持+文档齐全+可加交流群"},
		{"trust", "可以签合同吗", ".*(合同|协议|保障).*", "电子合同+法律效力"},
		{"competitor", "已经在用XX了", ".*(在用|用了.*了).*", "差异化+迁移成本补贴"},
		{"competitor", "朋友推荐其他家", ".*(朋友|推荐).*", "尊重选择+提供对比表"},
		{"timing", "现在不急", ".*(不急|以后|再说).*", "稀缺性+限时不等待"},
		{"timing", "和家人商量下", ".*(家人|商量|老婆).*", "提供决策资料+家人专属权益"},
	}
	objections := make([]model.ObjectionTemplate, 0, len(objectionTypes))
	for _, o := range objectionTypes {
		obj := model.ObjectionTemplate{
			ObjectionType:    o.Type,
			ObjectionKeyword: o.Keyword,
			ObjectionPattern: o.Pattern,
			ReplyTemplate:    fmt.Sprintf("【%s】我完全理解您的顾虑。%s"+seedTag, o.Keyword, s.objectionReply(o.Type)),
			ReplyStrategy:    o.Strategy,
			ExampleReply:     fmt.Sprintf("客户：%s\n销售：我完全理解您的顾虑。%s"+seedTag, o.Keyword, s.objectionReply(o.Type)),
			UseCount:         randInt(20, 300),
			SuccessCount:     randInt(10, 180),
			IsActive:         true,
			Priority:         randInt(1, 10),
		}
		objections = append(objections, obj)
	}
	return objections
}

func (s *aiAgentsSeeder) objectionReply(objType string) string {
	replies := map[string]string{
		"price":      "我们提供清晰的方案与价格明细，您可以先了解功能再决定；如预算有限，建议从轻量方案起步，按需扩展。",
		"effect":     "效果因场景而异，建议您从一个具体场景先试用，用真实数据做判断，而不是凭宣传。",
		"trust":      "我们是正规产品，可提供资质与协议说明；建议您先小范围验证，确认符合预期后再规模使用。",
		"competitor": "尊重您的选择。我们可以提供详细对比表，包括功能、价格、服务，帮您做出最优决策。",
		"timing":     "完全理解。购买时机由您决定，我们随时为您提供支持与资料，您可按自身节奏推进。",
	}
	if r, ok := replies[objType]; ok {
		return r
	}
	return "我们会为您提供最合适的方案。"
}

func (s *aiAgentsSeeder) buildSOPAgents() []model.SOPAgent {
	scenarios := []struct {
		Name       string
		Scenario   string
		Trigger    string
		Desc       string
		SOPGraph   model.JSONMap
		TriggerCfg model.JSONMap
		Priority   int
		ABVariants []any
	}{
		{
			Name:     "HiveMTK 新访客引导 SOP " + seedTag,
			Scenario: "deploy_guide",
			Trigger:  "intent_change",
			Desc:     "新访客进站→意图识别→方案介绍→引导部署/加群完整链路",
			SOPGraph: model.JSONMap{
				"nodes": []any{
					map[string]any{"id": "start", "type": "start", "name": "开始"},
					map[string]any{"id": "greet", "type": "message", "name": "欢迎+项目介绍"},
					map[string]any{"id": "discover", "type": "question", "name": "意图识别（部署/功能/报错）"},
					map[string]any{"id": "present", "type": "branch", "name": "方案介绍（按意图）"},
					map[string]any{"id": "objection", "type": "branch", "name": "异议处理"},
					map[string]any{"id": "close", "type": "message", "name": "引导部署/加群"},
					map[string]any{"id": "transfer", "type": "action", "name": "转人工（复杂/商务）"},
					map[string]any{"id": "end", "type": "end", "name": "结束"},
				},
				"edges": []any{
					map[string]any{"from": "start", "to": "greet"},
					map[string]any{"from": "greet", "to": "discover"},
					map[string]any{"from": "discover", "to": "present", "condition": "intent=consult"},
					map[string]any{"from": "present", "to": "objection", "condition": "has_objection"},
					map[string]any{"from": "present", "to": "close", "condition": "no_objection"},
					map[string]any{"from": "objection", "to": "close", "condition": "objection_resolved"},
					map[string]any{"from": "close", "to": "transfer", "condition": "complexity>=high"},
					map[string]any{"from": "close", "to": "end", "condition": "complexity<high"},
				},
			},
			TriggerCfg: model.JSONMap{
				"trigger_intent": "consult",
				"delay":          "0s",
				"condition":      "new_visitor=true",
				"max_duration":   "30m",
			},
			Priority: 100,
			ABVariants: []any{
				map[string]any{"name": "A_标准", "weight": 50},
				map[string]any{"name": "B_亲和", "weight": 50},
			},
		},
		{
			Name:     "HiveMTK 沉默用户唤醒 SOP " + seedTag,
			Scenario: "dormant_wake",
			Trigger:  "timer_24h",
			Desc:     "高意向/沉默用户分阶段触达，推送教程/案例/加群，深化参与",
			SOPGraph: model.JSONMap{
				"nodes": []any{
					map[string]any{"id": "start", "type": "start", "name": "开始"},
					map[string]any{"id": "check_status", "type": "action", "name": "查询活跃/版本状态"},
					map[string]any{"id": "personalize", "type": "message", "name": "个性化问候"},
					map[string]any{"id": "value_add", "type": "message", "name": "资料补充（教程/案例/更新）"},
					map[string]any{"id": "offer", "type": "message", "name": "邀请加群/看更新"},
					map[string]any{"id": "wait_response", "type": "wait", "name": "等待回复"},
					map[string]any{"id": "end", "type": "end", "name": "结束"},
				},
				"edges": []any{
					map[string]any{"from": "start", "to": "check_status"},
					map[string]any{"from": "check_status", "to": "personalize"},
					map[string]any{"from": "personalize", "to": "value_add"},
					map[string]any{"from": "value_add", "to": "offer"},
					map[string]any{"from": "offer", "to": "wait_response"},
					map[string]any{"from": "wait_response", "to": "end", "condition": "timeout_24h"},
				},
			},
			TriggerCfg: model.JSONMap{
				"trigger_intent": "deploy_intent",
				"delay":          "24h",
				"condition":      "intent_score>=60",
				"max_executions": 3,
			},
			Priority: 90,
			ABVariants: []any{
				map[string]any{"name": "A_教程型", "weight": 50},
				map[string]any{"name": "B_社群型", "weight": 50},
			},
		},
		{
			Name:     "HiveMTK 开源支持 SOP " + seedTag,
			Scenario: "oss_support",
			Trigger:  "intent_objection",
			Desc:     "报错/需求/贡献/商务 4 类分流，确保 12h 内首响（Gitee Issues）",
			SOPGraph: model.JSONMap{
				"nodes": []any{
					map[string]any{"id": "start", "type": "start", "name": "开始"},
					map[string]any{"id": "apology", "type": "message", "name": "安抚+认领"},
					map[string]any{"id": "collect_info", "type": "question", "name": "收集环境+报错/复现"},
					map[string]any{"id": "classify", "type": "branch", "name": "问题分类"},
					map[string]any{"id": "deploy_solution", "type": "message", "name": "部署问题方案"},
					map[string]any{"id": "feature_solution", "type": "message", "name": "功能/需求方案（建 Issue）"},
					map[string]any{"id": "bug_solution", "type": "message", "name": "报错排查方案"},
					map[string]any{"id": "contrib_solution", "type": "message", "name": "贡献引导方案"},
					map[string]any{"id": "confirm", "type": "question", "name": "确认方案"},
					map[string]any{"id": "execute", "type": "action", "name": "建 Issue/加群/转商务"},
					map[string]any{"id": "followup", "type": "wait", "name": "3天回访"},
					map[string]any{"id": "end", "type": "end", "name": "结束"},
				},
				"edges": []any{
					map[string]any{"from": "start", "to": "apology"},
					map[string]any{"from": "apology", "to": "collect_info"},
					map[string]any{"from": "collect_info", "to": "classify"},
					map[string]any{"from": "classify", "to": "deploy_solution", "condition": "type=deploy"},
					map[string]any{"from": "classify", "to": "feature_solution", "condition": "type=feature"},
					map[string]any{"from": "classify", "to": "bug_solution", "condition": "type=bug"},
					map[string]any{"from": "classify", "to": "contrib_solution", "condition": "type=contrib"},
					map[string]any{"from": "deploy_solution", "to": "confirm"},
					map[string]any{"from": "feature_solution", "to": "confirm"},
					map[string]any{"from": "bug_solution", "to": "confirm"},
					map[string]any{"from": "contrib_solution", "to": "confirm"},
					map[string]any{"from": "confirm", "to": "execute"},
					map[string]any{"from": "execute", "to": "followup"},
					map[string]any{"from": "followup", "to": "end"},
				},
			},
			TriggerCfg: model.JSONMap{
				"trigger_intent": "support",
				"delay":          "0s",
				"max_duration":   "30m",
				"require_human":  false,
			},
			Priority: 95,
			ABVariants: []any{
				map[string]any{"name": "A_快速", "weight": 50},
				map[string]any{"name": "B_关怀", "weight": 50},
			},
		},
		{
			Name:     "HiveMTK 版本发布通知 SOP " + seedTag,
			Scenario: "release_notify",
			Trigger:  "intent_high",
			Desc:     "新版本发布后推送更新摘要+升级步骤，覆盖重要修复/新功能",
			SOPGraph: model.JSONMap{
				"nodes": []any{
					map[string]any{"id": "start", "type": "start", "name": "开始"},
					map[string]any{"id": "value_present", "type": "message", "name": "更新摘要（FAB+案例）"},
					map[string]any{"id": "scarcity", "type": "message", "name": "重要提醒（安全/兼容）"},
					map[string]any{"id": "discount_stack", "type": "message", "name": "升级步骤"},
					map[string]any{"id": "objection_check", "type": "branch", "name": "异议检测"},
					map[string]any{"id": "handle_objection", "type": "message", "name": "异议处理"},
					map[string]any{"id": "final_offer", "type": "message", "name": "终极建议+限时"},
					map[string]any{"id": "close_yes", "type": "action", "name": "升级（推 make up）"},
					map[string]any{"id": "close_no", "type": "message", "name": "加群+长期培育"},
					map[string]any{"id": "end", "type": "end", "name": "结束"},
				},
				"edges": []any{
					map[string]any{"from": "start", "to": "value_present"},
					map[string]any{"from": "value_present", "to": "scarcity"},
					map[string]any{"from": "scarcity", "to": "discount_stack"},
					map[string]any{"from": "discount_stack", "to": "objection_check"},
					map[string]any{"from": "objection_check", "to": "handle_objection", "condition": "has_objection"},
					map[string]any{"from": "objection_check", "to": "final_offer", "condition": "no_objection"},
					map[string]any{"from": "handle_objection", "to": "final_offer"},
					map[string]any{"from": "final_offer", "to": "close_yes", "condition": "accept"},
					map[string]any{"from": "final_offer", "to": "close_no", "condition": "reject"},
					map[string]any{"from": "close_yes", "to": "end"},
					map[string]any{"from": "close_no", "to": "end"},
				},
			},
			TriggerCfg: model.JSONMap{
				"trigger_intent": "deploy_intent",
				"delay":          "0s",
				"condition":      "intent_score>=80 AND has_release=true",
				"max_duration":   "10m",
			},
			Priority: 85,
			ABVariants: []any{
				map[string]any{"name": "A_数字型", "weight": 50},
				map[string]any{"name": "B_故事型", "weight": 50},
			},
		},
		{
			Name:     "HiveMTK 社区贡献引导 SOP " + seedTag,
			Scenario: "contributor_guide",
			Trigger:  "churn_30d",
			Desc:     "按贡献活跃度分阶段触达，推送文档/加群/1v1，引导参与开源",
			SOPGraph: model.JSONMap{
				"nodes": []any{
					map[string]any{"id": "start", "type": "start", "name": "开始"},
					map[string]any{"id": "rfm_check", "type": "action", "name": "查询贡献活跃度"},
					map[string]any{"id": "channel_select", "type": "branch", "name": "渠道选择"},
					map[string]any{"id": "sms_send", "type": "action", "name": "文档推送（30天）"},
					map[string]any{"id": "wechat_send", "type": "action", "name": "加群邀请（60天）"},
					map[string]any{"id": "phone_call", "type": "action", "name": "1v1 沟通（90天+核心贡献者）"},
					map[string]any{"id": "wait_response", "type": "wait", "name": "等待 48h"},
					map[string]any{"id": "second_touch", "type": "action", "name": "二次触达（不同资料）"},
					map[string]any{"id": "end", "type": "end", "name": "结束（无响应）"},
				},
				"edges": []any{
					map[string]any{"from": "start", "to": "rfm_check"},
					map[string]any{"from": "rfm_check", "to": "channel_select"},
					map[string]any{"from": "channel_select", "to": "sms_send", "condition": "rfm=30d"},
					map[string]any{"from": "channel_select", "to": "wechat_send", "condition": "rfm=60d"},
					map[string]any{"from": "channel_select", "to": "phone_call", "condition": "rfm>=90d AND is_core=true"},
					map[string]any{"from": "sms_send", "to": "wait_response"},
					map[string]any{"from": "wechat_send", "to": "wait_response"},
					map[string]any{"from": "phone_call", "to": "wait_response"},
					map[string]any{"from": "wait_response", "to": "second_touch", "condition": "no_response"},
					map[string]any{"from": "wait_response", "to": "end", "condition": "responded"},
					map[string]any{"from": "second_touch", "to": "end"},
				},
			},
			TriggerCfg: model.JSONMap{
				"trigger_event":  "contribution_segment_change",
				"delay":          "0s",
				"condition":      "segment=at_risk OR segment=churn",
				"max_executions": 2,
			},
			Priority: 80,
			ABVariants: []any{
				map[string]any{"name": "A_文档型", "weight": 50},
				map[string]any{"name": "B_社群型", "weight": 50},
			},
		},
	}
	sops := make([]model.SOPAgent, 0, len(scenarios))
	for _, sc := range scenarios {
		sop := model.SOPAgent{
			Name:           sc.Name,
			Scenario:       sc.Scenario,
			Description:    sc.Desc,
			TriggerType:    sc.Trigger,
			TriggerConfig:  sc.TriggerCfg,
			SOPGraph:       sc.SOPGraph,
			Version:        1,
			IsActive:       true,
			Priority:       sc.Priority,
			ExecutionCount: randInt(50, 500),
			SuccessCount:   randInt(20, 250),
			ABTestConfig: model.JSONMap{
				"enabled":  true,
				"variants": sc.ABVariants,
				"salt":     "customer_id",
			},
			UseBandit: true,
		}
		sops = append(sops, sop)
	}
	return sops
}

func (s *aiAgentsSeeder) buildSOPExecutions(sops []model.SOPAgent, ctx *SeedContext) []model.SOPExecution {
	if len(sops) == 0 || len(ctx.CustomerIDs) == 0 {
		return nil
	}
	statuses := []string{"running", "completed", "completed", "completed", "failed", "completed"}
	nodes := []string{"start", "greet", "discover", "present", "objection", "close", "end"}
	executions := make([]model.SOPExecution, 0, 15)
	for i := 0; i < 15; i++ {
		sop := sops[i%len(sops)]
		customerID := ctx.CustomerIDs[i%len(ctx.CustomerIDs)]
		status := statuses[i%len(statuses)]
		node := nodes[i%len(nodes)]
		startedAt := daysAgo(randInt(0, 25))
		var completedAt *time.Time
		if status != "running" {
			t := startedAt.Add(time.Duration(randInt(30, 3600)) * time.Second)
			completedAt = &t
		}
		sessionID := ""
		if len(ctx.SessionIDs) > 0 {
			sessionID = ctx.SessionIDs[i%len(ctx.SessionIDs)]
		}
		variant := "A"
		if i%2 == 1 {
			variant = "B"
		}
		exec := model.SOPExecution{
			SOPID:          sop.ID,
			CustomerID:     customerID,
			SessionID:      sessionID,
			CurrentNode:    node,
			CurrentNodeIdx: randInt(0, 6),
			Status:         status,
			ExecutionData:  model.JSONMap{"seed": seedTag, "variant": variant, "step": node},
			StartedAt:      startedAt,
			CompletedAt:    completedAt,
			ErrorMessage: func() string {
				if status == "failed" {
					return "客户超时未回复，自动终止"
				}
				return ""
			}(),
			Variant:      variant,
			AttemptCount: randInt(0, 2),
			TraceID:      fmt.Sprintf("seed-trace-%d", i+1),
		}
		executions = append(executions, exec)
	}
	return executions
}

func (s *aiAgentsSeeder) buildMemories(ctx *SeedContext) []model.DialogueMemory {
	if len(ctx.ChampionCustomerIDs) == 0 {
		return nil
	}
	memories := make([]model.DialogueMemory, 0, 8)
	for i, custID := range ctx.ChampionCustomerIDs {
		mem := model.DialogueMemory{
			SessionID:  fmt.Sprintf("seed-sess-mem-%d", i+1),
			CustomerID: custID,
			Summary:    fmt.Sprintf("第%d位高价值客户对话摘要：客户对产品表现出明显兴趣，预算充足，决策周期较短。%s", i+1, seedTag),
			KeyFacts: model.JSONMap{
				"budget":         fmt.Sprintf("%d-%d万", 5+i, 10+i),
				"timeline":       "1周内",
				"decision_maker": "本人",
				"pain_point":     "现有方案效率低",
			},
			CustomerName:         fmt.Sprintf("高价值客户%d", i+1),
			CustomerPhone:        fmt.Sprintf("139%08d", i+1),
			CustomerWechat:       fmt.Sprintf("seed_wx_%d", i+1),
			Budget:               fmt.Sprintf("%d-%d万元", 5+i, 10+i),
			Demand:               "提升运营效率、降低人力成本、数据可视化决策",
			Objections:           model.JSONArray{"价格偏高", "需要对比其他方案", "决策需家人参与"},
			PurchaseIntent:       []string{"high", "medium", "high", "high", "medium", "high", "medium", "high"}[i%8],
			IntentTrail:          model.JSONArray{"initial咨询", "深度沟通", "比价阶段", "决策阶段"},
			SOPHistory:           model.JSONArray{fmt.Sprintf("SOP-%d-执行", i+1)},
			LastAction:           "发送报价单",
			NextActionSuggestion: "跟进报价反馈+邀约演示",
			LastActiveAt:         hoursAgo(randInt(1, 72)),
			MessageCount:         randInt(10, 50),
		}
		memories = append(memories, mem)
	}
	return memories
}

func (s *aiAgentsSeeder) buildIntentRecords(ctx *SeedContext) []model.IntentRecord {
	if len(ctx.SessionIDs) == 0 || len(ctx.CustomerIDs) == 0 {
		return nil
	}
	intentTypes := []struct {
		Type    string
		Subtype string
		ConfMin float64
		ConfMax float64
	}{
		{"consult", "product_info", 0.75, 0.95},
		{"price_inquiry", "quote_request", 0.8, 0.98},
		{"price_inquiry", "discount_request", 0.7, 0.9},
		{"objection", "price_objection", 0.65, 0.88},
		{"objection", "trust_objection", 0.6, 0.85},
		{"purchase_intent", "ready_to_buy", 0.85, 0.99},
		{"purchase_intent", "considering", 0.6, 0.8},
		{"after_sale", "logistics_query", 0.7, 0.92},
		{"after_sale", "return_request", 0.8, 0.95},
		{"complaint", "quality_issue", 0.75, 0.9},
	}
	records := make([]model.IntentRecord, 0, 20)
	rawTexts := []string{
		"这个产品多少钱？", "有优惠吗？", "效果怎么样？", "以前用过类似的",
		"价格太高了", "可以便宜点吗？", "我想下单", "再考虑下",
		"我的订单到哪了？", "想退货", "质量有问题", "可以签合同吗",
		"家人不同意", "其他家更便宜", "效果不好怎么办", "保修多久",
		"怎么付款", "支持分期吗", "什么时候发货", "有发票吗",
	}
	for i := 0; i < 20; i++ {
		it := intentTypes[i%len(intentTypes)]
		rawText := rawTexts[i%len(rawTexts)] + " " + seedTag
		conf := randFloat(it.ConfMin, it.ConfMax)
		var level string
		if conf >= 0.85 {
			level = "high"
		} else if conf >= 0.65 {
			level = "medium"
		} else {
			level = "low"
		}
		rec := model.IntentRecord{
			SessionID:       ctx.SessionIDs[i%len(ctx.SessionIDs)],
			CustomerID:      ctx.CustomerIDs[i%len(ctx.CustomerIDs)],
			MessageID:       uint(i + 1),
			RawText:         rawText,
			IntentType:      it.Type,
			IntentSubtype:   it.Subtype,
			Confidence:      conf,
			ConfidenceLevel: level,
			Entities:        model.JSONMap{"product": "HiveMTK企业版", "amount": randInt(1000, 50000)},
			Sentiment:       []string{"positive", "neutral", "negative"}[i%3],
			LLMModel:        "default",
			CostTokens:      randInt(50, 500),
			LatencyMs:       randInt(100, 2000),
		}
		records = append(records, rec)
	}
	return records
}

func (s *aiAgentsSeeder) buildIntentScores(ctx *SeedContext) []model.SalesIntentScore {
	if len(ctx.CustomerIDs) == 0 {
		return nil
	}
	levels := []struct {
		Level    string
		ScoreMin float64
		ScoreMax float64
	}{
		{"cold", 0, 30},
		{"warm", 30, 60},
		{"hot", 60, 85},
		{"urgent", 85, 100},
	}
	scores := make([]model.SalesIntentScore, 0, 12)
	for i := 0; i < 12; i++ {
		lv := levels[i%len(levels)]
		score := randFloat(lv.ScoreMin, lv.ScoreMax)
		lastMsg := hoursAgo(randInt(1, 168))
		scoreRec := model.SalesIntentScore{
			CustomerID:        ctx.CustomerIDs[i%len(ctx.CustomerIDs)],
			TotalScore:        score,
			IntentLevel:       lv.Level,
			Dimensions:        model.JSONMap{"behavior": score * 0.9, "content": score * 1.05, "frequency": score * 0.95, "profile": score},
			BehaviorScore:     randFloat(20, 95),
			ContentScore:      randFloat(20, 95),
			FrequencyScore:    randFloat(20, 95),
			ProfileScore:      randFloat(30, 90),
			LastMessageAt:     &lastMsg,
			LastIntentType:    []string{"consult", "price_inquiry", "objection", "purchase_intent"}[i%4],
			LastScoreChange:   randFloat(-5, 10),
			RecommendedAction: s.intentAction(lv.Level) + " " + seedTag,
		}
		scores = append(scores, scoreRec)
	}
	return scores
}

func (s *aiAgentsSeeder) intentAction(level string) string {
	switch level {
	case "cold":
		return "持续培育，发送行业资讯"
	case "warm":
		return "深度沟通，挖掘痛点"
	case "hot":
		return "立即跟进，发送报价单"
	case "urgent":
		return "限时优惠+立即逼单"
	}
	return "持续跟进"
}

func (s *aiAgentsSeeder) buildFeedbackEvents(ctx *SeedContext) []model.FeedbackEvent {
	if len(ctx.SessionIDs) == 0 || len(ctx.CustomerIDs) == 0 {
		return nil
	}
	events := make([]model.FeedbackEvent, 0, 30)
	signalKeys := []string{
		model.FeedbackSignalLike, model.FeedbackSignalDislike,
		model.FeedbackSignalRating, model.FeedbackSignalComplaint,
		model.FeedbackSignalConversion, model.FeedbackSignalReplyRate,
		model.FeedbackSignalTransfer, model.FeedbackSignalChampionMark,
	}
	eventTypes := []string{
		model.FeedbackEventTypeExplicit, model.FeedbackEventTypeImplicit, model.FeedbackEventTypeChampion,
	}
	for i := 0; i < 30; i++ {
		key := signalKeys[i%len(signalKeys)]
		et := eventTypes[i%len(eventTypes)]
		weight := 1.0
		if et == model.FeedbackEventTypeChampion {
			weight = 3.0
		}
		reward := weight * randFloat(0.1, 1.0)
		aiReply := fmt.Sprintf("智能体回复内容 #%d（基于上下文生成）", i+1)
		custMsg := fmt.Sprintf("客户消息 #%d", i+1)
		ev := model.FeedbackEvent{
			EventID:     fmt.Sprintf("seed-evt-%d-%d", time.Now().Unix(), i+1),
			SessionID:   ctx.SessionIDs[i%len(ctx.SessionIDs)],
			CustomerID:  ctx.CustomerIDs[i%len(ctx.CustomerIDs)],
			EventType:   et,
			SignalKey:   key,
			SignalValue: model.JSONMap{"value": randInt(1, 5), "note": seedTag},
			Weight:      weight,
			Reward:      reward,
			AIReply:     aiReply,
			CustomerMsg: custMsg,
			Metadata:    model.JSONMap{"seed": seedTag, "source": "demo"},
			CreatedBy:   0,
		}
		events = append(events, ev)
	}
	return events
}

func (s *aiAgentsSeeder) buildFeedbackSignals(ctx *SeedContext) []model.FeedbackSignal {
	if len(ctx.SessionIDs) == 0 || len(ctx.CustomerIDs) == 0 {
		return nil
	}
	signals := make([]model.FeedbackSignal, 0, 15)
	outcomes := []string{model.FeedbackSignalOutcomeSuccess, model.FeedbackSignalOutcomeFail, model.FeedbackSignalOutcomePending}
	for i := 0; i < 15; i++ {
		outcome := outcomes[i%len(outcomes)]
		reward := randFloat(-0.5, 1.5)
		if outcome == model.FeedbackSignalOutcomeSuccess {
			reward = randFloat(0.5, 1.5)
		} else if outcome == model.FeedbackSignalOutcomeFail {
			reward = randFloat(-0.5, 0.2)
		}
		startTime := daysAgo(randInt(0, 25))
		endTime := startTime.Add(time.Duration(randInt(5, 120)) * time.Minute)
		sig := model.FeedbackSignal{
			SessionID:        ctx.SessionIDs[i%len(ctx.SessionIDs)],
			CustomerID:       ctx.CustomerIDs[i%len(ctx.CustomerIDs)],
			AggregatedReward: reward,
			SignalCount:      randInt(1, 8),
			SignalBreakdown:  model.JSONMap{"like": randInt(0, 3), "dislike": randInt(0, 2), "conversion": randInt(0, 1)},
			Outcome:          "seed-" + outcome,
			IsChampion:       i%5 == 0,
			SessionStartedAt: &startTime,
			SessionEndedAt:   &endTime,
		}
		signals = append(signals, sig)
	}
	return signals
}

func (s *aiAgentsSeeder) buildPromptCandidates() []model.PromptCandidate {
	cands := []struct {
		Scenario string
		Version  string
		Title    string
		Status   string
	}{
		{"llm_system", "1.0", "HiveMTK 客服系统提示词 v1.0", model.PromptCandidateStatusRetired},
		{"llm_system", "1.1", "HiveMTK 客服系统提示词 v1.1", model.PromptCandidateStatusPromoted},
		{"llm_system", "2.0", "HiveMTK 客服系统提示词 v2.0", model.PromptCandidateStatusActive},
		{"sop_reply", "1.0", "SOP回复话术 v1.0", model.PromptCandidateStatusApproved},
		{"objection", "1.0", "异议处理话术 v1.0", model.PromptCandidateStatusPending},
		{"closing", "1.0", "引导加群话术 v1.0", model.PromptCandidateStatusDraft},
	}
	result := make([]model.PromptCandidate, 0, len(cands))
	for i, c := range cands {
		alpha := 2.0
		beta := 2.0
		samples := 0
		success := 0
		avgReward := 0.0
		if c.Status == model.PromptCandidateStatusActive || c.Status == model.PromptCandidateStatusPromoted {
			samples = randInt(50, 500)
			success = randInt(25, 350)
			avgReward = randFloat(0.3, 0.85)
			alpha = 2 + float64(success)
			beta = 2 + float64(samples-success)
		}
		var promotedAt *time.Time
		if c.Status == model.PromptCandidateStatusPromoted {
			t := daysAgo(randInt(5, 20))
			promotedAt = &t
		}
		pc := model.PromptCandidate{
			SOPNodeID:          fmt.Sprintf("node-%d", i+1),
			SOPID:              uint(i + 1),
			Scenario:           c.Scenario,
			Version:            c.Version,
			Title:              c.Title + " " + seedTag,
			SystemPrompt:       "你是 HiveMTK 官方客服助手，遵循需求澄清→事实回答→引导文档/加群的应答法，先理解意图再给准确方案。",
			UserPromptTemplate: "用户说：{{customer_msg}}\n请基于上下文生成专业回复。",
			Variables:          model.JSONMap{"customer_msg": "string", "topic": "string"},
			ParentID: func() uint {
				if i > 0 {
					return uint(i)
				}
				return 0
			}(),
			ImprovementNotes: "强化需求澄清+事实准确+引导文档/加群",
			Status:           c.Status,
			Alpha:            alpha,
			Beta:             beta,
			SampleCount:      samples,
			SuccessCount:     success,
			AvgReward:        avgReward,
			PromotedAt:       promotedAt,
			GeneratedBy:      "auto",
		}
		result = append(result, pc)
	}
	return result
}

func (s *aiAgentsSeeder) buildChampionDialogues(ctx *SeedContext) []model.ChampionDialogue {
	if len(ctx.CustomerIDs) == 0 {
		return nil
	}
	scenarios := []struct {
		Scenario string
		Stage    string
		CustMsg  string
		Reply    string
	}{
		{
			model.ChampionScenarioObjection, model.ChampionStageConsider,
			"价格比竞品高 30%，我觉得不划算",
			"完全理解您的顾虑。我们的价格确实略高，但核心差异在于：1) 提供专属客户经理 7×24 服务；2) 每月免费迭代升级；3) 数据安全保障。算下来每月多花 X 元，但节省 80% 的人力成本。",
		},
		{
			model.ChampionScenarioClosing, model.ChampionStageDecide,
			"再和家人商量下",
			"完全理解。为您准备一份详细的对比资料，您可以和家人一起看。另外，本周有专属家庭套餐优惠，今天锁定可以额外赠送 6 个月服务期。",
		},
		{
			model.ChampionScenarioFollowup, model.ChampionStageContact,
			"最近比较忙，没时间看",
			"理解您忙。我用 1 分钟给您同步关键信息：1) 您关注的 X 功能已上线；2) 本月有老客专享 7 折；3) 限前 50 名。需要我发详细资料吗？",
		},
		{
			model.ChampionScenarioNurture, model.ChampionStageLead,
			"你们的产品有什么不一样？",
			"好问题。和市面同类产品相比，我们有 3 个独特之处：1) AI 智能体深度集成；2) 多渠道统一管理；3) 数据驱动决策。您最关注哪个方面？我可以深入介绍。",
		},
		{
			model.ChampionScenarioRepurchase, model.ChampionStageRetain,
			"用了一年，效果还行",
			"感谢认可！为回馈老客户，我们推出：1) 升级版功能免费体验 3 个月；2) 推荐好友双方各得 500 元；3) 老客专属服务通道。需要我帮您开通吗？",
		},
	}
	dialogues := make([]model.ChampionDialogue, 0, 10)
	for i := 0; i < 10; i++ {
		sc := scenarios[i%len(scenarios)]
		staffID := uint(0)
		if len(ctx.CSUserIDs) > 0 {
			staffID = ctx.CSUserIDs[i%len(ctx.CSUserIDs)]
		}
		dlg := model.ChampionDialogue{
			DialogueFingerprint: fmt.Sprintf("seed-dlg-fp-%d-%d", time.Now().Unix(), i+1),
			SessionID:           fmt.Sprintf("seed-champion-sess-%d", i+1),
			CustomerID:          ctx.CustomerIDs[i%len(ctx.CustomerIDs)],
			StaffID:             staffID,
			StaffName:           fmt.Sprintf("销冠顾问%d %s", i+1, seedTag),
			Scenario:            sc.Scenario,
			JourneyStage:        sc.Stage,
			CustomerMsg:         sc.CustMsg,
			ChampionReply:       sc.Reply,
			ContextMsgs:         model.JSONMap{"prev_count": randInt(2, 5)},
			Embedding:           zeroVectorString(1024), 
			ClusterID:           uint(i%3 + 1),
			Reward:              randFloat(0.5, 0.95),
			ConversionAchieved:  i%3 == 0,
			ExtractedScripts:    model.JSONMap{"title": fmt.Sprintf("销冠话术#%d", i+1), "content": sc.Reply},
		}
		t := daysAgo(randInt(1, 20))
		dlg.ExtractedAt = &t
		dialogues = append(dialogues, dlg)
	}
	return dialogues
}

func (s *aiAgentsSeeder) buildAISalesLogs(ctx *SeedContext) []model.AISalesLog {
	if len(ctx.SessionIDs) == 0 {
		return nil
	}
	scenarios := []string{"objection", "closing", "followup", "nurture", "repurchase", "greeting", "discovery", "presentation"}
	logs := make([]model.AISalesLog, 0, 30)
	for i := 0; i < 30; i++ {
		customerID := ""
		if len(ctx.CustomerIDs) > 0 {
			customerID = ctx.CustomerIDs[i%len(ctx.CustomerIDs)]
		}
		promptTokens := randInt(100, 2000)
		completionTokens := randInt(50, 1000)
		success := i%10 != 0 
		var errMsg string
		if !success {
			errMsg = "LLM 超时，触发降级"
		}
		logRec := model.AISalesLog{
			SessionID:        ctx.SessionIDs[i%len(ctx.SessionIDs)],
			CustomerID:       customerID,
			SOPID:            uint(i%5 + 1),
			LLMModel:         "default",
			Scenario:         scenarios[i%len(scenarios)],
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      promptTokens + completionTokens,
			Cost:             randFloat(0.001, 0.05),
			LatencyMs:        randInt(200, 3000),
			Success:          success,
			ErrorMessage:     errMsg,
			Extra:            model.JSONMap{"seed": seedTag, "trace_id": fmt.Sprintf("seed-trace-%d", i+1)},
		}
		logs = append(logs, logRec)
	}
	return logs
}


