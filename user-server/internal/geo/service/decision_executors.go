package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"hivemtk-user/internal/geo/dto"
	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/geo/repository"
)

// LeadCapturePort 线索捕获端口（portcontract 模式：geo 不反向依赖业务层，
// 由装配层在 router 注入主域 clue/identity 实现）
type LeadCapturePort interface {
	CaptureLead(ctx context.Context, contact, contactType, sourceChainID, intent string) (clueID string, err error)
}

// DecisionChainDeps 决策链执行器依赖集
type DecisionChainDeps struct {
	Probe     SearchProbe
	ChainRepo repository.GeoQueryChainRepository
	TaskRepo  repository.GeoContentTaskRepository
	Brand     string
	LeadPort  LeadCapturePort
}

// RegisterDecisionChainExecutors 向工作流注册四个决策链步骤类型。
// 全部遵守 _step_results 上游产物传递契约（红队 F2 修正）。
func (s *WorkflowService) RegisterDecisionChainExecutors(deps DecisionChainDeps) {
	if deps.Probe == nil {
		deps.Probe = NewDefaultSearchProbe()
	}
	if deps.ChainRepo == nil || deps.TaskRepo == nil {
		return
	}
	registerQueryProbeExecutor(s, deps)
	registerSourceAttributionExecutor(s, deps)
	registerContentGapFillExecutor(s, deps)
	registerCaptureLeadExecutor(s, deps)
}

func registerQueryProbeExecutor(s *WorkflowService, deps DecisionChainDeps) {
	s.RegisterExecutor("query_probe", func(ctx context.Context, step map[string]interface{}) (string, error) {
		keyword, _ := step["keyword"].(string)
		if strings.TrimSpace(keyword) == "" {
			return "", fmt.Errorf("query_probe 缺少 keyword")
		}

		budget := 200
		if b, ok := step["budget"].(float64); ok && b > 0 {
			budget = int(b)
		}
		if n, cerr := deps.ChainRepo.CountToday(ctx); cerr == nil && int(n) >= budget {
			return "", fmt.Errorf("探针日预算已用尽(%d/%d)，明日再试或上调 budget", n, budget)
		}
		variants := probeVariants(keyword)
		chainID := fmt.Sprintf("probe:%s:%d:%s", keyword, time.Now().Unix(), time.Now().Format("2006-01-02"))

		out := map[string]any{
			"keyword":       keyword,
			"chain_id":      chainID,
			"probes":        []map[string]any{},
			"cited_domains": map[string]int{},
			"brand_hits":    0,
		}
		for i, q := range variants {
			pr, err := deps.Probe.Probe(ctx, q)
			if err != nil {
				continue
			}
			domains := map[string]bool{}
			for _, c := range pr.Citations {
				if u, perr := url.Parse(c.URL); perr == nil && u.Host != "" {
					domains[u.Host] = true
					out["cited_domains"].(map[string]int)[u.Host]++
				}
			}
			position := "absent"
			if deps.Brand != "" && strings.Contains(pr.Response, deps.Brand) {
				position = "candidate"
				out["brand_hits"] = out["brand_hits"].(int) + 1
			}
			_ = deps.ChainRepo.Append(ctx, &model.GeoQueryChain{
				ChainID: chainID, Seq: i + 1, Query: q,
				Intent: stepString(step, "intent"), BrandName: deps.Brand,
				BrandPosition: position, Source: "probe",
				CitedURLs: strings.Join(keysOf(domains), ","),
			})
			out["probes"] = append(out["probes"].([]map[string]any), map[string]any{
				"query": q, "engine": pr.Engine, "citations": len(pr.Citations), "position": position,
			})
		}
		b, _ := json.Marshal(out)
		return string(b), nil
	})
}

func registerSourceAttributionExecutor(s *WorkflowService, deps DecisionChainDeps) {
	s.RegisterExecutor("source_attribution", func(ctx context.Context, step map[string]interface{}) (string, error) {
		raw := upstreamJSON(step, "query_probe")
		if raw == "" {
			return "", fmt.Errorf("source_attribution 需要上游 query_probe 结果")
		}
		var probeOut struct {
			CitedDomains map[string]int `json:"cited_domains"`
			ChainID      string         `json:"chain_id"`
		}
		if err := json.Unmarshal([]byte(raw), &probeOut); err != nil {
			return "", fmt.Errorf("解析上游结果失败: %w", err)
		}
		ourDomains := map[string]bool{}
		for _, d := range strings.Split(stepString(step, "our_domains"), ",") {
			if d = strings.TrimSpace(d); d != "" {
				ourDomains[strings.ToLower(d)] = true
			}
		}
		gaps := []map[string]any{}
		covered := []string{}
		for dom, hits := range probeOut.CitedDomains {
			if ourDomains[strings.ToLower(dom)] {
				covered = append(covered, dom)
				continue
			}
			gaps = append(gaps, map[string]any{"domain": dom, "hits": hits})
		}
		out, _ := json.Marshal(map[string]any{
			"chain_id":      probeOut.ChainID,
			"coverage_rate": safeDiv(len(covered), len(probeOut.CitedDomains)),
			"coverage_gap":  gaps,
			"covered":       covered,
		})
		return string(out), nil
	})
}

func registerContentGapFillExecutor(s *WorkflowService, deps DecisionChainDeps) {
	s.RegisterExecutor("content_gap_fill", func(ctx context.Context, step map[string]interface{}) (string, error) {
		raw := upstreamJSON(step, "source_attribution")
		if raw == "" {
			return "", fmt.Errorf("content_gap_fill 需要上游 source_attribution 结果")
		}
		var attr struct {
			ChainID      string           `json:"chain_id"`
			CoverageGap  []map[string]any `json:"coverage_gap"`
			CoverageRate float64          `json:"coverage_rate"`
		}
		if err := json.Unmarshal([]byte(raw), &attr); err != nil {
			return "", err
		}
		intent := stepString(step, "intent")
		created := 0
		for _, g := range attr.CoverageGap {
			dom, _ := g["domain"].(string)
			task := &model.GeoContentTask{
				Keyword: stepString(step, "keyword"),
				Intent:  intent,
				GapType: "missing_domain",
				Detail:  fmt.Sprintf("竞对信源 %s 被 AI 引用而自家缺席；需在该类信源布控针对性内容", dom),
				Status:  "pending",
			}
			if err := deps.TaskRepo.Create(ctx, task); err == nil {
				created++
			}
		}
		out, _ := json.Marshal(map[string]any{
			"tasks_created": created, "coverage_rate": attr.CoverageRate,
		})
		return string(out), nil
	})
}

func registerCaptureLeadExecutor(s *WorkflowService, deps DecisionChainDeps) {
	if deps.LeadPort == nil {
		return
	}
	s.RegisterExecutor("capture_lead", func(ctx context.Context, step map[string]interface{}) (string, error) {
		contact := stepString(step, "contact")
		contactType := stepString(step, "contact_type")
		if contact == "" {
			contact = stepString(step, "phone")
			contactType = "phone"
		}
		if contact == "" {
			return "", fmt.Errorf("capture_lead 缺少 contact")
		}
		clueID, err := deps.LeadPort.CaptureLead(ctx, contact, contactType,
			stepString(step, "chain_id"), stepString(step, "intent"))
		if err != nil {
			return "", err
		}
		b, _ := json.Marshal(map[string]string{"clue_id": clueID})
		return string(b), nil
	})
}

func upstreamJSON(step map[string]interface{}, stepType string) string {
	sr, ok := step["_step_results"].(map[string]*dto.StepResult)
	if !ok {
		return ""
	}
	best := ""
	for _, r := range sr {
		if r != nil && r.StepType == stepType && r.Result != "" {
			best = r.Result
		}
	}
	return best
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func safeDiv(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b)
}

func probeVariants(keyword string) []string {
	return []string{
		keyword,
		keyword + " 值得选吗？有什么优缺点？",
		keyword + " 和同类产品对比哪个更好？",
		keyword + " 推荐哪家？给出排行和理由",
	}
}

// CaptureLeadFunc 供装配层的函数式适配器
type CaptureLeadFunc func(ctx context.Context, contact, contactType, chainID, intent string) (string, error)

// CaptureLead 实现 LeadCapturePort
func (f CaptureLeadFunc) CaptureLead(ctx context.Context, contact, contactType, chainID, intent string) (string, error) {
	return f(ctx, contact, contactType, chainID, intent)
}

// RegisterCaptureLeadExecutor 单独注入线索捕获端口（装配层调用）
func (s *WorkflowService) RegisterCaptureLeadExecutor(port LeadCapturePort) {
	registerCaptureLeadExecutor(s, DecisionChainDeps{LeadPort: port})
}

// InboxChainSync inbox 侧思维链回填（v3 决策链化 Phase3 收口）：
// 已绑定 OneID 的 GEO 归因链，其客户的真实会话消息回写为 source=inbox 行，
// 完成"探针模拟 → 真实用户行为"的数据闭环。
type InboxChainSync struct {
	chainRepo repository.GeoQueryChainRepository
}

func NewInboxChainSync(chainRepo repository.GeoQueryChainRepository) *InboxChainSync {
	return &InboxChainSync{chainRepo: chainRepo}
}

// HandleCustomerMessage 处理客户消息事件（event.TopicCustomerMessageReceived）
func (s *InboxChainSync) HandleCustomerMessage(ctx context.Context, oneID, content string) {
	if s.chainRepo == nil || strings.TrimSpace(oneID) == "" || strings.TrimSpace(content) == "" {
		return
	}
	rows, err := s.chainRepo.ListByOneID(ctx, oneID)
	if err != nil || len(rows) == 0 {
		return
	}
	_ = s.chainRepo.Append(ctx, &model.GeoQueryChain{
		ChainID:   rows[0].ChainID,
		Seq:       len(rows) + 1,
		Query:     content,
		Intent:    classifyIntent(content),
		BrandName: rows[0].BrandName,
		Source:    "inbox",
		OneID:     oneID,
	})
}
