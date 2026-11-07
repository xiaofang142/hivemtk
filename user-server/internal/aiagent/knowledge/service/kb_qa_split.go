// Package service T4：QA 自动拆分（§5）。
//
// 爬取管道对每篇 indexed 成功且开启 qa_split 的文档投一个异步任务：
// LLM 按 prompt 提取 3~5 个 QA 对（答案锚定原文防编造），落 qa_candidates
// （status=pending_review），进与 T1 共用的审核队列。解析失败重试 1 次（温度降 0），
// 再失败记 failed 不产生候选行——宁可少产出不产生脏候选。
//
// LLM client 走注入接口（LLMClient），生产装配接模型网关；测试用 fake。
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"hivemtk-user/internal/aiagent/knowledge/kbcrawler"
	internalmodel "hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/async"
	"hivemtk-user/internal/pkg/utils/logger"

	"gorm.io/gorm"
)

// LLMClient QA 拆分的模型调用接口（生产注入模型网关，测试注入 fake）
type LLMClient interface {
	// Complete 单轮补全：prompt → 文本输出
	Complete(ctx context.Context, prompt string) (string, error)
}

// QASplitService QA 拆分服务
type QASplitService struct {
	db  *gorm.DB
	llm LLMClient

	mu      sync.Mutex
	pending []qaTask // 简单内存队列：一篇文档一个任务（§5.1，不建任务表）

	// MaxDocTokens 单文档超长分批阈值（§5.1：>8000 token 按 chunk 分批）
	MaxDocTokens int
	// MaxQAPerBatch 每批最多产出 QA 对数
	MaxQAPerBatch int
}

// NewQASplitService 构造
func NewQASplitService(db *gorm.DB, llm LLMClient) *QASplitService {
	return &QASplitService{db: db, llm: llm, MaxDocTokens: 8000, MaxQAPerBatch: 5}
}

type qaTask struct {
	DocID uint
	KBID  uint
}

// EnqueueDocument 投一篇文档进拆分队列（异步执行）
func (s *QASplitService) EnqueueDocument(docID, kbID uint) {
	s.mu.Lock()
	s.pending = append(s.pending, qaTask{DocID: docID, KBID: kbID})
	s.mu.Unlock()
	async.RunWithTimeout(context.Background(), 5*time.Minute, func(ctx context.Context) {
		s.drain(ctx)
	})
}

// drain 消费队列
func (s *QASplitService) drain(ctx context.Context) {
	for {
		s.mu.Lock()
		if len(s.pending) == 0 {
			s.mu.Unlock()
			return
		}
		task := s.pending[0]
		s.pending = s.pending[1:]
		s.mu.Unlock()
		if err := s.ProcessDocument(ctx, task.DocID, task.KBID); err != nil {
			logger.Warnf("[QASplit] 文档 %d 拆分失败: %v", task.DocID, err)
		}
	}
}

// qaPrompt 拆分 prompt（§5.2：多样化问法 + 答案锚定原文 + 不编造）
const qaPromptTpl = `你是一名知识库编辑。下面是来自企业官网的一段内容。
请从中提取 3~5 个"客户最可能这样问"的问题与对应答案：
1. 问题要多样化：同一段内容若有多个独立知识点，每个知识点单独一问；不要改写原文没说的东西。
2. 答案必须锚定原文：只允许改写措辞，不允许新增事实；answer_anchor 字段抄录答案所依据的原文片段。
3. 输出严格为如下 JSON 数组，不要输出任何其他文字：
[{"question":"...","answer":"...","answer_anchor":"..."}]
若内容不含任何可提取的问答信息（如纯导航页），输出 []。

内容如下：
%s`

// qaItem LLM 输出的单个 QA 对
type qaItem struct {
	Question     string `json:"question"`
	Answer       string `json:"answer"`
	AnswerAnchor string `json:"answer_anchor"`
}

// ProcessDocument 对一篇文档执行 QA 拆分：读 kb_documents.content → LLM → 落候选行。
// taskID = qa_<docID>_<unixnano>（一篇文档一个任务，task_id 聚合判断完成度）。
func (s *QASplitService) ProcessDocument(ctx context.Context, docID, kbID uint) error {
	var doc internalmodel.KBDocument
	if err := s.db.WithContext(ctx).First(&doc, docID).Error; err != nil {
		return fmt.Errorf("读文档失败: %w", err)
	}
	if doc.Content == "" {
		return fmt.Errorf("文档 %d 无内容", docID)
	}
	// 文档超长按 chunk 边界分批（简化：按 token 估算切片，每批 ≤ MaxQAPerBatch 由 prompt 限）
	content := doc.Content
	if estimateTokens(content) > s.MaxDocTokens {
		content = content[:len(content)*s.MaxDocTokens/estimateTokens(content)] // 截断到阈值内
	}

	taskID := fmt.Sprintf("qa_%d_%d", docID, time.Now().UnixNano())

	out, err := s.llm.Complete(ctx, fmt.Sprintf(qaPromptTpl, content))
	if err != nil {
		// 重试 1 次（温度降 0 由 client 实现层负责；此处仅重试）
		out, err = s.llm.Complete(ctx, fmt.Sprintf(qaPromptTpl, content))
		if err != nil {
			return fmt.Errorf("LLM 调用失败（重试后）: %w", err)
		}
	}

	items, err := parseQAOutput(out)
	if err != nil {
		// 解析失败重试 1 次，再失败记 failed 不产生候选行（宁缺毋滥）
		out, retryErr := s.llm.Complete(ctx, fmt.Sprintf(qaPromptTpl, content))
		if retryErr != nil {
			return fmt.Errorf("LLM 重试失败: %w", retryErr)
		}
		items, err = parseQAOutput(out)
		if err != nil {
			return fmt.Errorf("LLM 输出解析失败（重试后）: %w", err)
		}
	}
	if len(items) == 0 {
		return nil // 纯导航页：正常无产出
	}

	rows := make([]*internalmodel.QACandidate, 0, len(items))
	// 显式写时间：审核队列按 created_at 展示"候选产生时间"，历史上存在绕过 GORM 时序钩子
	// 写入的行（created_at 为 NULL）导致前端整列显示 "-"，这里不再依赖 autoCreateTime。
	now := time.Now()
	for _, it := range items {
		if strings.TrimSpace(it.Question) == "" || strings.TrimSpace(it.Answer) == "" {
			continue
		}
		rows = append(rows, &internalmodel.QACandidate{
			TenantID:     doc.TenantID,
			SourceType:   internalmodel.QACandidateSourceTypeCrawl,
			SourceRef:    fmt.Sprintf("doc:%d", docID),
			KBID:         kbID,
			Question:     it.Question,
			Answer:       it.Answer,
			AnswerAnchor: it.AnswerAnchor,
			Status:       internalmodel.QACandidateStatusPendingReview,
			TaskID:       taskID,
			CreatedAt:    now,
			UpdatedAt:    now,
		})
	}
	if len(rows) == 0 {
		return nil
	}
	return s.db.WithContext(ctx).Create(&rows).Error
}

// parseQAOutput 解析 LLM 输出 JSON 数组（容忍前后多余文字）
func parseQAOutput(out string) ([]qaItem, error) {
	out = strings.TrimSpace(out)
	start := strings.Index(out, "[")
	end := strings.LastIndex(out, "]")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("输出中无 JSON 数组")
	}
	var items []qaItem
	if err := json.Unmarshal([]byte(out[start:end+1]), &items); err != nil {
		return nil, fmt.Errorf("JSON 解析失败: %w", err)
	}
	return items, nil
}

// estimateTokens 粗估 token 数（中文 ≈ 1.5 字/token，英文 ≈ 4 字符/token，取保守值）
func estimateTokens(s string) int {
	runes := len([]rune(s))
	if runes == 0 {
		return 0
	}
	return runes * 2 / 3
}

// —— QA 候选审核（与 T1 共用状态机；approve → publish → indexed）——

// ListQACandidates 候选列表（source_type/status 过滤，T1 前端复用同一接口语义）
func (s *QASplitService) ListQACandidates(ctx context.Context, sourceType, status string, page, pageSize int) ([]internalmodel.QACandidate, int64, error) {
	q := s.db.WithContext(ctx).Model(&internalmodel.QACandidate{})
	if sourceType != "" {
		q = q.Where("source_type = ?", sourceType)
	}
	if status != "" {
		q = q.Where("status = ?", status)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var out []internalmodel.QACandidate
	err := q.Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&out).Error
	return out, total, err
}

// ReviewQACandidate 裁决候选（approve/reject；走 QACandidateTransitionAllowed）
func (s *QASplitService) ReviewQACandidate(ctx context.Context, id uint64, to, reviewer string) error {
	var c internalmodel.QACandidate
	if err := s.db.WithContext(ctx).First(&c, id).Error; err != nil {
		// D-25：not-found 语义必须保留，controller 依赖 errhttp.IsNotFoundError 映射 404
		return gorm.ErrRecordNotFound
	}
	if !internalmodel.QACandidateTransitionAllowed(c.Status, to) {
		return fmt.Errorf("非法状态跃迁: %s → %s", c.Status, to)
	}
	return s.db.WithContext(ctx).Model(&c).Updates(map[string]any{"status": to, "reviewer": reviewer}).Error
}

// PublishQACandidate approved → indexed：QA 对转成知识库文档（answer 为正文、
// question 进 metadata 语义上由 title 承载），走 ProcessDocumentByID 同链路。
func (s *QASplitService) PublishQACandidate(ctx context.Context, id uint64, kb *KnowledgeBaseService) error {
	var c internalmodel.QACandidate
	if err := s.db.WithContext(ctx).First(&c, id).Error; err != nil {
		// D-25：not-found 语义必须保留，controller 依赖 errhttp.IsNotFoundError 映射 404
		return gorm.ErrRecordNotFound
	}
	if !internalmodel.QACandidateTransitionAllowed(c.Status, internalmodel.QACandidateStatusIndexed) {
		return fmt.Errorf("非法状态跃迁: %s → indexed（需先 approve）", c.Status)
	}
	doc := &internalmodel.KBDocument{
		TenantID: c.TenantID,
		Title:    c.Question,
		Content:  c.Answer,
		FileType: ".qa",
		FileSize: int64(len(c.Answer)),
		Status:   internalmodel.KBDocumentStatusPending,
	}
	if err := s.db.WithContext(ctx).Create(doc).Error; err != nil {
		return fmt.Errorf("落知识库文档失败: %w", err)
	}
	if err := s.db.WithContext(ctx).Model(&c).Update("status", internalmodel.QACandidateStatusIndexed).Error; err != nil {
		return err
	}
	go kb.ProcessDocumentByID(context.Background(), doc.ID)
	return nil
}

// ensure kbcrawler import kept for ErrEmptyPage doc reference linkage
var _ = kbcrawler.ErrEmptyPage
