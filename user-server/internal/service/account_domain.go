package service

import (
	"strconv"

	"context"
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils"
)

// BatchImportCluesFromDTO 批量导入线索（请求 DTO 已在 controller 完成类型校验）
func (s *ClueService) BatchImportCluesFromDTO(ctx context.Context, reqs []dto.ImportClueRequest) (int64, int64, error) {
	clues := make([]*model.Clue, 0, len(reqs))
	for _, item := range reqs {
		clueType, err := strconv.ParseInt(item.Type, 10, 64)
		if err != nil {
			return 0, 0, err
		}
		isVerify := item.IsVerify
		if isVerify < 0 || isVerify > 1 {
			isVerify = 0
		}
		intentScore := item.IntentScore
		if intentScore < 0 || intentScore > 100 {
			intentScore = 0
		}
		isOpportunity := item.IsOpportunity
		if isOpportunity < 0 || isOpportunity > 1 {
			isOpportunity = 0
		}
		clues = append(clues, &model.Clue{
			Name:           item.Name,
			Account:        item.Account,
			City:           item.City,
			Address:        item.Address,
			Desc:           item.Desc,
			Type:           clueType,
			IsVerify:       isVerify,
			IntentScore:    intentScore,
			IsOpportunity:  isOpportunity,
			SourceID:       item.SourceID,
			OneID:          item.OneID,
			ConversationID: item.ConversationID,
			OwnerAccount:   item.OwnerAccount,
		})
	}
	success, skipped, err := s.BatchImportClues(ctx, clues)
	if err != nil {
		return success, skipped, err
	}

	if success > 0 && s.scoreSvc != nil {
		// 评分是导入后的尽力而为补算。评分服务随 ClueService 在装配点注入，
		// 不在请求/协程体里现造（那会退回全局句柄，测试 cleanup 置 nil 后直接 panic）。
		// SafeGo：补算 panic 不能击穿进程（ clues 为本调用局部值，直接捕获与原传参等价）。
		utils.SafeGo(ctx, "clue.score_imported", func(_ context.Context) {
			s.scoreImportedClues(tenant.BackgroundWithTenant(ctx), s.scoreSvc, clues)
		})
	}
	return success, skipped, nil
}

func (s *ClueService) scoreImportedClues(ctx context.Context, scoreSvc *ClueScoreService, clues []*model.Clue) {
	for _, c := range clues {
		if c == nil {
			continue
		}
		_, _ = scoreSvc.ScoreClue(ctx, c)
	}
}
