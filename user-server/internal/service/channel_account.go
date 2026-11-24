package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

var (
	// ErrChannelAccountNotFound 渠道账号不存在（Controller 映射 404）。
	ErrChannelAccountNotFound = errors.New("渠道账号不存在")
	// ErrChannelAccountInvalid 渠道账号参数错误（Controller 映射 400）。
	ErrChannelAccountInvalid = errors.New("渠道账号参数错误")
	// ErrChannelNotFound 渠道不存在。
	ErrChannelNotFound = errors.New("渠道不存在")
	// ErrChannelAccountDataScopeInvalid 数据授权 scope 不是合法 JSON 对象。
	ErrChannelAccountDataScopeInvalid = errors.New("数据授权范围必须是 JSON 对象")
)

// allowedChannelAccountTypes 渠道账号类型白名单（单聊/群聊/客服）。
var allowedChannelAccountTypes = map[string]bool{"dm": true, "single": true, "group": true, "group_chat": true, "cs": true, "service": true}

// validateChannelAccountSave 校验渠道账号入参（FR-024 全字段完整化后的统一校验口）。
func validateChannelAccountSave(in *dto.ChannelAccountSave) error {
	if in == nil || in.ChannelID == 0 || strings.TrimSpace(in.Name) == "" {
		return ErrChannelAccountInvalid
	}
	if in.Type != "" && !allowedChannelAccountTypes[strings.ToLower(strings.TrimSpace(in.Type))] {
		return fmt.Errorf("%w: 不支持的账号类型 %q", ErrChannelAccountInvalid, in.Type)
	}
	if in.DataScope != "" {
		var obj map[string]any
		if err := json.Unmarshal([]byte(in.DataScope), &obj); err != nil || obj == nil {
			return ErrChannelAccountDataScopeInvalid
		}
	}
	if in.Status != nil && *in.Status != 0 && *in.Status != 1 {
		return ErrChannelAccountInvalid
	}
	return nil
}

func toAccountDetail(acc *model.ChannelAccount) dto.ChannelAccountDetail {
	return dto.ChannelAccountDetail{
		ID: acc.ID, ChannelID: acc.ChannelID, Name: acc.Name, Remark: acc.Remark,
		SelfIdentifyName: acc.SelfIdentifyName, Type: acc.Type,
		AutoReplyAgentID: acc.AutoReplyAgentID, LeadMiningAgentID: acc.LeadMiningAgentID,
		OwnerUserIDs: []string(acc.OwnerUserIDs), DataScope: acc.DataScope,
		IdentifyCode: acc.IdentifyCode, Status: acc.Status,
		CreatedAt: acc.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt: acc.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
}

// ChannelAccountService 渠道账号业务服务。
//
// ListOptions 是所有内部渠道账号选择（下拉、配置页、规则作用域选择、通知人配置等）
// 的唯一入口（FR-029）——任何调用方不得自己拼 repository 选项或直查表。
type ChannelAccountService interface {
	ListOptions(ctx context.Context, tenantID uint, q dto.ChannelAccountListQuery) ([]dto.ChannelAccountOption, error)
	Get(ctx context.Context, tenantID, id uint) (*dto.ChannelAccountOption, error)
	// GetDetail 返回全字段（含备注/负责人/数据授权），管理面编辑回填用。
	GetDetail(ctx context.Context, tenantID, id uint) (*dto.ChannelAccountDetail, error)
	Create(ctx context.Context, tenantID uint, in *dto.ChannelAccountSave) (*dto.ChannelAccountOption, error)
	Update(ctx context.Context, tenantID, id uint, in *dto.ChannelAccountSave) error
	Delete(ctx context.Context, tenantID, id uint) error
}

type channelAccountService struct {
	repo repository.ChannelAccountRepository
}

// NewChannelAccountService 创建渠道账号服务。
func NewChannelAccountService() ChannelAccountService {
	return &channelAccountService{repo: repository.NewChannelAccountRepository()}
}

// NewChannelAccountServiceWithRepo 用显式仓储构造（装配点/测试用）。
func NewChannelAccountServiceWithRepo(repo repository.ChannelAccountRepository) ChannelAccountService {
	return &channelAccountService{repo: repo}
}

func toAccountOption(acc *model.ChannelAccount) dto.ChannelAccountOption {
	return dto.ChannelAccountOption{
		ID:                acc.ID,
		ChannelID:         acc.ChannelID,
		Name:              acc.Name,
		IdentifyCode:      acc.IdentifyCode,
		Type:              acc.Type,
		Status:            acc.Status,
		SelfIdentifyName:  acc.SelfIdentifyName,
		AutoReplyAgentID:  acc.AutoReplyAgentID,
		LeadMiningAgentID: acc.LeadMiningAgentID,
	}
}

// ListOptions 统一下拉：默认只返回启用中的账号，避免停用的账号被继续选中。
func (s *channelAccountService) ListOptions(ctx context.Context, tenantID uint, q dto.ChannelAccountListQuery) ([]dto.ChannelAccountOption, error) {
	if s == nil || s.repo == nil || !s.repo.Available() {
		return nil, repository.ErrChannelStoreUnavailable
	}
	if tenantID == 0 {
		return nil, ErrChannelAccountInvalid
	}
	status := q.Status
	if status == nil {
		on := 1
		status = &on
	}
	rows, err := s.repo.List(ctx, repository.ChannelAccountListOptions{
		TenantID:  tenantID,
		ChannelID: q.ChannelID,
		Status:    status,
		Keyword:   q.Keyword,
		IDs:       q.IDs,
		Limit:     q.Limit,
		Offset:    q.Offset,
	})
	if err != nil {
		return nil, err
	}
	out := make([]dto.ChannelAccountOption, 0, len(rows))
	for i := range rows {
		out = append(out, toAccountOption(&rows[i]))
	}
	return out, nil
}

func (s *channelAccountService) Get(ctx context.Context, tenantID, id uint) (*dto.ChannelAccountOption, error) {
	if tenantID == 0 || id == 0 {
		return nil, ErrChannelAccountInvalid
	}
	acc, err := s.repo.GetByID(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if acc == nil {
		return nil, ErrChannelAccountNotFound
	}
	opt := toAccountOption(acc)
	return &opt, nil
}

func (s *channelAccountService) GetDetail(ctx context.Context, tenantID, id uint) (*dto.ChannelAccountDetail, error) {
	if tenantID == 0 || id == 0 {
		return nil, ErrChannelAccountInvalid
	}
	acc, err := s.repo.GetByID(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if acc == nil {
		return nil, ErrChannelAccountNotFound
	}
	detail := toAccountDetail(acc)
	return &detail, nil
}

func (s *channelAccountService) Create(ctx context.Context, tenantID uint, in *dto.ChannelAccountSave) (*dto.ChannelAccountOption, error) {
	if tenantID == 0 {
		return nil, ErrChannelAccountInvalid
	}
	if err := validateChannelAccountSave(in); err != nil {
		return nil, err
	}
	status := 1
	if in.Status != nil {
		status = *in.Status
	}
	typ := strings.ToLower(strings.TrimSpace(in.Type))
	if typ == "" {
		typ = "dm"
	}
	acc := &model.ChannelAccount{
		TenantID:          tenantID,
		ChannelID:         in.ChannelID,
		Name:              strings.TrimSpace(in.Name),
		Remark:            in.Remark,
		SelfIdentifyName:  in.SelfIdentifyName,
		Type:              typ,
		AutoReplyAgentID:  in.AutoReplyAgentID,
		LeadMiningAgentID: in.LeadMiningAgentID,
		OwnerUserIDs:      model.JSONStrings(in.OwnerUserIDs),
		DataScope:         "{}",
		Status:            status,
	}
	if in.DataScope != "" {
		acc.DataScope = in.DataScope
	}
	if err := s.repo.Create(ctx, acc); err != nil {
		return nil, err
	}
	opt := toAccountOption(acc)
	return &opt, nil
}

func (s *channelAccountService) Update(ctx context.Context, tenantID, id uint, in *dto.ChannelAccountSave) error {
	if tenantID == 0 || id == 0 {
		return ErrChannelAccountInvalid
	}
	if err := validateChannelAccountSave(in); err != nil {
		return err
	}
	existing, err := s.repo.GetByID(ctx, tenantID, id)
	if err != nil {
		return err
	}
	if existing == nil {
		return ErrChannelAccountNotFound
	}
	typ := strings.ToLower(strings.TrimSpace(in.Type))
	if typ == "" {
		typ = "dm"
	}
	updates := map[string]any{
		"channel_id":           in.ChannelID,
		"name":                 strings.TrimSpace(in.Name),
		"remark":               in.Remark,
		"self_identify_name":   in.SelfIdentifyName,
		"type":                 typ,
		"auto_reply_agent_id":  in.AutoReplyAgentID,
		"lead_mining_agent_id": in.LeadMiningAgentID,
		"owner_user_ids":       model.JSONStrings(in.OwnerUserIDs),
	}
	if in.DataScope != "" {
		updates["data_scope"] = in.DataScope
	}
	if in.Status != nil {
		updates["status"] = *in.Status
	}
	return s.repo.Update(ctx, tenantID, id, updates)
}

func (s *channelAccountService) Delete(ctx context.Context, tenantID, id uint) error {
	if tenantID == 0 || id == 0 {
		return ErrChannelAccountInvalid
	}
	return s.repo.Delete(ctx, tenantID, id)
}
