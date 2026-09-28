package model

import (
	"testing"
)

func TestRagProduct_TableName(t *testing.T) {
	product := &RagProduct{}
	tableName := product.TableName()
	if tableName != "rag_products" {
		t.Errorf("Expected table name 'rag_products', got %s", tableName)
	}
}

func TestRagProduct_BasicFields(t *testing.T) {
	product := &RagProduct{
		ID:               "rag-123",
		Name:             "Test Product",
		Description:      "Test description",
		Category:         "customer_service",
		LLMModel:         "gpt-4",
		Temperature:      0.7,
		MaxTokens:        1000,
		TopP:             0.9,
		FrequencyPenalty: 0.5,
		PresencePenalty:  0.5,
		ResponseFormat:   "json_object",
		SystemPrompt:     "You are a helpful assistant",
		IsActive:         true,
	}

	if product.ID != "rag-123" {
		t.Errorf("Expected ID 'rag-123', got %s", product.ID)
	}
	if product.Name != "Test Product" {
		t.Errorf("Expected Name 'Test Product', got %s", product.Name)
	}
	if product.Description != "Test description" {
		t.Errorf("Expected Description 'Test description', got %s", product.Description)
	}
	if product.Category != "customer_service" {
		t.Errorf("Expected Category 'customer_service', got %s", product.Category)
	}
	if product.LLMModel != "gpt-4" {
		t.Errorf("Expected LLMModel 'gpt-4', got %s", product.LLMModel)
	}
	if product.Temperature != 0.7 {
		t.Errorf("Expected Temperature 0.7, got %f", product.Temperature)
	}
	if product.MaxTokens != 1000 {
		t.Errorf("Expected MaxTokens 1000, got %d", product.MaxTokens)
	}
	if product.TopP != 0.9 {
		t.Errorf("Expected TopP 0.9, got %f", product.TopP)
	}
	if product.FrequencyPenalty != 0.5 {
		t.Errorf("Expected FrequencyPenalty 0.5, got %f", product.FrequencyPenalty)
	}
	if product.PresencePenalty != 0.5 {
		t.Errorf("Expected PresencePenalty 0.5, got %f", product.PresencePenalty)
	}
	if product.ResponseFormat != "json_object" {
		t.Errorf("Expected ResponseFormat 'json_object', got %s", product.ResponseFormat)
	}
	if product.SystemPrompt != "You are a helpful assistant" {
		t.Errorf("Expected SystemPrompt, got %s", product.SystemPrompt)
	}
	if !product.IsActive {
		t.Error("Expected IsActive to be true")
	}
}

func TestRagProduct_DefaultValues(t *testing.T) {
	product := &RagProduct{}

	if product.LLMModel != "" {
		t.Logf("LLMModel is %q (建列默认由 config.DefaultLLMModel 同值守卫，见 inference_profile_parity_test.go)", product.LLMModel)
	}
	if product.Temperature != 0 {
		t.Logf("Temperature is %f (expected 0 before save, default is 0.7)", product.Temperature)
	}
	if product.MaxTokens != 0 {
		t.Logf("MaxTokens is %d (expected 0 before save, default is 1000)", product.MaxTokens)
	}
	if product.TopP != 0 {
		t.Logf("TopP is %f (expected 0 before save, default is 0.9)", product.TopP)
	}
	if product.IsActive != false {
		t.Logf("IsActive is %v (expected false before save, default is true)", product.IsActive)
	}
}

func TestRagProduct_WithLLMProviderConfig(t *testing.T) {
	product := &RagProduct{
		ID: "rag-456",
		LLMProviderConfig: LLMProviderConfig{
			APIKey:         "sk-test123",
			BaseURL:        "https://api.openai.com/v1",
			APIType:        "openai",
			Model:          "gpt-4",
			MaxRetries:     3,
			RequestTimeout: 60,
		},
	}

	if product.LLMProviderConfig.APIKey != "sk-test123" {
		t.Errorf("Expected APIKey 'sk-test123', got %s", product.LLMProviderConfig.APIKey)
	}
	if product.LLMProviderConfig.BaseURL != "https://api.openai.com/v1" {
		t.Errorf("Expected BaseURL 'https://api.openai.com/v1', got %s", product.LLMProviderConfig.BaseURL)
	}
	if product.LLMProviderConfig.APIType != "openai" {
		t.Errorf("Expected APIType 'openai', got %s", product.LLMProviderConfig.APIType)
	}
	if product.LLMProviderConfig.Model != "gpt-4" {
		t.Errorf("Expected Model 'gpt-4', got %s", product.LLMProviderConfig.Model)
	}
	if product.LLMProviderConfig.MaxRetries != 3 {
		t.Errorf("Expected MaxRetries 3, got %d", product.LLMProviderConfig.MaxRetries)
	}
	if product.LLMProviderConfig.RequestTimeout != 60 {
		t.Errorf("Expected RequestTimeout 60, got %d", product.LLMProviderConfig.RequestTimeout)
	}
}

func TestRagProduct_WithEmptyID(t *testing.T) {
	product := &RagProduct{
		Name: "Test Product",
		ID:   "",
	}

	if product.ID != "" {
		t.Errorf("Expected empty ID, got %s", product.ID)
	}
}

func TestRagProduct_WithDisabled(t *testing.T) {
	product := &RagProduct{
		Name:     "Disabled Product",
		IsActive: false,
	}

	if product.IsActive {
		t.Error("Expected IsActive to be false")
	}
}

func TestRagProduct_WithResponseFormats(t *testing.T) {
	formats := []string{"json_object", "text"}

	for _, format := range formats {
		product := &RagProduct{
			ResponseFormat: format,
		}
		if product.ResponseFormat != format {
			t.Errorf("Expected ResponseFormat %s, got %s", format, product.ResponseFormat)
		}
	}
}

func TestLLMProviderConfig_DefaultValues(t *testing.T) {
	config := &LLMProviderConfig{}

	if config.MaxRetries != 0 {
		t.Logf("MaxRetries is %d (expected 0 before save, default is 3)", config.MaxRetries)
	}
	if config.RequestTimeout != 0 {
		t.Logf("RequestTimeout is %d (expected 0 before save, default is 60)", config.RequestTimeout)
	}
}

func TestLLMProviderConfig_WithAPIType(t *testing.T) {
	apiTypes := []string{"openai", "anthropic", "custom", "azure"}

	for _, apiType := range apiTypes {
		config := &LLMProviderConfig{
			APIType: apiType,
		}
		if config.APIType != apiType {
			t.Errorf("Expected APIType %s, got %s", apiType, config.APIType)
		}
	}
}
