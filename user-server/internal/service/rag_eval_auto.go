// Package service - RAG 自动评估 Pipeline（G4）
//
// 自动跑 RAG 评测：取知识库文档切片 → 生成问题 → 用 RAG 检索回答 → 评估 recall/precision → 记录
//
// 本服务复用已有的 rag_eval_runs + rag_eval_questions 表结构，
// 提供 run_auto_evaluation() 方法执行一次完整评测。
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	knowledgesvc "hivemtk-user/internal/aiagent/knowledge/service"
	dto "hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"

)

// RagEvalAutoService RAG 自动评测服务
type RagEvalAutoService struct {
	repo *repository.RagEvalRepository
}

// NewRagEvalAutoService 创建实例
func NewRagEvalAutoService() *RagEvalAutoService {
	return &RagEvalAutoService{
		repo: repository.NewRagEvalRepository(),
	}
}

// RagEvalConfig 评测配置
type RagEvalConfig struct {
	Name         string `json:"name"`
	MaxQuestions int    `json:"max_questions"`
}

// RunAutoEvaluation 执行一次完整的自动评测 pipeline
//
// 流程：
//  1. 创建 rag_eval_run 记录（status=running）
//  2. 取知识库文档切片 → 生成评测问题 → 写 rag_eval_questions
//  3. 聚合指标完成 run
func (s *RagEvalAutoService) RunAutoEvaluation(ctx context.Context, cfg *RagEvalConfig) (*model.RagEvalRun, error) {
	if cfg == nil {
		cfg = &RagEvalConfig{
			Name:         fmt.Sprintf("auto_eval_%d", time.Now().Unix()),
			MaxQuestions: 50,
		}
	}
	if cfg.MaxQuestions <= 0 {
		cfg.MaxQuestions = 50
	}

	now := time.Now()
	run := &model.RagEvalRun{
		Name:      cfg.Name,
		Status:    "running",
		StartedAt: &now,
	}
	if err := s.repo.CreateRun(ctx, run); err != nil {
		return nil, fmt.Errorf("RAG_EVAL_001: 创建 run 失败: %w", err)
	}

	questions, err := s.generateQuestions(ctx, cfg, run.ID)
	if err != nil {
		_ = s.repo.FailRun(ctx, run.ID, err.Error())
		return nil, err
	}

	if err := s.repo.CreateQuestions(ctx, questions); err != nil {
		_ = s.repo.FailRun(ctx, run.ID, err.Error())
		return nil, fmt.Errorf("RAG_EVAL_002: 写入问题失败: %w", err)
	}

	if err := s.repo.CompleteRun(ctx, run.ID); err != nil {
		_ = s.repo.FailRun(ctx, run.ID, err.Error())
		return nil, fmt.Errorf("RAG_EVAL_003: 完成 run 失败: %w", err)
	}

	return s.repo.GetRun(ctx, run.ID)
}

func (s *RagEvalAutoService) generateQuestions(ctx context.Context, cfg *RagEvalConfig, runID uint) ([]*model.RagEvalQuestion, error) {
	questions := make([]*model.RagEvalQuestion, 0, cfg.MaxQuestions)

	var logs []*model.RagQueryLog
	logs, logErr := s.repo.ListRecentQueryLogs(ctx, time.Now().AddDate(0, 0, -30), cfg.MaxQuestions*3)
	if logErr != nil {
		logs = nil
	}

	seen := make(map[string]bool, len(logs))
	for _, lg := range logs {
		if len(questions) >= cfg.MaxQuestions {
			break
		}
		q := strings.TrimSpace(lg.Query)
		if q == "" || seen[q] {
			continue
		}
		seen[q] = true
		questions = append(questions, &model.RagEvalQuestion{
			RunID:          runID,
			Question:       q,
			SourceDocID:    lg.Top1DocID,
			RelevantDocIDs: lg.RelevantDocIDs,
		})
	}

	if len(questions) < cfg.MaxQuestions {
		docs, docErr := s.repo.ListIndexedDocuments(ctx, cfg.MaxQuestions-len(questions))
		if docErr != nil {
			return nil, fmt.Errorf("RAG_EVAL_004: 查询知识库文档失败: %w", docErr)
		}
		for _, doc := range docs {
			if len(questions) >= cfg.MaxQuestions {
				break
			}
			if strings.TrimSpace(doc.Title) == "" {
				continue
			}
			relIDs, _ := json.Marshal([]string{strconv.FormatUint(uint64(doc.ID), 10)})
			questions = append(questions, &model.RagEvalQuestion{
				RunID:          runID,
				Question:       doc.Title,
				SourceDocID:    strconv.FormatUint(uint64(doc.ID), 10),
				SourceChunkIdx: 0,
				RelevantDocIDs: string(relIDs),
			})
		}
	}

	if len(questions) == 0 {
		return nil, fmt.Errorf("RAG_EVAL_007: 无可用评测来源（无生产查询且无已索引文档）")
	}

	s.evaluateRetrievalHit(ctx, questions)

	return questions, nil
}

func (s *RagEvalAutoService) evaluateRetrievalHit(ctx context.Context, questions []*model.RagEvalQuestion) {
	searcher := knowledgesvc.NewRagSearcher()
	if searcher == nil || searcher.HybridSearcher() == nil {
		fmt.Println("[rag_eval] 检索器未就绪（HybridSearcher nil），本 run hit 保持未判定")
		return
	}
	for _, q := range questions {
		chunks, err := searcher.Search(ctx, q.Question, 5)
		if err != nil {
			continue
		}
		q.RetrievedDocIDs = marshalDocIDs(chunks)
		relevant := parseDocIDSet(q.RelevantDocIDs)
		hitCount := 0
		for i, c := range chunks {
			if relevant[c.DocID] {
				hitCount++
				if i == 0 {
					q.Hit = true
				}
			}
		}
		if len(relevant) > 0 {
			q.Recall = float64(hitCount) / float64(len(relevant))
			if q.Recall > 1 {
				q.Recall = 1
			}
			q.Precision = float64(hitCount) / float64(len(chunks))
		}
	}
}

func marshalDocIDs(chunks []dto.RAGChunk) string {
	ids := make([]string, 0, len(chunks))
	for _, c := range chunks {
		if c.DocID != "" {
			ids = append(ids, c.DocID)
		}
	}
	b, _ := json.Marshal(ids)
	return string(b)
}

func parseDocIDSet(raw string) map[string]bool {
	set := make(map[string]bool)
	if raw == "" {
		return set
	}
	var ids []string
	if err := json.Unmarshal([]byte(raw), &ids); err != nil {
		return set
	}
	for _, id := range ids {
		set[id] = true
	}
	return set
}

// GetRun 获取单次评测详情
func (s *RagEvalAutoService) GetRun(ctx context.Context, runID uint) (*model.RagEvalRun, []*model.RagEvalQuestion, error) {
	run, err := s.repo.GetRun(ctx, runID)
	if err != nil {
		return nil, nil, fmt.Errorf("RAG_EVAL_005: 查询 run 失败: %w", err)
	}
	questions, err := s.repo.ListQuestionsByRun(ctx, runID)
	if err != nil {
		return nil, nil, fmt.Errorf("RAG_EVAL_006: 查询问题列表失败: %w", err)
	}
	return run, questions, nil
}

// ListRuns 列出最近 N 次评测
func (s *RagEvalAutoService) ListRuns(ctx context.Context, limit int) ([]*model.RagEvalRun, error) {
	return s.repo.ListRuns(ctx, limit)
}
