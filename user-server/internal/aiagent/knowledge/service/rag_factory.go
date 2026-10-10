package service

import (
	ragcustomerservice "hivemtk-user/internal/aiagent/rag/customer_service"
	ragretrieval "hivemtk-user/internal/aiagent/rag/retrieval"
)

type RAGStack struct {
	Retrieval ragretrieval.RagRetrievalService
	Customer  ragcustomerservice.RagCustomerService
}
