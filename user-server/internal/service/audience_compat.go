package service

// 兼容层：圈选层已迁入 internal/service/audience（sop 拆包第四刀前置，
// 与 sop_scheduler 的"按条件实时取名"直接 import，不再走接口注入）。

import (
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service/audience"
)

type AudienceConfig = audience.AudienceConfig

type AudienceSelection = audience.AudienceSelection

type AudienceSelector = audience.AudienceSelector

const (
	DefaultAudienceLimit = audience.DefaultAudienceLimit
	MaxAudienceLimit     = audience.MaxAudienceLimit
)

func NewAudienceSelectorWithSources(
	rfm repository.CustomerRFMRepository,
	tags repository.CustomerTagAssignmentRepository,
	churn *repository.ChurnScoreRepository,
) *AudienceSelector {
	return audience.NewAudienceSelectorWithSources(rfm, tags, churn)
}

func SetAudienceDefaultLimitProvider(fn func() int) { audience.SetAudienceDefaultLimitProvider(fn) }

func SetAudienceMaxLimitProvider(fn func() int) { audience.SetAudienceMaxLimitProvider(fn) }

func EffectiveAudienceDefaultLimit() int { return audience.EffectiveAudienceDefaultLimit() }

func EffectiveAudienceMaxLimit() int { return audience.EffectiveAudienceMaxLimit() }
