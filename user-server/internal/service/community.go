package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	bizerr "hivemtk-user/internal/domain/errors"
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"

	"github.com/google/uuid"
)

// 服务层哨兵：控制器据此回 404（带业务 ref），避免一律落成「记录不存在」。
var (
	// ErrCommunityGroupNotFound 社群不存在
	ErrCommunityGroupNotFound = errors.New("社群不存在")
	// ErrCommunityMemberNotFound 社群成员不存在
	ErrCommunityMemberNotFound = errors.New("社群成员不存在")
)

// wrapCommunityNotFound 把仓储层哨兵翻成服务层哨兵并带上业务 ref（id）。
func wrapCommunityNotFound(err error, ref string) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, repository.ErrCommunityGroupNotFound):
		return fmt.Errorf("%w: %s", ErrCommunityGroupNotFound, ref)
	case errors.Is(err, repository.ErrCommunityMemberNotFound):
		return fmt.Errorf("%w: %s", ErrCommunityMemberNotFound, ref)
	}
	return err
}

type CommunityService struct {
	repo repository.CommunityRepository
}

// NewCommunityService 用装配点注入的连接创建社群服务。
func NewCommunityService(repo repository.CommunityRepository) *CommunityService {
	return &CommunityService{repo: repo}
}

func (s *CommunityService) GetGroups(ctx context.Context, page, pageSize int, search string) ([]*dto.CommunityGroupResponse, int64, error) {
	groups, total, err := s.repo.GetGroups(ctx, page, pageSize, search)
	if err != nil {
		return nil, 0, err
	}

	responses := make([]*dto.CommunityGroupResponse, 0, len(groups))
	for _, group := range groups {
		resp := &dto.CommunityGroupResponse{
			ID:          group.ID,
			Name:        group.Name,
			Description: group.Description,
			MemberCount: group.MemberCount,
			CreatedAt:   group.CreatedAt,
			UpdatedAt:   group.UpdatedAt,
		}
		responses = append(responses, resp)
	}

	return responses, total, nil
}

func (s *CommunityService) GetGroupByID(ctx context.Context, id string) (*dto.CommunityGroupResponse, error) {
	group, err := s.repo.GetGroupByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if group == nil {
		return nil, fmt.Errorf("%w: %s", ErrCommunityGroupNotFound, id)
	}

	resp := &dto.CommunityGroupResponse{
		ID:          group.ID,
		Name:        group.Name,
		Description: group.Description,
		MemberCount: group.MemberCount,
		CreatedAt:   group.CreatedAt,
		UpdatedAt:   group.UpdatedAt,
	}
	return resp, nil
}

func (s *CommunityService) CreateGroup(ctx context.Context, req *dto.CreateCommunityGroupRequest) (*dto.CommunityGroupResponse, error) {
	// 名称是页面的必填项，但导入/接口直调绕过了前端校验：空名会落成一排无法辨认的群。
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, bizerr.New(bizerr.CodeParamInvalid, "请求参数错误：name（分组名称）不能为空")
	}
	if n := utf8.RuneCountInString(name); n > 100 {
		return nil, bizerr.New(bizerr.CodeParamInvalid,
			fmt.Sprintf("请求参数错误：name（分组名称）不能超过 100 个字符（当前 %d）", n))
	}
	req.Name = name

	id := uuid.New().String()

	group := &model.CommunityGroup{
		ID:          id,
		Name:        req.Name,
		Description: req.Description,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	savedGroup, err := s.repo.CreateGroup(ctx, group)
	if err != nil {
		return nil, err
	}

	response := &dto.CommunityGroupResponse{
		ID:          savedGroup.ID,
		Name:        savedGroup.Name,
		Description: savedGroup.Description,
		MemberCount: savedGroup.MemberCount,
		CreatedAt:   savedGroup.CreatedAt,
		UpdatedAt:   savedGroup.UpdatedAt,
	}

	return response, nil
}

func (s *CommunityService) UpdateGroup(ctx context.Context, id string, req *dto.UpdateCommunityGroupRequest) error {
	updates := make(map[string]any)
	if req.Name != "" {
		updates["name"] = req.Name
	}
	if req.Description != "" {
		updates["description"] = req.Description
	}
	updates["updated_at"] = time.Now()

	return wrapCommunityNotFound(s.repo.UpdateGroup(ctx, id, updates), id)
}

func (s *CommunityService) DeleteGroup(ctx context.Context, id string) error {
	return wrapCommunityNotFound(s.repo.DeleteGroup(ctx, id), id)
}

func (s *CommunityService) GetMembers(ctx context.Context, groupID string, page, pageSize int, search string) ([]*dto.CommunityMemberResponse, int64, error) {
	members, total, err := s.repo.GetMembers(ctx, groupID, page, pageSize, search)
	if err != nil {
		return nil, 0, err
	}

	responses := make([]*dto.CommunityMemberResponse, 0, len(members))
	for _, member := range members {
		resp := &dto.CommunityMemberResponse{
			ID:       member.ID,
			GroupID:  member.GroupID,
			Name:     member.Name,
			Username: member.Username,
			Role:     member.Role,
			Status:   member.Status,
			JoinDate: member.JoinDate,
			LastSeen: member.LastSeen,
		}
		responses = append(responses, resp)
	}

	return responses, total, nil
}

func (s *CommunityService) GetMemberByID(ctx context.Context, id string) (*dto.CommunityMemberResponse, error) {
	member, err := s.repo.GetMemberByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if member == nil {
		return nil, fmt.Errorf("%w: %s", ErrCommunityMemberNotFound, id)
	}

	resp := &dto.CommunityMemberResponse{
		ID:       member.ID,
		GroupID:  member.GroupID,
		Name:     member.Name,
		Username: member.Username,
		Role:     member.Role,
		Status:   member.Status,
		JoinDate: member.JoinDate,
		LastSeen: member.LastSeen,
	}
	return resp, nil
}

func (s *CommunityService) AddMember(ctx context.Context, req *dto.AddCommunityMemberRequest) (*dto.CommunityMemberResponse, error) {
	group, err := s.repo.GetGroupByID(ctx, req.GroupID)
	if err != nil {
		return nil, err
	}
	if group == nil {
		return nil, fmt.Errorf("%w: %s", ErrCommunityGroupNotFound, req.GroupID)
	}
	exists, err := s.repo.ExistsMemberByUsername(ctx, req.GroupID, req.Username)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, errors.New("该用户名已在群组中")
	}

	id := uuid.New().String()

	member := &model.CommunityMember{
		ID:        id,
		GroupID:   req.GroupID,
		Name:      req.Name,
		Username:  req.Username,
		Role:      req.Role,
		Status:    "active",
		JoinDate:  time.Now(),
		LastSeen:  time.Now(),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	savedMember, err := s.repo.AddMember(ctx, member)
	if err != nil {
		return nil, err
	}

	response := &dto.CommunityMemberResponse{
		ID:       savedMember.ID,
		GroupID:  savedMember.GroupID,
		Name:     savedMember.Name,
		Username: savedMember.Username,
		Role:     savedMember.Role,
		Status:   savedMember.Status,
		JoinDate: savedMember.JoinDate,
		LastSeen: savedMember.LastSeen,
	}

	return response, nil
}

func (s *CommunityService) UpdateMember(ctx context.Context, id string, req *dto.UpdateCommunityMemberRequest) error {
	updates := make(map[string]any)
	if req.Name != "" {
		updates["name"] = req.Name
	}
	if req.Role != "" {
		updates["role"] = req.Role
	}
	if req.Status != "" {
		updates["status"] = req.Status
	}
	updates["updated_at"] = time.Now()

	return wrapCommunityNotFound(s.repo.UpdateMember(ctx, id, updates), id)
}

func (s *CommunityService) RemoveMember(ctx context.Context, id string) error {
	return wrapCommunityNotFound(s.repo.RemoveMember(ctx, id), id)
}

func (s *CommunityService) GetMessages(ctx context.Context, groupID string, page, pageSize int) ([]*dto.CommunityMessageResponse, int64, error) {
	messages, total, err := s.repo.GetMessages(ctx, groupID, page, pageSize)
	if err != nil {
		return nil, 0, err
	}

	responses := make([]*dto.CommunityMessageResponse, 0, len(messages))
	for _, message := range messages {
		resp := &dto.CommunityMessageResponse{
			ID:          message.ID,
			GroupID:     message.GroupID,
			UserID:      message.UserID,
			UserName:    message.UserName,
			Content:     message.Content,
			Timestamp:   message.Timestamp,
			MessageType: message.MessageType,
		}
		responses = append(responses, resp)
	}

	return responses, total, nil
}

// GetGroupStatistics 单个社群的口径（社群页统计弹窗）。
// 群不存在回 ErrCommunityGroupNotFound —— 空统计与「群删了」必须区分。
func (s *CommunityService) GetGroupStatistics(ctx context.Context, groupID string) (*dto.CommunityGroupStatsResponse, error) {
	members, messages, activeToday, name, err := s.repo.GetGroupStats(ctx, groupID)
	if err != nil {
		return nil, wrapCommunityNotFound(err, groupID)
	}
	return &dto.CommunityGroupStatsResponse{
		GroupID:      groupID,
		Name:         name,
		MemberCount:  int(members),
		MessageCount: int(messages),
		ActiveCount:  int(activeToday),
	}, nil
}

func (s *CommunityService) GetStatistics(ctx context.Context) (*dto.CommunityStatisticsResponse, error) {
	stats, err := s.repo.GetStatistics(ctx)
	if err != nil {
		return nil, err
	}

	return &dto.CommunityStatisticsResponse{
		TotalGroups:     int(stats.TotalGroups),
		TotalMembers:    int(stats.TotalMembers),
		TotalMessages:   int(stats.TotalMessages),
		ActiveGroups:    int(stats.ActiveGroups),
		NewMembersToday: int(stats.NewMembersToday),
	}, nil
}
