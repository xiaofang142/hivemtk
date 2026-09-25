package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"hivemtk-user/internal/config"
	"hivemtk-user/internal/model"
)

// AIAgentCreateDTO 智能体创建/更新 DTO（脱离 model 的入参）
type AIAgentCreateDTO struct {
	AgentCode            string          `json:"agent_code"`
	Name                 string          `json:"name"`
	Description          string          `json:"description"`
	Avatar               string          `json:"avatar"`
	AgentType            string          `json:"agent_type"`
	Persona              string          `json:"persona"`
	SystemPrompt         string          `json:"system_prompt"`
	Greeting             string          `json:"greeting"`
	RagProductIDs        []string        `json:"rag_product_ids"`
	SOPIDs               []string        `json:"sop_ids"`
	ScriptLibraryIDs     []string        `json:"script_library_ids"`
	LLMModel             string          `json:"llm_model"`
	LLMProviderConfig    json.RawMessage `json:"llm_provider_config"`
	Temperature          float64         `json:"temperature"`
	MaxTokens            int             `json:"max_tokens"`
	TopP                 float64         `json:"top_p"`
	FrequencyPenalty     float64         `json:"frequency_penalty"`
	PresencePenalty      float64         `json:"presence_penalty"`
	EnableRAG            bool            `json:"enable_rag"`
	EnableScriptMatch    bool            `json:"enable_script_match"`
	EnableHumanizePolish bool            `json:"enable_humanize_polish"`
	EnableContentAudit   bool            `json:"enable_content_audit"`
	EnablePlaybook       bool            `json:"enable_playbook"`
	RAGTopK              int             `json:"rag_top_k"`
	ConfidenceThreshold  float64         `json:"confidence_threshold"`
	MaxAIConsecutive     int             `json:"max_ai_consecutive"`
	Status               int             `json:"status"`
}

// CreateAIAgent 创建智能体（组装 model 并应用默认值）
func (s *AIAgentService) CreateAIAgent(ctx context.Context, req *AIAgentCreateDTO) (*model.AIAgent, error) {
	systemPrompt := req.SystemPrompt
	greeting := req.Greeting
	if r := GetAssetResolver(); r != nil && req.Persona != "" && systemPrompt == "" {
		if p, err := r.LoadPersona(ctx, req.Persona); err == nil && p != nil {
			systemPrompt = p.SystemPrompt
			if greeting == "" && len(p.GreetingTemplates) > 0 {
				greeting = strings.Join(p.GreetingTemplates, "\n")
			}
		}
	}

	agent := &model.AIAgent{
		AgentCode:            req.AgentCode,
		Name:                 req.Name,
		Description:          req.Description,
		Avatar:               req.Avatar,
		AgentType:            req.AgentType,
		Persona:              req.Persona,
		SystemPrompt:         systemPrompt,
		Greeting:             greeting,
		RagProductIDs:        req.RagProductIDs,
		SOPIDs:               req.SOPIDs,
		ScriptLibraryIDs:     req.ScriptLibraryIDs,
		LLMModel:             req.LLMModel,
		Temperature:          req.Temperature,
		MaxTokens:            req.MaxTokens,
		TopP:                 req.TopP,
		FrequencyPenalty:     req.FrequencyPenalty,
		PresencePenalty:      req.PresencePenalty,
		EnableRAG:            req.EnableRAG,
		EnableScriptMatch:    req.EnableScriptMatch,
		EnableHumanizePolish: req.EnableHumanizePolish,
		EnableContentAudit:   req.EnableContentAudit,
		EnablePlaybook:       req.EnablePlaybook,
		RAGTopK:              req.RAGTopK,
		ConfidenceThreshold:  req.ConfidenceThreshold,
		MaxAIConsecutive:     req.MaxAIConsecutive,
		Status:               req.Status,
	}
	if len(req.LLMProviderConfig) > 0 {
		var cfg model.LLMProviderConfig
		if err := json.Unmarshal(req.LLMProviderConfig, &cfg); err == nil {
			agent.LLMProviderConfig = cfg
		}
	}

	if agent.AgentType == "" {
		agent.AgentType = string(model.AgentTypeSales)
	}
	if agent.Status == 0 {
		agent.Status = 1
	}
	if agent.LLMModel == "" {
		agent.LLMModel = config.DefaultLLMModel()
	}
	if agent.Temperature == 0 {
		agent.Temperature = 0.7
	}
	if agent.MaxTokens == 0 {
		agent.MaxTokens = 2048
	}
	if agent.RAGTopK == 0 {
		agent.RAGTopK = 3
	}
	if agent.ConfidenceThreshold == 0 {
		agent.ConfidenceThreshold = 0.5
	}
	if !agent.EnableRAG && !req.EnableRAG {
		agent.EnableRAG = true
	}
	if !agent.EnableScriptMatch && !req.EnableScriptMatch {
		agent.EnableScriptMatch = true
	}
	if !agent.EnableHumanizePolish && !req.EnableHumanizePolish {
		agent.EnableHumanizePolish = true
	}
	if !agent.EnablePlaybook && !req.EnablePlaybook {
		agent.EnablePlaybook = true
	}

	if err := s.Create(ctx, agent); err != nil {
		return nil, err
	}
	return agent, nil
}

// UpdateAIAgentFromJSON 按原始 JSON 的部分字段更新智能体，保留未提供字段原值
func (s *AIAgentService) UpdateAIAgentFromJSON(ctx context.Context, id uint, rawJSON []byte) (*model.AIAgent, error) {
	existing, err := s.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, errors.New("智能体不存在")
	}

	var reqMap map[string]any
	_ = json.Unmarshal(rawJSON, &reqMap)
	hasKey := func(k string) bool {
		_, ok := reqMap[k]
		return ok
	}

	var req AIAgentCreateDTO
	_ = json.Unmarshal(rawJSON, &req)

	if hasKey("agent_code") {
		existing.AgentCode = req.AgentCode
	}
	if hasKey("name") && req.Name != "" {
		existing.Name = req.Name
	}
	if hasKey("agent_type") && req.AgentType != "" {
		existing.AgentType = req.AgentType
	}
	if hasKey("persona") && req.Persona != "" {
		existing.Persona = req.Persona
	}
	if hasKey("description") {
		existing.Description = req.Description
	}
	if hasKey("avatar") {
		existing.Avatar = req.Avatar
	}
	if hasKey("system_prompt") {
		existing.SystemPrompt = req.SystemPrompt
	}
	if hasKey("greeting") {
		existing.Greeting = req.Greeting
	}
	if hasKey("rag_product_ids") {
		existing.RagProductIDs = req.RagProductIDs
	}
	if hasKey("sop_ids") {
		existing.SOPIDs = req.SOPIDs
	}
	if hasKey("script_library_ids") {
		existing.ScriptLibraryIDs = req.ScriptLibraryIDs
	}
	if hasKey("llm_model") && req.LLMModel != "" {
		existing.LLMModel = req.LLMModel
	}
	if hasKey("llm_provider_config") && len(req.LLMProviderConfig) > 0 {
		var cfg model.LLMProviderConfig
		if err := json.Unmarshal(req.LLMProviderConfig, &cfg); err == nil {
			existing.LLMProviderConfig = cfg
		}
	}
	if hasKey("temperature") {
		existing.Temperature = req.Temperature
	}
	if hasKey("max_tokens") && req.MaxTokens != 0 {
		existing.MaxTokens = req.MaxTokens
	}
	if hasKey("top_p") {
		existing.TopP = req.TopP
	}
	if hasKey("frequency_penalty") {
		existing.FrequencyPenalty = req.FrequencyPenalty
	}
	if hasKey("presence_penalty") {
		existing.PresencePenalty = req.PresencePenalty
	}
	if hasKey("enable_rag") {
		existing.EnableRAG = req.EnableRAG
	}
	if hasKey("enable_script_match") {
		existing.EnableScriptMatch = req.EnableScriptMatch
	}
	if hasKey("enable_humanize_polish") {
		existing.EnableHumanizePolish = req.EnableHumanizePolish
	}
	if hasKey("enable_content_audit") {
		existing.EnableContentAudit = req.EnableContentAudit
	}
	if hasKey("enable_playbook") {
		existing.EnablePlaybook = req.EnablePlaybook
	}
	if hasKey("rag_top_k") && req.RAGTopK != 0 {
		existing.RAGTopK = req.RAGTopK
	}
	if hasKey("confidence_threshold") {
		existing.ConfidenceThreshold = req.ConfidenceThreshold
	}
	if hasKey("max_ai_consecutive") && req.MaxAIConsecutive != 0 {
		existing.MaxAIConsecutive = req.MaxAIConsecutive
	}
	if hasKey("status") && req.Status != 0 {
		existing.Status = req.Status
	}

	if err := s.Update(ctx, existing); err != nil {
		return nil, err
	}
	return existing, nil
}

// ChannelBindingDTO 渠道绑定创建 DTO（脱离 model 的入参）
type ChannelBindingDTO struct {
	ChannelType string `json:"channel_type" binding:"required"`
	AccountID   string `json:"account_id" binding:"required"`
	AgentID     uint   `json:"agent_id" binding:"required"`
	IsPrimary   bool   `json:"is_primary"`
	Enabled     bool   `json:"enabled"`
}

// CreateChannelBinding 创建渠道绑定
func (s *ChannelAgentBindingService) CreateChannelBinding(ctx context.Context, req *ChannelBindingDTO) (*model.ChannelAgentBinding, error) {
	b := &model.ChannelAgentBinding{
		ChannelType: NormalizeChannelType(req.ChannelType),
		AccountID:   req.AccountID,
		AgentID:     req.AgentID,
		IsPrimary:   req.IsPrimary,
		Enabled:     true,
	}
	if !req.Enabled {
		b.Enabled = false
	}
	if err := s.Create(ctx, b); err != nil {
		return nil, err
	}
	return b, nil
}

// UpdateChannelBindingFromJSON 按原始 JSON 更新渠道绑定
func (s *ChannelAgentBindingService) UpdateChannelBindingFromJSON(ctx context.Context, id uint, rawJSON []byte) (*model.ChannelAgentBinding, error) {
	existing, err := s.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, errors.New("绑定不存在")
	}
	var req ChannelBindingDTO
	if err := json.Unmarshal(rawJSON, &req); err != nil {
		return nil, err
	}
	// PATCH 语义：未传的字段保留原值，防止漏字段把绑定意外停用
	var rawKeys map[string]json.RawMessage
	_ = json.Unmarshal(rawJSON, &rawKeys)
	existing.ChannelType = NormalizeChannelType(req.ChannelType)
	existing.AccountID = req.AccountID
	existing.AgentID = req.AgentID
	if _, ok := rawKeys["is_primary"]; ok {
		existing.IsPrimary = req.IsPrimary
	}
	if _, ok := rawKeys["enabled"]; ok {
		existing.Enabled = req.Enabled
	}
	if err := s.Update(ctx, existing); err != nil {
		return nil, err
	}
	return existing, nil
}

// CSAgentMountDTO 客服挂载创建 DTO（脱离 model 的入参）
type CSAgentMountDTO struct {
	AgentStatusID uint `json:"agent_status_id" binding:"required"`
	AIAgentID     uint `json:"ai_agent_id" binding:"required"`
	IsPrimary     bool `json:"is_primary"`
	Enabled       bool `json:"enabled"`
}

// CreateCSAgentMount 创建客服挂载
func (s *CustomerServiceAgentService) CreateCSAgentMount(ctx context.Context, req *CSAgentMountDTO) (*model.CustomerServiceAgent, error) {
	m := &model.CustomerServiceAgent{
		AgentStatusID: req.AgentStatusID,
		AIAgentID:     req.AIAgentID,
		IsPrimary:     req.IsPrimary,
		Enabled:       true,
	}
	if !req.Enabled {
		m.Enabled = false
	}
	if err := s.Create(ctx, m); err != nil {
		return nil, err
	}
	return m, nil
}

// UpdateCSAgentMountFromJSON 按原始 JSON 更新客服挂载
func (s *CustomerServiceAgentService) UpdateCSAgentMountFromJSON(ctx context.Context, id uint, rawJSON []byte) (*model.CustomerServiceAgent, error) {
	existing, err := s.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, errors.New("挂载不存在")
	}
	var req CSAgentMountDTO
	if err := json.Unmarshal(rawJSON, &req); err != nil {
		return nil, err
	}
	// PATCH 语义：未传的字段保留原值，防止漏字段把挂载意外停用
	var rawKeys map[string]json.RawMessage
	_ = json.Unmarshal(rawJSON, &rawKeys)
	existing.AgentStatusID = req.AgentStatusID
	existing.AIAgentID = req.AIAgentID
	if _, ok := rawKeys["is_primary"]; ok {
		existing.IsPrimary = req.IsPrimary
	}
	if _, ok := rawKeys["enabled"]; ok {
		existing.Enabled = req.Enabled
	}
	if err := s.Update(ctx, existing); err != nil {
		return nil, err
	}
	return existing, nil
}
