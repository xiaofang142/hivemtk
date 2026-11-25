package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"hivemtk-user/internal/model"
)

// CRM-18：复用 OperationLog 记录业务变更的字段前后差异/来源/可信 actor/业务事件，
// 写入失败显式返回，不让"审计故障"被报成完整成功。
//
// 不重建全局审计平台：middleware/audit.go 继续记请求元数据，这里只补业务语义层。

// CRMBusinessAuditInput 一次业务变更的审计输入。
type CRMBusinessAuditInput struct {
	ActorID       uint
	Actor         string
	Action        string
	Module        string
	Resource      string
	ResourceID    string
	BusinessEvent string
	Source        string
	Before        map[string]any
	After         map[string]any
}

// CRMBusinessAuditRecord 写入结果（审计 ID 显式返回，失败时 error 非空）。
type CRMBusinessAuditRecord struct {
	ID     uint   `json:"id"`
	Status string `json:"status"`
}

var crmAuditSensitiveRe = regexp.MustCompile(`(?i)(phone|mobile|email|身份证|id_card|微信|wechat|token|secret|password|银行)`)

// CRMBusinessAuditService 业务审计写入口；Mask 可为 nil，默认按字段名+值形态脱敏。
type CRMBusinessAuditService struct {
	repo repositoryOperationLogWriter
	Mask func(string) string
}

type repositoryOperationLogWriter interface {
	Create(ctx context.Context, log *model.OperationLog) error
}

func NewCRMBusinessAuditServiceWithRepo(repo repositoryOperationLogWriter) *CRMBusinessAuditService {
	return &CRMBusinessAuditService{repo: repo}
}

func NewCRMBusinessAuditService() *CRMBusinessAuditService {
	return NewCRMBusinessAuditServiceWithRepo(NewOperationLogService())
}

// Record 写一条含字段差异的 OperationLog；业务事件/来源进 Detail，敏感值脱敏。
func (s *CRMBusinessAuditService) Record(ctx context.Context, in CRMBusinessAuditInput) (*CRMBusinessAuditRecord, error) {
	if s == nil || s.repo == nil {
		return nil, errors.New("审计仓储未装配，拒绝宣称审计成功")
	}
	if strings.TrimSpace(in.Action) == "" || strings.TrimSpace(in.ResourceID) == "" {
		return nil, errors.New("审计缺少动作或对象 ID")
	}
	mask := s.Mask
	if mask == nil {
		mask = crmAuditDefaultMask
	}
	before := maskCRMAuditFields(in.Before, mask)
	after := maskCRMAuditFields(in.After, mask)
	detail := map[string]any{
		"business_event": in.BusinessEvent,
		"source":         in.Source,
		"actor_id":       in.ActorID,
		"fields_changed": crmAuditChangedFields(in.Before, in.After),
	}
	rawDetail, _ := json.Marshal(detail)
	rawBefore, _ := json.Marshal(before)
	rawAfter, _ := json.Marshal(after)
	log := &model.OperationLog{
		UserID: in.ActorID, Username: in.Actor, Action: in.Action,
		Module: in.Module, Resource: in.Resource, ResourceID: in.ResourceID,
		Detail: string(rawDetail), OldValue: string(rawBefore), NewValue: string(rawAfter),
	}
	if err := s.repo.Create(ctx, log); err != nil {
		return nil, fmt.Errorf("业务审计写失败（对象 %s/%s 的业务变更已发生）: %w", in.Resource, in.ResourceID, err)
	}
	return &CRMBusinessAuditRecord{ID: log.ID, Status: "written"}, nil
}

func crmAuditChangedFields(before, after map[string]any) []string {
	keys := map[string]bool{}
	for k := range before {
		keys[k] = true
	}
	for k := range after {
		keys[k] = true
	}
	out := make([]string, 0, len(keys))
	for k := range keys {
		if fmt.Sprint(before[k]) != fmt.Sprint(after[k]) {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func maskCRMAuditFields(fields map[string]any, mask func(string) string) map[string]any {
	out := make(map[string]any, len(fields))
	for k, v := range fields {
		raw := fmt.Sprint(v)
		if crmAuditSensitiveRe.MatchString(k) || leadNotifyPhoneRe.MatchString(raw) || leadNotifyEmailRe.MatchString(raw) {
			out[k] = mask(raw)
			continue
		}
		out[k] = v
	}
	return out
}

func crmAuditDefaultMask(raw string) string {
	if leadNotifyPhoneRe.MatchString(raw) {
		return leadNotifyPhoneRe.ReplaceAllString(raw, "1**********")
	}
	if leadNotifyEmailRe.MatchString(raw) {
		return leadNotifyEmailRe.ReplaceAllString(raw, "***@***")
	}
	return "[redacted]"
}
