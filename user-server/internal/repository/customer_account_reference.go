package repository

import (
	"context"
	"sort"
	"strconv"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
)

// CRMAccountReferenceReader 为数字账号 ACL 提供本次查询可用的来源引用。
// 不扩充通用渠道账号接口；识别码不是持久授权键。
type CRMAccountReferenceReader interface {
	GetCRMSourceReferences(ctx context.Context, tenantID uint, ids []uint) (map[uint][]string, error)
}

// GetCRMSourceReferences 只为本租户现存账号返回数字 ID 与唯一有效的码。
// 权威记录存在时覆盖冗余码；跨账号/租户歧义、过期码和数字形别名均拒绝。
func (r *channelAccountRepo) GetCRMSourceReferences(ctx context.Context, tenantID uint, ids []uint) (map[uint][]string, error) {
	targets, err := r.GetByIDs(ctx, tenantID, ids)
	if err != nil {
		return nil, err
	}
	out := make(map[uint][]string, len(targets))
	if tenantID == 0 || len(targets) == 0 {
		return out, nil
	}
	channels := make([]uint, 0, len(targets))
	for _, a := range targets {
		channels = append(channels, a.ChannelID)
	}
	sysDB := r.db.WithContext(tenant.WithSystemScope(ctx))
	var authority []model.ChannelIdentifyCode
	if err := sysDB.Where("tenant_id = ? AND channel_id IN ?", tenantID, channels).Find(&authority).Error; err != nil {
		return nil, err
	}
	byChannel := make(map[uint]model.ChannelIdentifyCode)
	for _, rec := range authority {
		byChannel[rec.ChannelID] = rec
	}
	now := time.Now()
	validCodes := func(rec model.ChannelIdentifyCode) []string {
		codes := []string{rec.Code}
		if rec.PrevValidUntil != nil && now.Before(*rec.PrevValidUntil) {
			codes = append(codes, rec.PrevCode)
		}
		return codes
	}
	tokens := make(map[string]bool)
	for _, a := range targets {
		out[a.ID] = []string{strconv.FormatUint(uint64(a.ID), 10)}
		codes := []string{a.IdentifyCode}
		if rec, ok := byChannel[a.ChannelID]; ok {
			codes = validCodes(rec)
		}
		for _, code := range codes {
			if _, numeric := strconv.ParseUint(code, 10, 64); code != "" && numeric != nil {
				tokens[code] = true
			}
		}
	}
	if len(tokens) == 0 {
		return out, nil
	}
	keys := make([]string, 0, len(tokens))
	for code := range tokens {
		keys = append(keys, code)
	}
	// 歧义判定显式使用系统作用域；返回值仍只包含上面的目标租户账号。
	var matchingCodes []model.ChannelIdentifyCode
	if err := sysDB.Where("code IN ? OR (prev_code IN ? AND prev_valid_until > ?)", keys, keys, now).Find(&matchingCodes).Error; err != nil {
		return nil, err
	}
	claims := make(map[string][]model.ChannelIdentifyCode)
	for _, rec := range matchingCodes {
		channels = append(channels, rec.ChannelID)
		seenCodes := make(map[string]bool)
		for _, code := range validCodes(rec) {
			if tokens[code] && !seenCodes[code] {
				claims[code] = append(claims[code], rec)
				seenCodes[code] = true
			}
		}
	}
	var candidates []model.ChannelAccount
	if err := sysDB.Where("identify_code IN ? OR channel_id IN ?", keys, channels).Find(&candidates).Error; err != nil {
		return nil, err
	}
	for _, a := range candidates {
		channels = append(channels, a.ChannelID)
	}
	authority = nil
	if err := sysDB.Where("channel_id IN ?", channels).Find(&authority).Error; err != nil {
		return nil, err
	}
	for _, rec := range authority {
		byChannel[rec.ChannelID] = rec
	}
	owners := make(map[string]map[uint]bool)
	accountsByID := make(map[uint]model.ChannelAccount, len(candidates))
	for _, a := range candidates {
		accountsByID[a.ID] = a
		codes := []string{a.IdentifyCode}
		if rec, ok := byChannel[a.ChannelID]; ok {
			if rec.TenantID != a.TenantID {
				continue
			}
			codes = validCodes(rec)
		}
		for _, code := range codes {
			if !tokens[code] {
				continue
			}
			if owners[code] == nil {
				owners[code] = make(map[uint]bool)
			}
			owners[code][a.ID] = true
		}
	}
	sort.Strings(keys)
	for _, code := range keys {
		if len(owners[code]) != 1 {
			continue
		}
		for id := range owners[code] {
			if codeClaims := claims[code]; len(codeClaims) > 0 {
				account := accountsByID[id]
				if len(codeClaims) != 1 || account.ChannelID != codeClaims[0].ChannelID || account.TenantID != codeClaims[0].TenantID {
					continue
				}
			}
			if _, allowed := out[id]; allowed {
				out[id] = append(out[id], code)
			}
		}
	}
	return out, nil
}
